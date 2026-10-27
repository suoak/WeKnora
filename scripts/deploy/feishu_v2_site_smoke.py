"""Check exact-SHA private previews, callback headers, authentication and live workers.

No real Feishu documents or user OAuth grants are used. The generated test admin
is read only inside the new private release directory; no credentials are printed.
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
import json,pathlib,re,subprocess,urllib.request,urllib.error
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':raise ValueError('unexpected_build_root')
release=root/'isolated-site';proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+BASE_SHA[:12]
if proof['source_sha']!=BASE_SHA or proof['project']!=project:raise ValueError('project_identity_mismatch')
if SHA!=BASE_SHA:
 upgrade=json.loads((release/('upgrade-'+SHA[:12])/'state.json').read_text())
 if upgrade.get('source_sha')!=SHA or upgrade.get('phase')!='RUNNING':raise ValueError('running_upgrade_required')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
def request(port,path,method='GET',body=None,token=None,limit=8*1024*1024):
 if port not in [28480,28481,28580,28581] or not path.startswith('/') or path.startswith('//'):
  raise ValueError('unexpected_smoke_target')
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 req=urllib.request.Request('http://127.0.0.1:'+str(port)+path,method=method,headers=headers,
  data=json.dumps(body).encode() if body is not None else None)
 try:
  with opener.open(req,timeout=15) as response:
   data=response.read(limit+1)
   if len(data)>limit:raise ValueError('smoke_response_limit')
   return response.status,dict(response.headers.items()),data
 except urllib.error.HTTPError as error:return error.code,dict(error.headers.items()),b''
checks=[]
for port,path,expected in [(28480,'/health',200),(28481,'/',200),(28580,'/',200),(28581,'/health',200),
 (28480,'/api/v1/me/feishu',401),(28480,'/api/v1/mcp/personal',401)]:
 status,headers,data=request(port,path)
 checks.append({'port':port,'path':path,'status':status,'expected':expected})
 if status!=expected:
  print(json.dumps({'status':'FAILED','scope':'private_preview_smoke','checks':checks}));raise SystemExit(2)
for port in [28481,28580]:
 status,headers,_=request(port,'/feishu/callback')
 normalized={key.lower():value.lower() for key,value in headers.items()}
 ok=status==200 and normalized.get('referrer-policy')=='no-referrer' and 'no-store' in normalized.get('cache-control','')
 checks.append({'port':port,'path':'/feishu/callback','callback_headers_valid':ok,'status':status})
 if not ok:raise ValueError('isolated_callback_response_headers_invalid')
_,_,html=request(28481,'/')
scripts=re.findall(rb'<script[^>]+src="(/assets/[A-Za-z0-9_.-]+\.js)"',html)
if not scripts:raise ValueError('frontend_entry_asset_not_found')
frontend_sha=False
for asset in scripts[:3]:
 status,_,body=request(28481,asset.decode())
 frontend_sha|=status==200 and SHA[:7].encode() in body
if not frontend_sha:
 command='grep -l -F '+SHA[:7]+' /usr/share/nginx/html/assets/*.js'
 found=subprocess.run(['docker','compose','--project-name',project,'--file',str(release/'compose.json'),
  'exec','-T','frontend','/bin/sh','-c',command],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=30)
 for filename in found.stdout.splitlines()[:5]:
  name=pathlib.PurePosixPath(filename).name
  if not re.fullmatch(r'[A-Za-z0-9_.-]+\.js',name):raise ValueError('unexpected_static_asset_name')
  status,_,body=request(28481,'/assets/'+name)
  frontend_sha|=status==200 and SHA[:7].encode() in body
if not frontend_sha:raise ValueError('frontend_build_identity_not_found')
account=json.loads((release/'bootstrap-user.json').read_text())
status,_,data=request(28480,'/api/v1/auth/login','POST',{'email':account['email'],'password':account['password']})
if status!=200:raise ValueError('isolated_smoke_admin_login_failed')
login=json.loads(data);token=login['token']
status,_,data=request(28480,'/api/v1/system/info',token=token)
if status!=200 or SHA not in data.decode('utf-8'):raise ValueError('backend_build_identity_not_found')
status,_,data=request(28480,'/api/v1/system/admin/runtime/queues',token=token)
if status!=200:raise ValueError('isolated_worker_registry_api_failed')
runtime=json.loads(data)
if not runtime.get('available'):raise ValueError('live_worker_registry_not_available')
pools=runtime.get('pools',[])
required={'core','maintenance','shared'}
observed={pool['name']:pool for pool in pools}
if not required.issubset(observed) or any(observed[name].get('instances')!=2 for name in required):
 print(json.dumps({'status':'FAILED','reason':'two_live_worker_instances_not_observed','pools':pools}))
 raise SystemExit(2)
result={'status':'PASS','source_sha':SHA,'project':project,'scope':'synthetic_isolated_startup_only',
 'checks':checks,'backend_sha_verified':True,'frontend_display_commit':SHA[:7],
 'live_worker_pools':pools,'real_personal_oauth':'NOT_EXECUTED','real_document_search':'NOT_EXECUTED',
 'existing_entry_changed':False}
(release/'startup-smoke.json').write_text(json.dumps(result,indent=2)+'\n')
token=None;login.clear();account.clear()
print(json.dumps(result))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--manifest',type=Path,required=True)
    parser.add_argument('--candidate-manifest',type=Path)
    args=parser.parse_args();raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(16384))
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'isolated_smoke_failed'}))\n raise SystemExit(2)\n"
        expected=sha
        if args.candidate_manifest:
            payload=args.candidate_manifest.read_bytes()
            candidate=json.loads(payload.decode('utf-16') if payload.startswith((b'\xff\xfe',b'\xfe\xff')) else payload.decode('utf-8-sig'))
            expected=candidate['source_sha']
            if not re.fullmatch('[a-f0-9]{40}',expected):raise ValueError('invalid_candidate_sha')
        code=base64.b64encode(('ROOT='+repr(str(root))+'\nSHA='+repr(expected)+'\nBASE_SHA='+repr(sha)+'\n'+guarded).encode()).decode()
        _,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=180)
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 0 if result.get('status')=='PASS' else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_private_smoke_unavailable'}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
