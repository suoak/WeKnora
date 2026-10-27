"""Prepare the expiring test callback and bootstrap only the new isolated site.

App credentials arrive via SSH stdin. This does not authorize a Feishu user or
import documents. Generated local test-admin credentials stay in a private file.
"""
import argparse
import base64
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import datetime,json,os,pathlib,secrets,subprocess,urllib.request,urllib.error,re
app=json.loads(__import__('sys').stdin.read(16384))
if not re.fullmatch(r'cli_[a-zA-Z0-9]+',app.get('app_id','')) or not app.get('client_secret'):
 raise ValueError('invalid_test_app_credentials')
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
release=root/'isolated-site'
proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+SHA[:12]
if proof['source_sha']!=SHA or proof['project']!=project:raise ValueError('project_identity_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(release/'compose.json')]
def run(args,input=None):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,input=input,text=True,timeout=45)
def private_write(path,content):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as out:out.write(content)
environment=dict(line.split('=',1) for line in (release/'app.env').read_text().splitlines() if '=' in line)
if environment.get('KNOWHUB_ENVIRONMENT')!='isolated-test':raise ValueError('isolated_environment_required')
if PHASE=='prepare':
 p=run(compose+['ps','--status','running','--quiet','app','worker2'])
 if p.returncode or p.stdout.strip():raise ValueError('callback_prepare_requires_stopped_application')
 if environment.get('FEISHU_TEST_HTTP_CALLBACK_ENABLED')=='1':
  if environment.get('FEISHU_TEST_HTTP_CALLBACK_APP_ID')!=app['app_id']:
   raise ValueError('existing_test_callback_app_mismatch')
  expiry=environment['FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT']
 else:
  expiry=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=23)).isoformat()
  environment.update({'FEISHU_TEST_HTTP_CALLBACK_ENABLED':'1','FEISHU_TEST_HTTP_CALLBACK_APP_ID':app['app_id'],
   'FEISHU_TEST_HTTP_CALLBACK_URL':'http://172.30.232.42/feishu/callback',
   'FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT':expiry})
  temporary=release/'app.callback.env'
  private_write(temporary,''.join(key+'='+value+'\n' for key,value in environment.items()))
  temporary.replace(release/'app.env')
 print(json.dumps({'status':'PREPARED','source_sha':SHA,'project':project,
  'callback':'http://172.30.232.42/feishu/callback','expires_at':expiry,
  'application_started':False,'production_policy_changed':False,'plaintext_http_risk_remains':True}))
 raise SystemExit(0)
if not (release/'restore-verification.json').is_file():raise ValueError('verified_restore_required_before_bootstrap')
if environment.get('FEISHU_TEST_HTTP_CALLBACK_APP_ID')!=app['app_id']:
 raise ValueError('isolated_callback_configuration_required')
p=run(compose+['ps','--quiet','app'])
if p.returncode or not p.stdout.strip():raise ValueError('isolated_application_not_running')
container=run(['docker','inspect',p.stdout.strip()])
if container.returncode:raise ValueError('isolated_application_inspection_failed')
record=json.loads(container.stdout)[0]
if (record['Config'].get('Labels',{}).get('com.docker.compose.project')!=project
 or record['Image']!=proof['app_image'] or record['State'].get('Health',{}).get('Status')!='healthy'):
 raise ValueError('healthy_exact_image_test_application_required')
ports=record['HostConfig'].get('PortBindings',{}).get('8080/tcp',[])
if ports!=[{'HostIp':'127.0.0.1','HostPort':'28480'}]:raise ValueError('unexpected_application_preview_binding')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
def api(method,path,body=None,token=None):
 if not path.startswith('/api/v1/'):raise ValueError('unexpected_bootstrap_api_path')
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 request=urllib.request.Request('http://127.0.0.1:28480'+path,
  data=json.dumps(body).encode() if body is not None else None,headers=headers,method=method)
 try:
  with opener.open(request,timeout=30) as response:
   data=response.read(2*1024*1024+1)
   if len(data)>2*1024*1024:raise ValueError('bootstrap_api_response_limit')
   return response.status,json.loads(data)
 except urllib.error.HTTPError as error:
  raw=error.read(65536)
  try:detail=json.loads(raw)
  except ValueError:detail={}
  return error.code,detail
account_file=release/'bootstrap-user.json'
if not account_file.exists():
 account={'username':'feishu-v2-admin','email':'feishu-v2-admin@example.invalid','password':secrets.token_urlsafe(18)+'aA!9'}
 private_write(account_file,json.dumps(account))
else:account=json.loads(account_file.read_text())
if len(account['password'])>32:
 p=run(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres',
  '-d','knowhub_feishu_v2_test','-At'],input="SELECT count(*) FROM users WHERE email='feishu-v2-admin@example.invalid';")
 if p.returncode or p.stdout.strip()!='0':raise ValueError('registered_bootstrap_credential_must_not_be_replaced')
 account['password']=secrets.token_urlsafe(18)+'aA!9'
 temporary=release/'bootstrap-user.pending.json'
 private_write(temporary,json.dumps(account));temporary.replace(account_file)
status,_=api('POST','/api/v1/auth/login',{'email':account['email'],'password':account['password']})
if status!=200:
 status,details=api('POST','/api/v1/auth/register',account)
 if status!=201:
  diagnostic=json.dumps(details)
  for sensitive in [account['password'],app['client_secret']]:diagnostic=diagnostic.replace(sensitive,'[redacted]')
  print(json.dumps({'status':'BLOCKED','reason':'isolated_bootstrap_registration_failed','http_status':status,
   'sanitized_error':diagnostic[:1500]}))
  raise SystemExit(2)
# Bootstrap the first operator of this new database only. Normal later admin
# promotions use the authenticated application endpoint and its audit workflow.
sql="UPDATE users SET is_system_admin=true WHERE email='feishu-v2-admin@example.invalid' AND username='feishu-v2-admin' RETURNING id;"
p=run(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres',
 '-d','knowhub_feishu_v2_test','-At'],input=sql)
ids=[value for value in p.stdout.splitlines() if re.fullmatch(r'[a-f0-9-]{36}',value)]
if p.returncode or len(ids)!=1:raise ValueError('isolated_first_admin_bootstrap_failed')
status,login=api('POST','/api/v1/auth/login',{'email':account['email'],'password':account['password']})
if status!=200 or not login.get('token') or not login.get('user',{}).get('is_system_admin'):
 raise ValueError('isolated_admin_login_failed')
token=login['token']
if (release/'bootstrap-ready.json').exists():
 ready=json.loads((release/'bootstrap-ready.json').read_text())
 print(json.dumps(ready));raise SystemExit(0)
status,_=api('PUT','/api/v1/system/admin/model-policy',{'embedding_model_id':'builtin-feishu-v2-embedding'},token)
if status!=200:raise ValueError('isolated_embedding_default_policy_failed')
status,_=api('PUT','/api/v1/system/admin/feishu',{
 'app_id':app['app_id'],'client_secret':app['client_secret'],'redirect_uri':'http://172.30.232.42/feishu/callback',
 'enabled':True,'auth_mode':'user_oauth','capabilities':{'application_enabled':True,'personal_enabled':True}},token)
if status!=200:raise ValueError('isolated_feishu_app_configuration_failed')
status,_=api('POST','/api/v1/system/admin/feishu/test',{},token)
if status!=200:raise ValueError('isolated_feishu_app_token_probe_failed')
status,connection=api('GET','/api/v1/me/feishu',token=token)
if status!=200:raise ValueError('isolated_feishu_connection_status_failed')
ready={'status':'PASS','source_sha':SHA,'project':project,'bootstrap_admin_id':ids[0],
 'default_embedding':'builtin-feishu-v2-embedding','dimensions':1024,'feishu_application_token_probe':'PASS',
 'personal_oauth':'NOT_EXECUTED','real_document_import':'NOT_EXECUTED',
 'callback_expires_at':environment['FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT'],
 'scope':'isolated_initial_configuration_only','existing_entry_changed':False}
private_write(release/'bootstrap-ready.json',json.dumps(ready,indent=2)+'\n')
app.clear();account.clear();login.clear();token=None
print(json.dumps(ready))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest',type=Path,required=True)
    parser.add_argument('--phase',choices=['prepare','bootstrap'],required=True)
    args=parser.parse_args();raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(32768));app=credential.pop('app')
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'isolated_site_bootstrap_failed'}))\n raise SystemExit(2)\n"
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\nPHASE='+repr(args.phase)+'\n'
        code=base64.b64encode((prefix+guarded).encode()).decode()
        incoming,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=240)
        incoming.write(json.dumps(app));incoming.channel.shutdown_write();app.clear()
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 0 if result.get('status') in ('PREPARED','PASS') else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_isolated_bootstrap_unavailable'}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
