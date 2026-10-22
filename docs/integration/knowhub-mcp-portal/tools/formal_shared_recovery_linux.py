"""Cold-start the independent RC3 hotfix and its real Worker in a synthetic clone.

No host port, no production input; new DB/Redis/files. No Go MCP source added.
"""
from pathlib import Path
import hashlib,json,os,subprocess,sys,time,urllib.parse
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());private=Path(state['private_path']);candidate_dir=Path(state['candidate'])
manifest=json.loads((candidate_dir/'manifest.json').read_text());instance={'candidate_sha256':manifest['candidate_sha256']}
source=json.loads((base/'feishu-source-current.json').read_text());assert json.loads((base/'results'/source['results_dir']/'result.json').read_text())['exit_code']==0
hot={'material_sha256': '2ecd6b90388a258bbb854c9351700d0316de9a37c064c146c5003d1fb2ca85dc', 'base': 'knowhub-feishu-rc3/app:02a8c5eb187764a9', 'base_id': 'sha256:0a0117c920c4fed402e7f744578b539a5ef5e6ef8da7c141ff5e9ac15031b73d', 'files': {'migrations/migration-map.json': 'd94765d2ed80c809ccb5dd47332c0cb25b5f569df063e7260ab1341337a9245a', 'migrations/versioned/003037_personal_dynamic_wiki.up.sql': 'e4b7199a9cd282f38bf43f33ba5975dd8f5b8d61ae955ac214434be807659024', 'migrations/versioned/003037_personal_dynamic_wiki.down.sql': '453ad5966c9aa0f46a5f0640dec9f49e051fb0ea789836471934992a392bd8db', 'migrations/sqlite/003037_personal_dynamic_wiki.up.sql': '7e88ba21fae6b6bd4ad261c9ca569d47593764a26f04daf9fd05db5ddf7d4e86', 'migrations/sqlite/003037_personal_dynamic_wiki.down.sql': '4afc676d0d07366141d7d62ecd3fc67a61b16519bf3bc1ddb6d6cbe570eda3a8'}, 'remote': '/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-safe-rollback-20261022/2ecd6b90388a258b', 'rollback_approved': False, 'reason': 'requires isolated same-link and REST denial, forward and restore evidence', 'schema': 3037, 'binary_source': 'independent RC3 Feishu hardened patch; no new personal MCP product features', 'argv': ['docker', 'build', '--network', 'none', '--pull=false', '-t', 'knowhub-safe-rollback/app:2ecd6b90388a258b', '/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-safe-rollback-20261022/2ecd6b90388a258b'], 'exit_code': 0, 'image': 'knowhub-safe-rollback/app:2ecd6b90388a258b', 'image_id': 'sha256:d5c17d6b2d7c84ff463453a3f6809b4b921c97c621d83d3ce7f73d9fcd1fb1b8'}
assert json.loads(subprocess.check_output(['docker','image','inspect',hot['image']]))[0]['Id']==hot['image_id']
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert attempt in('initial','a2','a3');suffix=''if attempt=='initial'else'-'+attempt
out=base/'results'/('recovery'+suffix);out.mkdir(exist_ok=False)
compose=state['upgrade_compose_argv'];project='kh-formal-'+base.name+suffix
app=project+'-safe-rollback-probe';redis='kh-rb-redis-'+manifest['candidate_sha256'][:12]+suffix;db='knowhub_formal_denial_restore_'+base.name+suffix.replace('-','_')
files=project+'_safe_rollback_files';network='weknora_WeKnora-network';rows=[];started=[]
env=dict(x.split('=',1)for x in(private/'app.env').read_text().splitlines()if'='in x)
env.update(dict(x.split('=',1)for x in(private/'formal-signing.env').read_text().splitlines()if'='in x))
fe=dict(x.split('=',1)for x in Path(state['fixture_env']).read_text().splitlines()if'='in x)
env['KNOWHUB_V1_TEST_PASSWORD']=fe['FORMAL_TEST_PASSWORD']
probeenv=private/'formal-rollback.env';probeenv.write_text(''.join(k+'='+v+'\n'for k,v in env.items()));os.chmod(probeenv,0o600)
reference=Path(source['revoked_file_reference']).resolve();assert reference.parent==private
revoked_link=reference.read_text();assert hashlib.sha256(revoked_link.encode()).hexdigest()==source['revoked_file_sha256']
assert int(urllib.parse.parse_qs(urllib.parse.urlsplit(revoked_link).query)['expires'][0])>time.time()

def run(name,args,stdin=None,require=True,timeout=240):
 r=subprocess.run(args,input=stdin,capture_output=True,timeout=timeout);raw=r.stdout+r.stderr
 for secret in env.values():
  if len(secret)>12:raw=raw.replace(secret.encode(),b'[REDACTED_SYNTHETIC_CREDENTIAL]')
 raw=raw.replace(revoked_link.encode(),b'[REDACTED_SIGNED_TEST_REFERENCE]')
 (out/(name+'.log')).write_bytes(raw);rows.append(dict(name=name,argv=args,exit_code=r.returncode));(out/'commands.json').write_text(json.dumps(rows,indent=2))
 if require:assert r.returncode==0,name
 return r

def pg(dbname,args,stdin=None):
 return subprocess.check_output(['docker','exec','-i','-u','postgres','WeKnora-postgres']+args+['-U','postgres','-d',dbname],input=stdin)
def sql(q):return subprocess.check_output(['docker','exec','-u','postgres','WeKnora-postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d',db,'-qAtc',q]).decode().strip()

def api(name,method,path,body=None):
 code="""import urllib.request,urllib.error,json,os
origin=ORIGIN
def decode(raw):
 try:return json.loads(raw or b'null')
 except (ValueError,TypeError):return raw.decode(errors='replace')
def req(method,path,body=None,token=None):
 h={'Content-Type':'application/json','X-Tenant-ID':'820099'}
 if token:h['Authorization']='Bearer '+token
 r=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,method=method,headers=h)
 try:
  with urllib.request.urlopen(r,timeout=30)as s:return s.status,decode(s.read())
 except urllib.error.HTTPError as e:return e.code,decode(e.read())
status,auth=req('POST','/api/v1/auth/login',{'email':'fm-owner@example.invalid','password':os.environ['KNOWHUB_V1_TEST_PASSWORD']});assert status==200
status,data=req(METHOD,PATH,BODY,auth['token']);print(json.dumps({'status':status,'data':data}))
""".replace('ORIGIN',repr('http://'+app+':8080')).replace('METHOD',repr(method)).replace('PATH',repr(path)).replace('BODY',repr(body))
 args=['docker','run','--rm','--network',network,'--env-file',str(probeenv),'--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','-c',code]
 return json.loads(run(name,args).stdout)
def fixture(kind,revision,text):
 code="import urllib.request,json;urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8000/control/"+kind+"',data="+repr(json.dumps(dict(status=200,block_status=200,missing=False,delay=0,revision=revision,text=text)).encode())+",headers={'Content-Type':'application/json'},method='POST')).read()"
 run('fixture-'+kind+'-'+str(revision),compose+['exec','-T','feishu','python','-c',code])
def sync(ds,label):
 response=api(label+'-enqueue','POST','/api/v1/datasource/'+ds+'/sync',{});assert response['status']==200
 log=response['data']['id'];deadline=time.monotonic()+180
 while time.monotonic()<deadline:
  status=sql("SELECT status FROM sync_logs WHERE id='"+log+"'")
  if status!='running':break
  time.sleep(1)
 assert status=='success',label+': '+status
 return log

result={};checks={}
try:
 run('create-clone',['docker','exec','-u','postgres','WeKnora-postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE '+db+' TEMPLATE template0'])
 backup=Path(source['backup']).resolve();assert backup.parent==private
 run('restore-revoked-test-clone',['docker','exec','-i','-u','postgres','WeKnora-postgres','pg_restore','-U','postgres','-d',db],backup.read_bytes())
 security_queries={
  'credential_authority':"SELECT md5(COALESCE(jsonb_agg(row_to_json(s) ORDER BY s.id)::text,'')) FROM (SELECT id,owner_user_id,scope_type,full_access,capabilities,personal_scope_mode,expires_at,revoked_at FROM tenant_api_keys ORDER BY id)s",
  'tenant_scopes':"SELECT md5(COALESCE(jsonb_agg(row_to_json(s) ORDER BY s.id)::text,'')) FROM api_key_tenant_scopes s",
  'kb_scopes':"SELECT md5(COALESCE(jsonb_agg(row_to_json(s) ORDER BY s.id)::text,'')) FROM api_key_kb_scopes s",
  'source_denials':"SELECT md5(COALESCE(jsonb_agg(row_to_json(s) ORDER BY s.id)::text,'')) FROM (SELECT id,legacy_access_state FROM data_sources ORDER BY id)s",
 }
 authority_before={name:sql(q)for name,q in security_queries.items()}
 run('clone-test-files',['docker','run','--rm','--network','none','-v','weknora_data-files:/old:ro','-v',files+':/new','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','sh','-c','cp -a /old/. /new/'])
 run('start-private-redis',['docker','run','-d','--name',redis,'--label','knowhub.next=safe-rollback','--network',network,'redis:7.0-alpine','redis-server','--save','','--appendonly','no']);started.append(redis)
 candidate=json.loads((candidate_dir/'results/build/done.json').read_text())['app']
 buildimages=json.loads((candidate_dir/'results/build/images.json').read_text())
 assert json.loads(subprocess.check_output(['docker','image','inspect',candidate]))[0]['Id']==buildimages[0]['id']
 assert json.loads((base/'results/deploy-a4/result.json').read_text())['schema']==3037
 checks['forward_upgrade']='PASS'
 linkconfig=('url="http://127.0.0.1:8080'+revoked_link+'"\noutput="/dev/null"\nsilent\nmax-time=10\nwrite-out="%{http_code}"\n').encode()
 drive=source['sources']['drive'];kid=drive['knowledge_id'];ds=drive['source_id']
 for phase,image in [('forward',candidate),('compatibility',hot['image']),('return',candidate)]:
  # DNS labels are limited to 63 bytes; Compose's long candidate prefix plus
  # "compatibility" exceeds that limit in the Python HTTP client.
  app='kh-rb-'+instance['candidate_sha256'][:12]+'-'+phase+suffix
  args=['docker','run','-d','--name',app,'--label','knowhub.next=safe-rollback','--network',network,'--env-file',str(probeenv),'-e','DB_DRIVER=postgres','-e','DB_HOST=WeKnora-postgres','-e','DB_PORT=5432','-e','DB_USER=postgres','-e','DB_NAME='+db,'-e','REDIS_ADDR='+redis+':6379','-e','REDIS_PASSWORD=','-e','DUCKDB_SKIP_EXTENSION_LOAD=1','-e','AUTO_RECOVER_DIRTY=false','-e','FEISHU_LEGACY_SYNC_SUSPENDED=true','-e','FEISHU_SAFE_INCREMENTAL_ENABLED=false','-e','WEKNORA_TENANT_ENABLE_RBAC=true','-e','RETRIEVE_DRIVER=postgres','-e','STORAGE_TYPE=local','-e','LOCAL_STORAGE_BASE_DIR=/data/files','-e','SSRF_WHITELIST=model,feishu','-v',files+':/data/files','--entrypoint','/app/WeKnora',image]
  run('start-'+phase,args);started.append(app)
  for _ in range(90):
   r=subprocess.run(['docker','exec',app,'curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
   if r.returncode==0 and r.stdout==b'200':break
   time.sleep(1)
  else:raise RuntimeError(phase+' cold start failed')
  assert api(phase+'-revoked-REST','GET','/api/v1/knowledge/'+kid)['status']in(403,404)
  assert api(phase+'-historical-reference','GET','/api/v1/datasource/'+ds+'/logs')['status']in(403,404)
  assert run(phase+'-same-unexpired-HMAC',['docker','exec','-i',app,'curl','--config','-'],linkconfig).stdout.decode().strip()in('403','404')
  assert api(phase+'-MCP-disabled','GET','/api/v1/mcp/personal')['status']in(403,404)
  assert sql("SELECT version||':'||dirty FROM schema_migrations")=='3037:false'
  assert sql("SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'")=='revoked'
  assert {name:sql(q)for name,q in security_queries.items()}==authority_before, phase+' changed persisted authority'
  state=json.loads(subprocess.check_output(['docker','inspect',app]))[0];assert not state['HostConfig']['PortBindings']
  run(phase+'-APP-log',['docker','logs',app]);run('stop-'+phase,['docker','stop','--time','5',app])
  checks[{'forward':'restore_denial_persisted','compatibility':'compatibility_cold_start','return':'return_to_candidate'}[phase]]='PASS'
 for name in ['revoked_hmac_denied','revoked_rest_denied','historical_reference_denied','feature_off_denied','schema_retained']:checks[name]='PASS'
 result=dict(exit_code=0,candidate_sha256=instance['candidate_sha256'],image_id=hot['image_id'],schema=3037,checks=checks,authority_digests_preserved=authority_before,production_input=False,old_unsafe_image_executed=False,schema_down_executed=False,source='authorized shared test database revoked backup only',limitation='fallback disables personal Go MCP; retains hardened source reads; not a production deployment authorization')
except Exception as error:result=dict(exit_code=1,error_class=type(error).__name__,message=str(error),checks=checks,production_input=False)
finally:
 if app in started:run('final-APP-log',['docker','logs',app],require=False)
 for name in reversed(started):run('cleanup-stop-'+name,['docker','stop','--time','5',name],require=False)
 (out/'proof.json').write_text(json.dumps(result,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
if result['exit_code']==0:(base/'formal-recovery-current.json').write_text(json.dumps({'results_dir':out.name,'db':db,'files_volume':files,'proof_sha256':hashlib.sha256((out/'proof.json').read_bytes()).hexdigest()},indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
