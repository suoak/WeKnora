"""Isolated old-image compatibility probe; never an exposed APP downgrade.

Restores synthetic backup into a NEW DB, uses new empty Redis and no host ports.
File volume is read-only. Old-image inability or loss of denial is a rollback
limitation, not authorization to drop the hardened reader in production.
"""
from pathlib import Path
import hashlib,json,os,subprocess,sys,time,urllib.parse
base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
instance=json.loads((base/'results/deploy/instance.json').read_text());project=instance['project']
assert project.startswith('knowhub-v1-feishu-')
source=json.loads((base/'feishu-source-current.json').read_text())
assert json.loads((base/'results'/source['results_dir']/'result.json').read_text())['exit_code']==0
out=base/'results/old-image-offline-probe';out.mkdir(exist_ok=False)
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
old='knowhub-rc3/app:0.8.0-oa-feishu-rc3-1e2ded5b6'
image=json.loads(subprocess.check_output(['docker','image','inspect',old]))[0]
assert image['Id']=='sha256:0c84cac5af5103b73102c6261f32130d3e51eeed1b8cbf77fda61de20c4bd983'
app=project+'-old-offline-probe';redis=project+'-old-offline-redis';db='knowhub_old_offline_probe'
network=project+'_isolated';rows=[];started=[]
env=dict(x.split('=',1)for x in(base/'private/app.env').read_text().splitlines())
reference=Path(source['revoked_file_reference']).resolve();assert reference.parent==base/'private'
link=reference.read_text();assert hashlib.sha256(link.encode()).hexdigest()==source['revoked_file_sha256']
assert int(urllib.parse.parse_qs(urllib.parse.urlsplit(link).query)['expires'][0])>time.time()
def run(name,args,stdin=None,timeout=240,require=True):
 r=subprocess.run(args,input=stdin,capture_output=True,timeout=timeout)
 raw=r.stdout+r.stderr
 for secret in list(env.values())+[link]:
  if len(secret)>12:raw=raw.replace(secret.encode(),b'[REDACTED_SYNTHETIC_CREDENTIAL]')
 (out/(name+'.log')).write_bytes(raw)
 rows.append(dict(name=name,argv=args,exit_code=r.returncode,log_credentials_redacted=True))
 (out/'commands.json').write_text(json.dumps(rows,indent=2))
 if require:assert r.returncode==0,name
 return r
result={}
try:
 run('create-synthetic-clone',compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE '+db+' TEMPLATE template0'])
 backup=Path(source['backup']).resolve();assert backup.parent==base/'private'
 run('restore-synthetic-clone',compose+['exec','-T','postgres','pg_restore','-U','postgres','-d',db],backup.read_bytes())
 run('start-empty-private-redis',['docker','run','-d','--name',redis,'--label','knowhub.v1=old-image-probe','--network',network,'redis:7.0-alpine','redis-server','--save','','--appendonly','no']);started.append(redis)
 args=['docker','run','-d','--name',app,'--label','knowhub.v1=old-image-probe','--network',network,'--env-file',str(base/'private/app.env'),'-e','DB_DRIVER=postgres','-e','DB_HOST=postgres','-e','DB_PORT=5432','-e','DB_USER=postgres','-e','DB_NAME='+db,'-e','REDIS_ADDR='+redis+':6379','-e','REDIS_PASSWORD=','-e','DUCKDB_SKIP_EXTENSION_LOAD=1','-e','AUTO_RECOVER_DIRTY=false','-e','FEISHU_LEGACY_SYNC_SUSPENDED=true','-e','FEISHU_SAFE_INCREMENTAL_ENABLED=false','-e','STORAGE_TYPE=local','-e','LOCAL_STORAGE_BASE_DIR=/data/files','-v',project+'_files:/data/files:ro','--entrypoint','/app/WeKnora',old]
 run('start-unexposed-old-image',args);started.append(app)
 health=None
 for _ in range(45):
  state=json.loads(subprocess.check_output(['docker','inspect',app]))[0]
  if not state['State']['Running']:break
  r=subprocess.run(['docker','exec',app,'curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if r.returncode==0 and r.stdout==b'200':health=200;break
  time.sleep(1)
 state=json.loads(subprocess.check_output(['docker','inspect',app]))[0]
 assert not state['HostConfig']['PortBindings'] and state['HostConfig']['NetworkMode']==network
 file_mounts=[m for m in state['Mounts'] if m['Destination']=='/data/files']
 assert len(file_mounts)==1 and file_mounts[0]['Name']==project+'_files' and not file_mounts[0]['RW']
 file_status=None
 if health==200:
  config=('url="http://127.0.0.1:8080'+link+'"\noutput="/dev/null"\nsilent\nmax-time=10\nwrite-out="%{http_code}"\n').encode()
  r=run('old-image-unexpired-file-status',['docker','exec','-i',app,'curl','--config','-'],config,require=False)
  file_status=r.stdout.decode().strip()
 run('old-image-startup-log',['docker','logs',app],require=False)
 result=dict(exit_code=0,candidate_sha256=instance['candidate_sha256'],probe='completed isolated compatibility observation; NOT old rollback approval',old_image=old,old_image_id=image['Id'],duckdb_extensions='offline_startup_option_excel_NOT_TESTED',old_image_source_head='UNKNOWN_binary_not_relabelled_as_current_HEAD',schema_clone=3036,health=health,old_file_http_status=file_status,drop_in_rollback='UNSAFE' if file_status=='200' else 'NOT_VERIFIED_SAFE',old_startup_exit_code=state['State']['ExitCode'],no_host_ports=True,new_synthetic_DB=True,new_empty_Redis=True,files_readonly=True,schema_down=False,production_changes=False,recommended_rollback='retain hardened APP/schema, close MCP/safe-sync and suspend legacy sync; old image requires maintenance isolation and separate data/state reconciliation')
except Exception as error:
 result=dict(exit_code=1,error_class=type(error).__name__,message=str(error),production_changes=False)
finally:
 for name in reversed(started):run('stop-'+name,['docker','stop','--time','5',name],require=False)
 (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
