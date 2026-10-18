"""Fresh isolated instance and protected online backup; never touches entry."""
import os
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');assert not R.exists()
R.mkdir(mode=0o700);(R/'private').mkdir(mode=0o700)
worker=r'''import os,sys,json,pathlib,subprocess,datetime,secrets,time,hashlib,shutil,traceback
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');P=R/'private';BACK=P/'original-backup';BACK.mkdir(mode=0o700)
OLD='knowhub-company-test';NEW='knowhub-jit-fresh';APP=OLD+'-app-1';PG=OLD+'-postgres-1'
IMAGE='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b';REV='5f988be51779f1a22bd9d027b7503fe6a8749f62'
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort
port=DockerPilotPort.__new__(DockerPilotPort)
out={'result':'RUNNING','stage':'preflight','real_sid_requests':0,'original_entry_changed':False}
def save():(R/'prepare-result.json').write_text(json.dumps(out))
def cmd(args,data=None,timeout=90):
 z=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_operation_failed')
 return z.stdout
def inspect(name,field):return json.loads(cmd(['docker','inspect','--format','{{json '+field+'}}',name]))
def q(sql):return port.query(sql)
def jq(sql):return json.loads(q(sql))
def fresh(sql):return cmd(['docker','exec','-i',NEW+'-postgres-1','psql','-X','-qAt','-U','fresh_owner','-d','knowhub_jit_fresh','-v','ON_ERROR_STOP=1'],('BEGIN READ ONLY;'+sql+';ROLLBACK;').encode()).decode().strip()
def compose(*args):return cmd(['docker','compose','-p',NEW,'--project-directory',str(R),'-f',str(R/'compose.json')]+list(args),timeout=180)
def baseline():
 tables=('users','tenants','tenant_members','knowledge_bases','knowledges','external_identities','source_id_binding_reviews','source_id_sessions','source_id_jit_registrations','source_id_link_decisions')
 return {t:jq("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT to_jsonb(v)-'password_hash'-'email'-'username'-'api_key'-'subject'-'employee_no' AS row FROM "+t+" v ORDER BY id)x") for t in tables}
save()
try:
 assert shutil.disk_usage(R).free>8*1024**3
 assert inspect(APP,'.Image')==IMAGE and inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REV
 assert q("SELECT version::text||'|'||dirty FROM schema_migrations")=='3030|false'
 assert q("SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>'' AND NOT consumed AND expires_at>NOW()")=='0'
 assert q("SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()")=='0'
 before=baseline();container_names=json.loads(cmd(['docker','ps','--format','{{json .Names}}']).decode().replace('\n',',').rstrip(',').join(['[',']']))
 original={n:{'id':inspect(n,'.Id'),'image':inspect(n,'.Image'),'mounts':inspect(n,'.Mounts'),'ports':inspect(n,'.HostConfig.PortBindings')} for n in container_names if n.startswith(OLD+'-') or n=='knowhub-company-entry-entry-1'}
 gateway=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf');gateway_before=gateway.read_bytes()
 (BACK/'gateway.conf').write_bytes(gateway_before);(BACK/'containers.json').write_text(json.dumps(original));(BACK/'data-baseline.json').write_text(json.dumps(before))
 (BACK/'app-inspect-protected.json').write_bytes(cmd(['docker','inspect',APP]))
 files=inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files'].split(',')
 for i,f in enumerate(files):(BACK/('compose-'+str(i)+'.json')).write_bytes(pathlib.Path(f).read_bytes())
 (BACK/'compose-order.json').write_text(json.dumps(files))
 e=dict(v.split('=',1) for v in inspect(APP,'.Config.Env') if '=' in v)
 (BACK/'runtime-env.json').write_text(json.dumps(e))
 out['stage']='online_backup';save()
 with (BACK/'company-db3030.dump').open('wb') as f:
  z=subprocess.run(['docker','exec',PG,'pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=180);assert z.returncode==0
 scan="import os,json,hashlib;rows=[]\nfor root,ds,fs in os.walk('/scan'):\n for n in fs:\n  p=os.path.join(root,n)\n  if os.path.isfile(p):\n   with open(p,'rb') as f:h=hashlib.sha256(f.read()).hexdigest()\n   rows.append([os.path.relpath(p,'/scan'),os.path.getsize(p),h])\nprint(json.dumps(sorted(rows)))"
 def file_scan():return cmd(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',IMAGE,'-c',scan])
 fs_before=file_scan()
 with (BACK/'company-files.tar').open('wb') as f:
  z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','tar',IMAGE,'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
 assert fs_before==file_scan() and before==baseline()
 (BACK/'protected-file-manifest.json').write_bytes(fs_before)
 out['backup']={'database_bytes':(BACK/'company-db3030.dump').stat().st_size,'files_bytes':(BACK/'company-files.tar').stat().st_size,'database_transaction_consistent':True,'file_double_scan_stable':True,'application_stopped':False,'concurrent_metadata_changes_detected':False}
 out['stage']='create_fresh_resources';save()
 assert not cmd(['docker','ps','-aq','--filter','label=com.docker.compose.project='+NEW]).strip()
 password=secrets.token_urlsafe(32);jwt=secrets.token_urlsafe(48);aes=secrets.token_hex(16);redis_password=secrets.token_urlsafe(32)
 appenv={k:e[k] for k in ('DB_DRIVER','RETRIEVE_DRIVER','STORAGE_TYPE','LOCAL_STORAGE_BASE_DIR','GIN_MODE','APP_ENV','LOG_LEVEL','DOCREADER_ADDR','TZ') if k in e}
 appenv.update(DB_HOST='postgres',DB_PORT='5432',DB_NAME='knowhub_jit_fresh',DB_USER='fresh_owner',DB_PASSWORD=password,REDIS_ADDR='redis:6379',REDIS_PASSWORD=redis_password,JWT_SECRET=jwt,SYSTEM_AES_KEY=aes,AUTO_MIGRATE='false',OIDC_AUTH_ENABLE='false',SOURCEID_ENABLE='true',SOURCEID_PROTOCOL_ONLY='true',SOURCEID_CONTRACT_CONFIRMED='false',SOURCEID_EMAIL_BINDING_ENABLE='false',SOURCEID_AUTO_BINDING_ENABLE='false',SOURCEID_ACCOUNT_SCOPE='enterprise_all',SOURCEID_ACCOUNT_LINK_MODE='trusted_ownership',SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED='false',SOURCEID_VERIFIED_EMAIL_POLICY_REF='',SOURCEID_SESSION_MODE='external',SOURCEID_LOCAL_REFRESH_ENABLE='false',SOURCEID_JIT_ENABLE='false',SOURCEID_JIT_POLICY_CONFIRMED='false',SOURCEID_LOGOUT_ENABLE='false',SOURCEID_FORCE_REAUTH_PROMPT_LOGIN='false',SOURCEID_BASIC_ENCODING='oauth_form',SOURCEID_DIAGNOSTICS_ENABLE='false')
 assert jwt!=e['JWT_SECRET'] and aes!=e['SYSTEM_AES_KEY'] and password!=e['DB_PASSWORD']
 def envfile(name,data):
  assert all('\n' not in str(v) for v in data.values());(P/name).write_text(''.join(k+'='+str(v)+'\n' for k,v in data.items()))
 envfile('app.env',appenv);envfile('postgres.env',{'POSTGRES_USER':'fresh_owner','POSTGRES_PASSWORD':password,'POSTGRES_DB':'knowhub_jit_fresh'});envfile('redis.env',{'FRESH_REDIS_PASSWORD':redis_password})
 pgimage=inspect(PG,'.Image');redisimage=inspect(OLD+'-redis-1','.Image');frontimage=inspect(OLD+'-frontend-1','.Image');docimage=inspect(OLD+'-docreader-1','.Image')
 common={'restart':'unless-stopped','logging':{'driver':'none'},'networks':['isolated']}
 c={'services':{'postgres':dict(common,image=pgimage,env_file=[str(P/'postgres.env')],volumes=['postgres:/var/lib/postgresql/data']),'redis':dict(common,image=redisimage,env_file=[str(P/'redis.env')],command=['sh','-c','exec redis-server --requirepass "$$FRESH_REDIS_PASSWORD" --appendonly yes'],volumes=['redis:/data']),'docreader':dict(common,image=docimage,volumes=['doc-tmp:/tmp/docreader']),'app':dict(common,image=IMAGE,env_file=[str(P/'app.env')],volumes=['files:/data/files','doc-tmp:/tmp/docreader:ro']),'frontend':dict(common,image=frontimage,environment={'APP_HOST':'app','APP_PORT':'8080','APP_SCHEME':'http'})},'networks':{'isolated':{'internal':True,'enable_ipv6':False}},'volumes':{k:{} for k in ('postgres','redis','files','doc-tmp')}}
 (R/'compose.json').write_text(json.dumps(c,indent=2));compose('up','-d','--no-build','--pull','never','postgres','redis','docreader')
 for _ in range(60):
  if subprocess.run(['docker','exec',NEW+'-postgres-1','pg_isready','-U','fresh_owner','-d','knowhub_jit_fresh'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('fresh_postgres_not_ready')
 out['stage']='standard_fresh_migrations';save()
 migrations='/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/source/migrations/versioned'
 migration_code="import os,subprocess,urllib.parse;dsn='postgres://fresh_owner:'+urllib.parse.quote(os.environ['DB_PASSWORD'],safe='')+'@postgres:5432/knowhub_jit_fresh?sslmode=disable';p=subprocess.run(['migrate','-path','/migration','-database',dsn,'goto','3030'],stdout=subprocess.PIPE,stderr=subprocess.PIPE);raise SystemExit(p.returncode)"
 cmd(['docker','run','--rm','--network',NEW+'_isolated','--log-driver','none','--env-file',str(P/'app.env'),'--mount','type=bind,source='+migrations+',target=/migration,readonly','--entrypoint','python3',IMAGE,'-c',migration_code],timeout=240)
 assert fresh("SELECT version::text||'|'||dirty FROM schema_migrations")=='3030|false'
 tables=('users','external_identities','source_id_binding_reviews','source_id_account_ownership','source_id_sessions','source_id_jit_registrations','source_id_link_blocks','auth_tokens','tenant_members','knowledge_bases')
 counts={t:int(fresh('SELECT COUNT(*) FROM '+t)) for t in tables};assert all(v==0 for v in counts.values())
 out['stage']='start_fresh_app';save();compose('up','-d','--no-build','--pull','never','app','frontend')
 for _ in range(90):
  if inspect(NEW+'-app-1','.State').get('Health',{}).get('Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('fresh_app_not_healthy')
 probe="import urllib.request,json; o=urllib.request.build_opener(urllib.request.ProxyHandler({}));r=o.open('http://127.0.0.1:8080/api/v1/auth/oidc/config');v=json.load(r);assert v['enabled'] is False;print(json.dumps({'config_status':r.status,'oidc_enabled':v['enabled']}))"
 health=json.loads(cmd(['docker','exec',NEW+'-app-1','python3','-c',probe]))
 for name,old in original.items():
  assert inspect(name,'.Id')==old['id'] and inspect(name,'.Image')==old['image'] and inspect(name,'.HostConfig.PortBindings')==old['ports']
 assert gateway.read_bytes()==gateway_before and baseline()==before
 fresh_names=json.loads(cmd(['docker','ps','--filter','label=com.docker.compose.project='+NEW,'--format','{{json .Names}}']).decode().replace('\n',',').rstrip(',').join(['[',']']))
 assert len(fresh_names)==5 and all(not inspect(n,'.HostConfig.PortBindings') for n in fresh_names)
 assert inspect(NEW+'-app-1','.Image')==IMAGE
 for f in BACK.iterdir():f.chmod(0o600)
 out.update(result='PASS',stage='FRESH_CLOSED_READY',project=NEW,app_revision=REV,schema=3030,dirty=False,empty_identity_counts=counts,health=health,independent_database_redis_files_and_keys=True,internal_network_only=True,all_host_ports_unpublished=True,original_containers_and_routes_unchanged=True,source_account_and_binding_unchanged=True,empty_fresh_instance_not_company_eligibility_proof=True)
except BaseException as e:
 out.update(result='FAIL',failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
'''
(R/'prepare-worker.py').write_text(worker)
subprocess.Popen(['python3',str(R/'prepare-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','project':'knowhub-jit-fresh','existing_entry_changed':False,'real_sid_requests':0}))
