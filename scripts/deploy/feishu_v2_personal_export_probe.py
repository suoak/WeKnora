"""Run the source-pinned personal export diagnostic in the isolated network.

Source documents and Knowledge are read-only. The existing UserToken service may
rotate its own persisted OAuth grant; Feishu may create temporary export jobs.
The separate health mode audits application scopes only, without document calls.
No identity fallback, publication, worker start or sync-control mutation.
"""
import sys

import feishu_v2_docreader_service as transport

REMOTE = r'''
import json,pathlib,subprocess,hashlib,os,tempfile,re
root=pathlib.Path(ROOT);source=pathlib.Path(SOURCE);site=root/'isolated-site'
project='knowhub-feishu-v2-'+BASE_SHA[:12]
if root.is_symlink() or source.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:
 raise ValueError('isolated_project_mismatch')
source_proof=json.loads((source.parent/'source-manifest.json').read_text())
if source_proof['source_sha']!=SHA or source_proof['source_directory']!=str(source):raise ValueError('source_mismatch')
binary=source/'feishu-diagnostic';receipt=source/'candidate-diagnostic.sha256'
if binary.is_symlink() or not binary.is_file() or not receipt.is_file():raise ValueError('diagnostic_build_missing')
digest=hashlib.sha256(binary.read_bytes()).hexdigest()
if receipt.read_text().split()[0]!=digest:raise ValueError('diagnostic_build_checksum_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
def run(args,timeout=30):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
 if p.returncode:raise ValueError('isolated_probe_operation_failed')
 return p.stdout
container=run(compose+['ps','--quiet','app']).strip()
app=json.loads(run(['docker','inspect',container]))[0]
if app['Config']['Labels'].get('com.docker.compose.project')!=project:raise ValueError('app_project_mismatch')
environment=dict(v.split('=',1) for v in app['Config']['Env'] if '=' in v)
if environment.get('DB_NAME')!='knowhub_feishu_v2_test':raise ValueError('database_scope_mismatch')
environment['FEISHU_DIAGNOSTIC_ISOLATED']='1'
environment['FEISHU_DIAGNOSTIC_APP_SCOPES_ONLY']='1' if MODE=='health' else '0'
network=list(app['NetworkSettings']['Networks'])
if len(network)!=1:raise ValueError('ambiguous_network')
name='knowhub-feishu-personal-diagnostic-'+SHA[:12]+'-'+MODE
if subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
 raise ValueError('diagnostic_already_exists')
handle,path=tempfile.mkstemp(prefix='personal-diagnostic-',dir=str(site));os.chmod(path,0o600)
try:
 with os.fdopen(handle,'w') as out:
  for key,value in environment.items():
   if '\n' in value or '\r' in value:raise ValueError('invalid_environment')
   out.write(key+'='+value+'\n')
 args=['docker','run','--name',name,'--label','knowhub.environment=isolated-test',
  '--label','knowhub.source-sha='+SHA,'--network',network[0],'--env-file',path,
  '--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true',
  '--memory','768m','--cpus','1','--pids-limit','128','--tmpfs','/tmp:rw,nosuid,nodev,size=32m',
  '--volume',str(binary)+':/diagnostic:ro','--entrypoint','/diagnostic',app['Image']]
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=270)
 if p.returncode:raise ValueError('personal_export_diagnostic_failed')
 lines=[v for v in p.stdout.splitlines() if v.startswith('{')]
 if len(lines)!=1:raise ValueError('diagnostic_receipt_missing')
 result=json.loads(lines[0]);result['diagnostic_source_sha']=SHA;result['binary_sha256']=digest
 print(json.dumps(result))
finally:
 os.unlink(path)
'''

if __name__ == '__main__':
    if '--mode' not in sys.argv or sys.argv[sys.argv.index('--mode') + 1] not in ['status','health']:
        raise SystemExit('status probes personal export; health audits application scopes only')
    transport.REMOTE = REMOTE
    raise SystemExit(transport.main())
