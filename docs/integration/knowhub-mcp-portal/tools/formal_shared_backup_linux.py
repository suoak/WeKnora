"""Approved P3 test-only backup/restore; no production endpoint or cloud call."""
from pathlib import Path
import datetime,hashlib,json,os,re,shutil,subprocess,sys

base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert re.fullmatch(r'[a-z0-9-]+',attempt)
out=base/'results'/('backup'if attempt=='initial'else'backup-'+attempt);out.mkdir(parents=True,exist_ok=False);rows=[]
assert '172.30.232.42'in subprocess.check_output(['hostname','-I']).decode().split()
candidate=Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb')
app='knowhub-mvp-p3-app-1';front='knowhub-mvp-p3-frontend-1';pg='WeKnora-postgres';db='WeKnora'
def inspect(n):return json.loads(subprocess.check_output(['docker','inspect',n]))[0]
a=inspect(app);f=inspect(front);env=dict(v.split('=',1)for v in a['Config']['Env']if'='in v)
assert env['DB_HOST']==pg and env['DB_NAME']==db and a['Config']['Labels']['com.docker.compose.project']=='knowhub-mvp-p3'
assert inspect(pg)['NetworkSettings']['Networks'].get('weknora_WeKnora-network')is not None
assert not inspect(pg)['HostConfig'].get('NetworkMode')=='host'
secrets=[v.encode()for k,v in env.items()if any(s in k for s in ['PASSWORD','SECRET','KEY'])and len(v)>8]
def run(name,args,stdin=None,timeout=300,log=True):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat();p=subprocess.run(args,input=stdin,capture_output=True,timeout=timeout)
 raw=p.stdout+p.stderr
 for s in secrets:raw=raw.replace(s,b'[REDACTED]')
 if log or p.returncode:(out/(name+'.log')).write_bytes(raw if log else p.stderr)
 rows.append(dict(name=name,argv=args,exit_code=p.returncode,started_utc=start));(out/'commands.json').write_text(json.dumps(rows,indent=2))
 assert p.returncode==0,name
 return p.stdout
def sql(q,target=db,readonly=True):
 statement=('BEGIN READ ONLY;SET LOCAL statement_timeout=\'20s\';'+q+';ROLLBACK;')if readonly else q
 return run('sql-'+str(len(rows)),['docker','exec','-i','-u','postgres',pg,'psql','-X','-w','-U','postgres','-d',target,'-qAt','-v','ON_ERROR_STOP=1'],statement.encode(),log=False).decode().strip()
def snapshot(target):
 result={'schema':sql('SELECT version||\':\'||dirty FROM schema_migrations',target)}
 for t in ['users','tenants','tenant_members','tenant_api_keys','api_key_tenant_scopes','api_key_kb_scopes','tenant_portal_configs','knowledges','chunks','embeddings','data_sources']:
  exists=sql("SELECT to_regclass('public."+t+"')IS NOT NULL",target)
  if exists=='t':result[t]=json.loads(sql("SELECT json_build_object('count',count(*),'fingerprint',md5(COALESCE(string_agg((to_jsonb(t)-'personal_scope_mode'-'legacy_access_state')::text,E'\\n' ORDER BY (to_jsonb(t)-'personal_scope_mode'-'legacy_access_state')::text),'')))FROM "+t+' t',target))
 return result

try:
 # This task-owned backup directory is on the data disk, not root. Create only
 # this exact path with Docker's existing permission; never chmod the disk.
 backup=Path('/mnt/sata0')/('knowhub-formal-c2-'+base.name+'-'+attempt)
 assert backup.parent==Path('/mnt/sata0')and not backup.exists()
 run('create-backup-directory',['docker','run','--rm','--network','none','--entrypoint','sh','-v','/mnt/sata0:/disk','nginx:1.30.3-alpine','-c','mkdir -m 700 /disk/'+backup.name+' && chown 1000:1000 /disk/'+backup.name])
 assert shutil.disk_usage(backup).free>5*1024**3
 private=backup/'private';private.mkdir(mode=0o700)
 os.umask(0o077)
 (private/'app-inspect.json').write_text(json.dumps(a));(private/'front-inspect.json').write_text(json.dumps(f))
 (private/'app.env').write_text(''.join(k+'='+v+'\n'for k,v in env.items()))
 labels=a['Config']['Labels'];files=labels['com.docker.compose.project.config_files'].split(',')
 cmd=['docker','compose','--project-name','knowhub-mvp-p3','--project-directory',labels['com.docker.compose.project.working_dir']]
 for path in files:cmd+=['-f',path]
 # Config contains secrets; never put it in public stdout or a git artifact.
 resolved=subprocess.run(cmd+['config','--format','json'],capture_output=True,timeout=40)
 (private/'original-compose-resolution.stderr').write_bytes(resolved.stderr)
 if resolved.returncode==0:(private/'resolved-original-compose.json').write_bytes(resolved.stdout)
 # Historical override files may have expired. The complete running inspect
 # remains authoritative and is backed up privately; don't invent a config.
 (out/'compose-resolution.json').write_text(json.dumps({'exit_code':resolved.returncode,'actual_running_inspect_preserved':True,'resolution_error_retained_privately':True}))
 copies=[]
 for i,path in enumerate(files):
  p=Path(path);dest=private/('compose-'+str(i)+p.suffix)
  if p.is_file():
   shutil.copy2(p,dest);copies.append({'original':path,'copy':dest.name,'sha256':hashlib.sha256(dest.read_bytes()).hexdigest()})
  else:copies.append({'original':path,'exists':False})
 (private/'compose-index.json').write_text(json.dumps(copies,indent=2))
 before=snapshot(db);assert before['schema']=='3033:false'
 dump=private/'test-before-3033.dump'
 with dump.open('wb')as stream:
  p=subprocess.run(['docker','exec','-u','postgres',pg,'pg_dump','-U','postgres','-Fc',db],stdout=stream,stderr=subprocess.PIPE,timeout=300)
 assert p.returncode==0,'pg_dump failed'
 os.chmod(dump,0o600)
 clone='knowhub_formal_restore_'+base.name.replace('-','_')
 assert re.fullmatch(r'knowhub_formal_restore_[a-z0-9_]+',clone)
 sql('CREATE DATABASE '+clone+' TEMPLATE template0',target='postgres',readonly=False)
 with dump.open('rb')as stream:
  p=subprocess.run(['docker','exec','-i','-u','postgres',pg,'pg_restore','-U','postgres','-d',clone],stdin=stream,capture_output=True,timeout=300)
 assert p.returncode==0,'restore failed'
 restored=snapshot(clone);after=snapshot(db)
 assert before==restored==after,'protected business rows changed during backup; quiesce before continuing'
 # Store the complete current file volume and source ownership rules locally
 # on the same protected backup disk; no data is sent to the operator machine.
 volumes=[m for m in a['Mounts']if m['Type']=='volume'and m['Destination']in('/data/files','/data/feishu-staging')]
 for i,m in enumerate(volumes):
  name='storage-'+str(i)+'.tgz'
  run('backup-'+name,['docker','run','--rm','--network','none','--entrypoint','sh','-v',m['Name']+':/data:ro','-v',str(private)+':/backup','nginx:1.30.3-alpine','-c','tar -czf /backup/'+name+' -C /data . && chown 1000:1000 /backup/'+name+' && chmod 600 /backup/'+name],timeout=900)
  run('verify-'+name,['tar','-tzf',str(private/name)],log=False)
 for i,m in enumerate(a['Mounts']):
  if m['Type']=='bind'and Path(m['Source']).exists():
   source=Path(m['Source']);dest=private/('bind-'+str(i))
   if source.is_dir():shutil.copytree(source,dest)
   else:shutil.copy2(source,dest)
 # Redis backup is task-owned; do not change queues or credentials.
 redis='WeKnora-redis';password=env.get('REDIS_PASSWORD','');rdb='/tmp/knowhub-formal-'+base.name+'.rdb'
 cmd=['docker','exec','-i',redis,'sh','-c','read -r REDISCLI_AUTH; export REDISCLI_AUTH; exec redis-cli --rdb '+rdb]
 run('redis-snapshot',cmd,(password+'\n').encode(),log=False)
 run('redis-copy',['docker','cp',redis+':'+rdb,str(private/'redis.rdb')],log=False)
 os.chmod(private/'redis.rdb',0o600)
 state={'backup_path':str(backup),'private_path':str(private),'clone_database':clone,'app_name':app,'frontend_name':front,'postgres_name':pg,'database_name':db,'original_compose_argv':cmd,'candidate':str(candidate)}
 # Record the actual Compose invocation separately (cmd above is Redis).
 state['original_compose_argv']=['docker','compose','--project-name','knowhub-mvp-p3','--project-directory',labels['com.docker.compose.project.working_dir']]+sum([['-f',v]for v in files],[])
 (base/'private').mkdir(mode=0o700);(base/'private/state.json').write_text(json.dumps(state,indent=2))
 hashes={str(p.relative_to(private)):hashlib.sha256(p.read_bytes()).hexdigest()for p in private.rglob('*')if p.is_file()}
 (private/'backup-manifest.json').write_text(json.dumps(hashes,indent=2))
 (out/'protected-row-snapshot.json').write_text(json.dumps(before,indent=2))
 result={'exit_code':0,'read_only_production':True,'production_accessed':False,'backup_on_data_disk':str(backup),'free_bytes_after':shutil.disk_usage(backup).free,'restore_database':clone,'schema':before['schema'],'protected_rows_equal':before==restored==after,'AES_material_preserved':bool(env.get('SYSTEM_AES_KEY')),'key_decryption_verified':False,'private_files_exported':False,'dump_sha256':hashlib.sha256(dump.read_bytes()).hexdigest(),'storage_archives':len(volumes)}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'production_accessed':False}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
