"""Execute bounded infrastructure, migration and restore checks on the new test project.

Starting application processes requires exact-SHA success evidence for every
release gate. No operation exposes public ports or changes the existing entry.
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
import hashlib,json,os,pathlib,subprocess
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
release=root/'isolated-site'
proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+SHA[:12]
if proof['source_sha']!=SHA or proof['project']!=project or release.is_symlink():
 raise ValueError('isolated_project_identity_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(release/'compose.json')]
def run(args,timeout=45,input=None):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,input=input,text=True,timeout=timeout)
def compose_run(args,timeout=45): return run(compose+args,timeout)
def command_failure(reason,process):
 message=(process.stdout+process.stderr)[-4000:]
 for line in (release/'app.env').read_text().splitlines():
  _,_,value=line.partition('=')
  if len(value)>=8: message=message.replace(value,'[redacted]')
 print(json.dumps({'status':'BLOCKED','reason':reason,'sanitized_diagnostic':message}))
 raise SystemExit(2)
def container(service):
 p=compose_run(['ps','--all','--quiet',service])
 if p.returncode or not p.stdout.strip(): return None
 p=run(['docker','inspect',p.stdout.strip()])
 if p.returncode: raise ValueError('isolated_container_unavailable')
 record=json.loads(p.stdout)[0]
 labels=record['Config'].get('Labels',{})
 if labels.get('com.docker.compose.project')!=project or labels.get('com.docker.compose.service')!=service:
  raise ValueError('container_project_identity_mismatch')
 return record
def sql(statement):
 p=run(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres',
  '-d','knowhub_feishu_v2_test','-At'],input=statement)
 if p.returncode: raise ValueError('isolated_database_query_failed')
 return p.stdout.strip()
def snapshot(name):
 backups=release/'backups'
 backups.mkdir(mode=0o700,exist_ok=True)
 path=backups/(name+'.dump')
 evidence=backups/(name+'.json')
 if path.is_symlink() or evidence.is_symlink(): raise ValueError('unexpected_backup_path')
 if path.exists():
  expected=json.loads(evidence.read_text())
  if expected['sha256']!=hashlib.sha256(path.read_bytes()).hexdigest(): raise ValueError('existing_backup_mismatch')
  return expected
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'wb') as output:
  p=subprocess.run(compose+['exec','-T','postgres','pg_dump','-U','postgres','-Fc','--no-owner','--no-acl',
   'knowhub_feishu_v2_test'],stdout=output,stderr=subprocess.PIPE,timeout=120)
 if p.returncode: raise ValueError('isolated_database_backup_failed')
 # Validate the actual archive via stdin; never read or dump any other project.
 with path.open('rb') as incoming:
  checked=subprocess.run(compose+['exec','-T','postgres','pg_restore','--list'],stdin=incoming,
   stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=60)
 if checked.returncode: raise ValueError('backup_archive_validation_failed')
 result={'source_sha':SHA,'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'bytes':path.stat().st_size,
  'database':'knowhub_feishu_v2_test','scope':'this_new_compose_project_only'}
 fd=os.open(str(evidence),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as out: json.dump(result,out,indent=2)
 return result
result={'source_sha':SHA,'project':project,'phase':PHASE,'existing_entry_changed':False,'public_ports_exposed':False}
if PHASE=='infra':
 for name in ['postgres','redis']:
  volume=project+'_'+name
  p=run(['docker','volume','inspect',volume])
  if p.returncode==0 and json.loads(p.stdout)[0].get('Labels',{}).get('com.docker.compose.project')!=project:
   raise ValueError('existing_volume_not_owned_by_project')
 p=compose_run(['up','-d','--no-build','--pull','never','postgres','redis'],90)
 if p.returncode: command_failure('isolated_infrastructure_start_failed',p)
 pg=container('postgres')
 if pg['State'].get('Health',{}).get('Status')!='healthy':
  result['status']='STARTING'
 else:
  tables=sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;")
  # ParadeDB's fresh template can contain extension-owned tables such as
  # spatial_ref_sys. Only those are permitted before application migrations;
  # any ordinary table (including an old KnowHub schema) fails the check.
  unowned=int(sql("""SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
   WHERE n.nspname='public' AND c.relkind IN ('r','p') AND NOT EXISTS
   (SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid
    AND d.refclassid='pg_extension'::regclass AND d.deptype='e');"""))
  if not (release/'backups/before-migration.dump').exists() and unowned:
   raise ValueError('unexpected_application_tables_before_first_backup')
  result['initial_public_tables']=tables.splitlines()
  result['backup']=snapshot('before-migration')
  result['status']='PASS'
elif PHASE=='start':
 required={'v2','native','integrated','portal','python_mcp'}
 if not isinstance(GATES,dict) or set(GATES)!=required: raise ValueError('complete_release_gate_evidence_required')
 for gate in GATES.values():
  if gate.get('headSha')!=SHA or gate.get('status')!='completed' or gate.get('conclusion')!='success':
   raise ValueError('release_gates_not_passed_for_this_source')
 if not (release/'backups/before-migration.json').is_file(): raise ValueError('empty_database_backup_required')
 p=compose_run(['up','-d','--no-build','--pull','never','app'],90)
 if p.returncode: command_failure('isolated_application_start_failed',p)
 record=container('app')
 result['status']='PASS' if record['State'].get('Health',{}).get('Status')=='healthy' else 'STARTING'
 result['health']=record['State'].get('Health',{}).get('Status','starting')
elif PHASE=='restore':
 record=container('app')
 if not record or record['State'].get('Health',{}).get('Status')!='healthy':
  raise ValueError('healthy_migrated_test_app_required')
 state=sql('SELECT version::text||\':\'||dirty::text FROM schema_migrations;')
 if state!='3042:false': raise ValueError('unexpected_or_dirty_migration_state')
 result['migration_state']=state
 backup=snapshot('after-migration')
 restored=release/'restore-verification.json'
 if restored.exists():
  previous=json.loads(restored.read_text())
  if previous.get('backup_sha256')!=backup['sha256']: raise ValueError('restore_evidence_mismatch')
  result['restore']=previous
 else:
  restore_database='knowhub_feishu_v2_restore'+('' if RESTORE_ATTEMPT==1 else '_'+str(RESTORE_ATTEMPT))
  p=run(compose+['exec','-T','postgres','createdb','-U','postgres','--template=template0',restore_database])
  if p.returncode: raise ValueError('new_restore_database_creation_failed')
  with (release/'backups/after-migration.dump').open('rb') as incoming:
   p=subprocess.run(compose+['exec','-T','postgres','pg_restore','--exit-on-error','--no-owner','--no-acl',
    '-U','postgres','-d',restore_database],stdin=incoming,stdout=subprocess.DEVNULL,
    stderr=subprocess.PIPE,timeout=180)
  if p.returncode:
   message=p.stderr.decode('utf-8',errors='replace')
   lines=[line for line in message.splitlines() if 'pg_restore: error:' in line or 'ERROR:' in line]
   for line in (release/'app.env').read_text().splitlines():
    _,_,value=line.partition('=')
    if len(value)>=8:lines=[entry.replace(value,'[redacted]') for entry in lines]
   print(json.dumps({'status':'BLOCKED','reason':'isolated_database_restore_failed',
    'restore_database':restore_database,'sanitized_errors':lines[:12]}))
   raise SystemExit(2)
  query="SELECT version::text||':'||dirty::text FROM schema_migrations;"
  p=run(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres',
   '-d',restore_database,'-At'],input=query)
  if p.returncode or p.stdout.strip()!=state: raise ValueError('restored_migration_state_mismatch')
  count_query="SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';"
  expected=sql(count_query)
  p=run(compose+['exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres',
   '-d',restore_database,'-At'],input=count_query)
  if p.returncode or p.stdout.strip()!=expected: raise ValueError('restored_schema_table_count_mismatch')
  previous={'status':'PASS','source_sha':SHA,'backup_sha256':backup['sha256'],'migration_state':state,
   'public_table_count':int(expected),'restore_database':restore_database,
   'scope':'isolated_schema_and_initial_data_restore_only'}
  fd=os.open(str(restored),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'w') as out: json.dump(previous,out,indent=2)
  result['restore']=previous
 p=compose_run(['up','-d','--no-build','--pull','never','worker2','frontend'],90)
 if p.returncode: raise ValueError('isolated_second_worker_start_failed')
 result['status']='PASS'
else:
 result['status']='STATUS_ONLY'
result['containers']={}
for service in ['postgres','redis','app','worker2','frontend']:
 record=container(service)
 if record:
  result['containers'][service]={'image':record['Image'],'status':record['State']['Status'],
   'health':record['State'].get('Health',{}).get('Status')}
print(json.dumps(result))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--phase', choices=['infra','start','restore','status'], default='status')
    parser.add_argument('--gates', type=Path)
    parser.add_argument('--restore-attempt',type=int,choices=range(1,6),default=1)
    args = parser.parse_args()
    def read_json(path):
        raw = path.read_bytes()
        return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    manifest = read_json(args.manifest)
    sha = manifest['source_sha']
    root = PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):
        raise SystemExit('invalid staged source manifest')
    gates = read_json(args.gates) if args.gates else None
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx': raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home()/'.ssh'/'known_hosts'
        if known.is_file(): client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\nPHASE='+repr(args.phase)+'\nGATES='+repr(gates)+'\n'
        prefix+='RESTORE_ATTEMPT='+repr(args.restore_attempt)+'\n'
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'isolated_site_preflight_failed'}))\n raise SystemExit(2)\n"
        code=base64.b64encode((prefix+guarded).encode()).decode()
        _,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=360)
        result=json.loads(out.read(65536))
        print(json.dumps(result,indent=2))
        return 2 if result.get('status')=='BLOCKED' else 0
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_test_site_preflight_unavailable'}))
        return 2
    finally:
        client.close()


if __name__=='__main__':
    sys.exit(main())
