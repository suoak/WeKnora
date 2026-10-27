"""Renew the exact isolated HTTP callback for six hours and request user scope.

No OAuth grant, source hold, sync permit, Redis latch or publication is written.
"""
import sys

import feishu_v2_docreader_service as transport

REMOTE = r'''
import datetime,hashlib,json,os,pathlib,subprocess
root=pathlib.Path(ROOT);source=pathlib.Path(SOURCE);site=root/'isolated-site'
project='knowhub-feishu-v2-'+BASE_SHA[:12]
if MODE!='enable-updates' or project!='knowhub-feishu-v2-d6e99d894d3a':raise ValueError('isolated_operation_required')
if root.is_symlink() or source.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:
 raise ValueError('project_mismatch')
if SHA!='56fc87ee576098cd326ce4331189edbf0c0e2f63':raise ValueError('verified_candidate_required')
compose_path=site/'compose.json';compose=['docker','compose','--project-name',project,'--file',str(compose_path)]
def checked(args,timeout=30):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
 if p.returncode:raise ValueError('isolated_configuration_operation_failed')
 return p.stdout.strip()
for name in ['app','worker2']:
 record=json.loads(checked(['docker','inspect',checked(compose+['ps','--quiet',name])]))[0]
 if record['Config']['Labels'].get('knowhub.source-sha')!=SHA:raise ValueError('deployed_source_mismatch')
env_path=site/'app.env';before=env_path.read_bytes()
env=dict(line.split('=',1) for line in before.decode().splitlines() if '=' in line)
if env.get('KNOWHUB_ENVIRONMENT')!='isolated-test' or env.get('FEISHU_TEST_HTTP_CALLBACK_ENABLED')!='1' or env.get('FEISHU_TEST_HTTP_CALLBACK_URL')!='http://172.30.232.42/feishu/callback':raise ValueError('exact_isolated_callback_required')
work=source.parent/'personal-scope-configuration';work.mkdir(mode=0o700,exist_ok=True)
if work.is_symlink():raise ValueError('unexpected_configuration_directory')
receipt=work/'receipt.json'
if receipt.exists():
 result=json.loads(receipt.read_text())
 if result['environment_sha256']!=hashlib.sha256(before).hexdigest():raise ValueError('configuration_drift')
 print(json.dumps(result));raise SystemExit(0)
now=datetime.datetime.now(datetime.timezone.utc)
previous=datetime.datetime.fromisoformat(env['FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT'])
if previous>now:raise ValueError('expired_callback_required')
until=(now+datetime.timedelta(hours=6)).isoformat()
updates={'FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT':until,'FEISHU_PERSONAL_PERMISSION_AUTH_SCOPE_ENABLED':'true'}
lines=[]
for line in before.decode().splitlines():
 key=line.partition('=')[0]
 lines.append(key+'='+updates.pop(key) if key in updates else line)
lines.extend(key+'='+value for key,value in updates.items())
after=('\n'.join(lines)+'\n').encode()
def private_write(path,payload):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'wb') as out:out.write(payload)
private_write(work/'app.before.env',before)
private_write(work/'app.after.env',after)
checked(compose+['stop','--timeout','45','app','worker2'],120)
try:
 env_path.write_bytes(after);env_path.chmod(0o600)
 checked(compose+['up','-d','--no-build','--pull','never','app','worker2'],180)
except Exception:
 env_path.write_bytes(before)
 checked(compose+['up','-d','--no-build','--pull','never','app','worker2'],180)
 raise
result={'status':'CONFIGURED','source_sha':SHA,'at':now.isoformat(),
 'previous_callback_expiry':previous.isoformat(),'callback_expires_at':until,
 'permission_auth_scope_requested':True,'environment_sha256':hashlib.sha256(after).hexdigest(),
 'scope':'existing_isolated_callback_only','oauth_grants_modified':False,
 'sync_locks_modified':False,'source_holds_modified':False,'publication_modified':False}
private_write(receipt,(json.dumps(result,indent=2)+'\n').encode());print(json.dumps(result))
'''

if __name__ == '__main__':
    transport.REMOTE = REMOTE
    raise SystemExit(transport.main())
