"""Upgrade only the existing isolated test project, preserving users and OAuth.

Requires five passing exact-source gates, verified images, a stopped-worker
database snapshot and a successful restore. Original public entry is untouched.
Credentials are read through stdin; reports never include environment values.
"""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import hashlib,json,os,pathlib,subprocess
base=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
root=pathlib.Path(ROOT); candidate=pathlib.Path(CANDIDATE)
for directory in [root,candidate]:
 if directory.is_symlink() or directory.resolve().parent!=base:raise ValueError('unexpected_release_root')
site=root/'isolated-site'
if site.is_symlink():raise ValueError('unexpected_site_directory')
proof=json.loads((site/'preparation.json').read_text())
project='knowhub-feishu-v2-'+OLD_SHA[:12]
if proof['source_sha']!=OLD_SHA or proof['project']!=project:raise ValueError('isolated_project_mismatch')
compose_path=site/'compose.json';env_path=site/'app.env'
compose=['docker','compose','--project-name',project,'--file',str(compose_path)]
def run(args,timeout=60,input=None):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,input=input,timeout=timeout)
def checked(args,timeout=60,input=None):
 p=run(args,timeout,input)
 if p.returncode:raise ValueError('isolated_operation_failed:'+args[0])
 return p.stdout.strip()
def inspect(value):return json.loads(checked(['docker','inspect',value]))[0]
def container(name):
 record=inspect(checked(compose+['ps','--all','--quiet',name]))
 if record['Config'].get('Labels',{}).get('com.docker.compose.project')!=project:raise ValueError('container_project_mismatch')
 return record
def sql(query,database='knowhub_feishu_v2_test'):
 return checked(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',database,'-At'],input=query)
def private_write(path,data):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'wb') as out:out.write(data)
upgrade=site/('upgrade-'+SHA[:12])
upgrade.mkdir(mode=0o700,exist_ok=True)
if upgrade.is_symlink():raise ValueError('unexpected_upgrade_directory')
state_path=upgrade/'state.json'
state=json.loads(state_path.read_text()) if state_path.exists() else {'source_sha':SHA,'previous_sha':OLD_SHA,'project':project,'phase':'NEW'}
if state['source_sha']!=SHA or state['previous_sha']!=OLD_SHA:raise ValueError('upgrade_identity_mismatch')
def save():
 temporary=upgrade/'state.tmp'
 with temporary.open('w') as out:json.dump(state,out,indent=2)
 temporary.chmod(0o600);temporary.replace(state_path)
if MODE=='prepare':
 if set(GATES)!={'v2','native','integrated','portal','python_mcp'}:raise ValueError('all_release_gates_required')
 for gate in GATES.values():
  if gate.get('headSha')!=SHA or gate.get('status')!='completed' or gate.get('conclusion')!='success':
   raise ValueError('exact_source_gates_not_passed')
 app=(candidate/'runtime-image.id').read_text().strip()
 if inspect(app)['Config'].get('Labels',{}).get('org.opencontainers.image.revision')!=SHA:raise ValueError('app_provenance_mismatch')
 package=candidate/'frontend-package'
 manifest=json.loads((package/'manifest.json').read_text())
 archive=package/'frontend-image.tar.gz'
 if manifest['source_sha']!=SHA or hashlib.sha256(archive.read_bytes()).hexdigest()!=manifest['archive_sha256']:
  raise ValueError('frontend_archive_mismatch')
 if run(['docker','image','inspect',manifest['image_id']]).returncode:
  checked(['docker','load','--input',str(archive)],180)
 if inspect(manifest['image_id'])['Config'].get('Labels',{}).get('org.opencontainers.image.revision')!=SHA:
  raise ValueError('frontend_provenance_mismatch')
 current=json.loads(compose_path.read_text())
 for service in ['app','worker2','frontend','entry','entry-preview']:
  record=container(service)
  if record['Image']!=current['services'][service]['image']:raise ValueError('deployed_image_drift')
 if state['phase']=='NEW':
  private_write(upgrade/'compose.before.json',compose_path.read_bytes())
  private_write(upgrade/'app.before.env',env_path.read_bytes())
  state.update(phase='PREPARED',app_image=app,frontend_image=manifest['image_id'],gates=GATES)
  save()
elif MODE=='apply':
 if state['phase']!='PREPARED':raise ValueError('prepared_upgrade_required')
 if compose_path.read_bytes()!=(upgrade/'compose.before.json').read_bytes() or env_path.read_bytes()!=(upgrade/'app.before.env').read_bytes():
  raise ValueError('site_configuration_changed_since_prepare')
 checked(compose+['stop','--timeout','45','entry','app','worker2'],120)
 try:
  if sql("SELECT version::text||':'||dirty::text FROM schema_migrations;")!='3041:false':raise ValueError('unexpected_schema')
  backup=upgrade/'before-upgrade.dump'
  fd=os.open(str(backup),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'wb') as output:
   p=subprocess.run(compose+['exec','-T','postgres','pg_dump','-U','postgres','-Fc','--no-owner','--no-acl','knowhub_feishu_v2_test'],stdout=output,stderr=subprocess.PIPE,timeout=180)
  if p.returncode:raise ValueError('upgrade_backup_failed')
  restored='feishu_upgrade_restore_'+SHA[:12]
  checked(compose+['exec','-T','postgres','createdb','-U','postgres','--template=template0',restored])
  with backup.open('rb') as incoming:
   p=subprocess.run(compose+['exec','-T','postgres','pg_restore','--exit-on-error','--no-owner','--no-acl','-U','postgres','-d',restored],stdin=incoming,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=180)
  if p.returncode:raise ValueError('upgrade_restore_failed')
  counts={}
  for table in ['users','feishu_connections','feishu_private_kbs','feishu_personal_imports','knowledges']:
   query='SELECT count(*) FROM '+table+';'
   actual=sql(query)
   if sql(query,restored)!=actual:raise ValueError('restored_business_counts_mismatch')
   counts[table]=int(actual)
  plan=json.loads((upgrade/'compose.before.json').read_text())
  for service in ['app','worker2']:
   plan['services'][service]['image']=state['app_image']
   plan['services'][service].setdefault('labels',{})['knowhub.source-sha']=SHA
  for service in ['frontend','entry','entry-preview']:
   plan['services'][service]['image']=state['frontend_image']
   plan['services'][service].setdefault('labels',{})['knowhub.source-sha']=SHA
  newenv=[]
  for line in (upgrade/'app.before.env').read_text().splitlines():
   if line.startswith('FEISHU_SYNC_SOURCE_SHA='):line='FEISHU_SYNC_SOURCE_SHA='+SHA
   newenv.append(line)
  private_write(upgrade/'compose.after.json',(json.dumps(plan,indent=2)+'\n').encode())
  private_write(upgrade/'app.after.env',('\n'.join(newenv)+'\n').encode())
  compose_path.write_bytes((upgrade/'compose.after.json').read_bytes())
  env_path.write_bytes((upgrade/'app.after.env').read_bytes())
  checked(compose+['config','--quiet'])
  state.update(phase='APPLIED',backup_sha256=hashlib.sha256(backup.read_bytes()).hexdigest(),restore_database=restored,business_counts=counts)
  save()
  checked(compose+['up','-d','--no-build','--pull','never','app'],120)
 except Exception:
  compose_path.write_bytes((upgrade/'compose.before.json').read_bytes())
  env_path.write_bytes((upgrade/'app.before.env').read_bytes())
  run(compose+['up','-d','--no-build','--pull','never','app','worker2','frontend','entry'],180)
  state['phase']='FAILED_OLD_CONFIGURATION_RESTORED';save()
  raise
elif MODE=='finish':
 if state['phase'] not in ['APPLIED','RUNNING']:raise ValueError('applied_upgrade_required')
 record=container('app')
 if record['Image']!=state['app_image'] or record['State'].get('Health',{}).get('Status')!='healthy':raise ValueError('new_app_not_healthy')
 if sql("SELECT version::text||':'||dirty::text FROM schema_migrations;")!='3041:false':raise ValueError('migration_drift')
 checked(compose+['up','-d','--no-build','--pull','never','worker2','frontend','entry-preview','entry'],180)
 records={name:container(name) for name in ['app','worker2','frontend','entry']}
 ready=all(r['State']['Running'] for r in records.values()) and all(records[n]['State'].get('Health',{}).get('Status')=='healthy' for n in ['app','worker2'])
 state['phase']='RUNNING' if ready else 'APPLIED';save()
elif MODE=='rollback':
 if state['phase'] not in ['APPLIED','RUNNING']:raise ValueError('applied_upgrade_required')
 checked(compose+['stop','--timeout','45','entry','app','worker2'],120)
 compose_path.write_bytes((upgrade/'compose.before.json').read_bytes())
 env_path.write_bytes((upgrade/'app.before.env').read_bytes())
 checked(compose+['up','-d','--no-build','--pull','never','app','worker2','frontend','entry-preview','entry'],180)
 state['phase']='ROLLED_BACK';save()
else:
 state['containers']={}
 for name in ['app','worker2','frontend','entry']:
  r=container(name)
  state['containers'][name]={'image':r['Image'],'running':r['State']['Running'],'health':r['State'].get('Health',{}).get('Status')}
 if TEST_USERNAME:
  if not __import__('re').fullmatch('[A-Za-z0-9_.@-]{1,64}',TEST_USERNAME):raise ValueError('invalid_test_username')
  query="SELECT json_build_object('exists',count(*)>0,'active',coalesce(bool_or(u.is_active),false),'connection_record_present',coalesce(bool_or(EXISTS(SELECT 1 FROM feishu_connections c WHERE c.user_id=u.id)),false)) FROM users u WHERE u.username='"+TEST_USERNAME+"' AND u.deleted_at IS NULL;"
  state['test_account']=json.loads(sql(query))
print(json.dumps({'status':state['phase'],'source_sha':SHA,'project':project,'backup_sha256':state.get('backup_sha256'),
 'business_restore_verified':bool(state.get('restore_database')),'containers':state.get('containers'),'test_account':state.get('test_account'),
 'scope':'existing_isolated_project_only','secrets_rotated':False,'cursor_reset':False}))
'''


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--candidate-manifest', type=Path, required=True)
    parser.add_argument('--release-gates', type=Path)
    parser.add_argument('--test-username')
    parser.add_argument('--mode', choices=['prepare', 'apply', 'finish', 'status', 'rollback'], required=True)
    args = parser.parse_args()
    old, new = read_json(args.manifest), read_json(args.candidate_manifest)
    for manifest in [old, new]:
        sha = manifest['source_sha']
        root = PurePosixPath(manifest['source_directory']).parent
        if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
                or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
            raise SystemExit('invalid source manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host') != '172.30.232.42' or credential.get('user') != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        values = {'ROOT': str(PurePosixPath(old['source_directory']).parent), 'CANDIDATE': str(PurePosixPath(new['source_directory']).parent),
                  'OLD_SHA': old['source_sha'], 'SHA': new['source_sha'], 'MODE': args.mode,
                  'GATES': read_json(args.release_gates) if args.release_gates else {}, 'TEST_USERNAME':args.test_username}
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'isolated_upgrade_operation_failed','error_type':type(error).__name__}))\n raise SystemExit(2)\n"
        code = base64.b64encode(('\n'.join(k + '=' + repr(v) for k, v in values.items()) + '\n' + guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=600)
        result = json.loads(out.read(65536))
        print(json.dumps(result, indent=2))
        return 2 if result.get('status') == 'BLOCKED' else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException) as error:
        print(json.dumps({'status': 'BLOCKED', 'reason': 'verified_isolated_upgrade_unavailable', 'error_type': type(error).__name__}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
