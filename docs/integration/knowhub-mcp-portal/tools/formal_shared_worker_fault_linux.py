"""Fault-inject a temporary no-port candidate probe on the restored test clone.

Never stops shared Redis/P3. Only fm- sources may execute. The private lease age
injection tests fencing, not thirty minutes of wall-clock passage.
"""
from pathlib import Path
import datetime,hashlib,json,os,subprocess,sys,time,traceback,urllib.request,urllib.error
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());private=Path(state['private_path']);candidate=Path(state['candidate']);m=json.loads((candidate/'manifest.json').read_text())
rc=json.loads((base/'formal-recovery-current.json').read_text());proof=json.loads((base/'results'/rc['results_dir']/'proof.json').read_text());assert proof['exit_code']==0 and proof['candidate_sha256']==m['candidate_sha256']
source=json.loads((base/'feishu-source-current.json').read_text());ds=source['sources']['wiki']['source_id'];kid=source['sources']['wiki']['knowledge_id'];assert kid.startswith('fm-')
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert attempt in('initial','a2');suffix=''if attempt=='initial'else'-'+attempt
out=base/'results'/('worker-fault'+suffix);out.mkdir(exist_ok=False);commands=[];checks={};app='kh-formal-fault-'+m['candidate_sha256'][:12]+suffix;redis=app+'-redis';network='weknora_WeKnora-network';started=[]
db=rc['db']+suffix.replace('-','_');files=rc['files_volume']+suffix
env=dict(x.split('=',1)for x in(private/'formal-rollback.env').read_text().splitlines()if'='in x)
image=json.loads((candidate/'results/build/done.json').read_text())['app'];origin=None
def command(name,args,timeout=180):
 p=subprocess.run(args,capture_output=True,timeout=timeout);raw=p.stdout+p.stderr
 for value in env.values():
  if len(value)>12:raw=raw.replace(value.encode(),b'[REDACTED]')
 (out/(name+'.log')).write_bytes(raw);commands.append({'name':name,'argv':args,'exit_code':p.returncode});(out/'commands.json').write_text(json.dumps(commands,indent=2));assert p.returncode==0,name
 return p.stdout
def sql(q):
 # The SQL target is always the independent restored clone, never WeKnora.
 assert db.startswith('knowhub_formal_denial_restore_')
 return subprocess.check_output(['docker','exec','-u','postgres','WeKnora-postgres','psql','-X','-U','postgres','-d',db,'-qAt','-v','ON_ERROR_STOP=1','-c',q]).decode().strip()
def req(method,path,body=None,token=None):
 h={'Content-Type':'application/json','X-Tenant-ID':'820099'}
 if token:h['Authorization']='Bearer '+token
 q=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,headers=h,method=method)
 try:
  with urllib.request.urlopen(q,timeout=45)as s:status,raw=s.status,s.read()
 except urllib.error.HTTPError as e:status,raw=e.code,e.read()
 return status,json.loads(raw)if raw else None
def fixture(changes=None):
 control='http://formal-feishu:8000/'+('state'if changes is None else'control/wiki')
 code='import urllib.request,json;print(urllib.request.urlopen(urllib.request.Request('+repr(control)+',data='+repr(None if changes is None else json.dumps(changes).encode())+',headers={"Content-Type":"application/json"})).read().decode())'
 raw=subprocess.check_output(['docker','run','--rm','--network',network,'--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','-c',code])
 return json.loads(raw)['wiki']if changes is None else None
def healthy():
 global origin
 for _ in range(90):
  x=json.loads(subprocess.check_output(['docker','inspect',app]))[0];ip=x['NetworkSettings']['Networks'][network]['IPAddress'];origin='http://'+ip+':8080'
  try:
   with urllib.request.urlopen(origin+'/health',timeout=2)as s:
    if s.status==200:return
  except Exception:pass
  time.sleep(1)
 raise AssertionError('probe readiness failed')
def terminal(log):
 end=time.monotonic()+180
 while time.monotonic()<end:
  status=sql("SELECT status FROM sync_logs WHERE id='"+log+"'")
  if status!='running':return status
  time.sleep(1)
 raise AssertionError('sync remained running')
try:
 if attempt!='initial':
  command('create-new-restored-clone',['docker','exec','-u','postgres','WeKnora-postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE '+db+' TEMPLATE template0'])
  with Path(source['backup']).open('rb')as f:code=subprocess.run(['docker','exec','-i','-u','postgres','WeKnora-postgres','pg_restore','-U','postgres','-d',db],stdin=f,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE).returncode
  assert code==0,'fresh revoked clone restore failed'
  command('copy-independent-probe-files',['docker','run','--rm','--network','none','-v',rc['files_volume']+':/old:ro','-v',files+':/new','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','sh','-c','cp -a /old/. /new/'])
 assert sql("SELECT count(*)FROM data_sources WHERE deleted_at IS NULL AND type IN('feishu','feishu_drive')AND tenant_id<>820099")=='0'
 assert sql("SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'")=='active'
 command('start-private-redis',['docker','run','-d','--name',redis,'--network',network,'--label','knowhub.formal=temporary-fault-probe','redis:7.0-alpine','redis-server','--save','','--appendonly','no']);started.append(redis)
 args=['docker','run','-d','--name',app,'--network',network,'--label','knowhub.formal=temporary-fault-probe','--env-file',str(private/'formal-rollback.env'),'-e','DB_DRIVER=postgres','-e','DB_HOST=WeKnora-postgres','-e','DB_PORT=5432','-e','DB_NAME='+db,'-e','REDIS_ADDR='+redis+':6379','-e','REDIS_PASSWORD=','-e','FEISHU_LEGACY_SYNC_SUSPENDED=false','-e','FEISHU_SAFE_INCREMENTAL_ENABLED=true','-e','FEISHU_DOCX_PARSE_MODE=blocks','-e','FEISHU_API_RETRY_MAX=0','-e','AUTO_RECOVER_DIRTY=false','-e','DUCKDB_SKIP_EXTENSION_LOAD=1','-e','RETRIEVE_DRIVER=postgres','-e','SSRF_WHITELIST=formal-model,formal-feishu','-e','LOCAL_STORAGE_BASE_DIR=/data/files','-v',files+':/data/files','--entrypoint','/app/WeKnora',image]
 command('start-private-candidate-probe',args);started.append(app);healthy()
 x=json.loads(subprocess.check_output(['docker','inspect',app]))[0];assert not x['HostConfig']['PortBindings']
 status,login=req('POST','/api/v1/auth/login',{'email':'fm-owner@example.invalid','password':env['KNOWHUB_V1_TEST_PASSWORD']});assert status==200;owner=login['token']
 cursorq="SELECT md5(last_sync_cursor::text)FROM data_sources WHERE id='"+ds+"'";digestq="SELECT md5(coalesce(string_agg(content,','ORDER BY chunk_id),''))FROM embeddings WHERE knowledge_id='"+kid+"'"
 cursor=sql(cursorq);before=sql(digestq);seen=fixture()['blocks']
 fixture({'status':200,'block_status':200,'missing':False,'delay':10,'revision':1792555380,'text':'wiki restartcanary verified source text'})
 status,data=req('POST','/api/v1/datasource/'+ds+'/sync',{},owner);assert status==200;log=data['id']
 end=time.monotonic()+60
 while time.monotonic()<end and fixture()['blocks']==seen:time.sleep(.1)
 assert fixture()['blocks']>seen,'worker did not reach synthetic cloud'
 generation=int(sql("SELECT execution_generation FROM sync_logs WHERE id='"+log+"'"));assert generation>0
 assert req('POST','/api/v1/datasource/'+ds+'/sync',{},owner)[0]==400
 command('kill-only-private-probe',['docker','kill','--signal','SIGKILL',app])
 assert sql(digestq)==before and sql(cursorq)==cursor
 sql("UPDATE sync_logs SET updated_at=clock_timestamp()-interval'31 minutes',started_at=clock_timestamp()-interval'31 minutes'WHERE id='"+log+"'")
 fixture({'delay':0});command('restart-private-probe',['docker','start',app]);healthy()
 recovered=sql("SELECT status||':'||execution_generation||':'||execution_claimed FROM sync_logs WHERE id='"+log+"'")
 assert recovered=='running:'+str(generation)+':true','AUTO_MIGRATE=false leaves startup cleanup disabled: '+recovered
 status,data=req('POST','/api/v1/datasource/'+ds+'/sync',{},owner);assert status==200
 recovered=sql("SELECT status||':'||execution_generation||':'||execution_claimed FROM sync_logs WHERE id='"+log+"'")
 assert recovered=='failed:'+str(generation+1)+':false','real sync admission failed to fence stale generation: '+recovered
 assert terminal(data['id'])=='success'
 assert sql("SELECT count(*)FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")=='1'
 assert sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")==kid
 assert sql("SELECT count(*)FROM embeddings WHERE knowledge_id='"+kid+"'AND content ||| 'restartcanary'")!='0'
 checks['worker_crash_failure_keeps_old_cursor_and_indices']='PASS';checks['duplicate_admission_and_stale_generation_fenced']='PASS';checks['retry_stable_KID_no_duplicates']='PASS'
 cursor=sql(cursorq);before=sql(digestq);fixture({'revision':1792555440,'text':'wiki redisrecoverycanary verified source text'})
 command('stop-only-private-redis',['docker','stop','--time','3',redis]);status,data=req('POST','/api/v1/datasource/'+ds+'/sync',{},owner)
 assert status in(200,400,500,503);assert sql(digestq)==before and sql(cursorq)==cursor
 command('restart-only-private-redis',['docker','start',redis]);outage=status
 if status==200:assert terminal(data['id'])=='success';path='admitted-outbox'
 else:
  latest=sql("SELECT id FROM sync_logs WHERE data_source_id='"+ds+"'ORDER BY created_at DESC LIMIT 1");done=terminal(latest)
  published=sql("SELECT count(*)FROM embeddings WHERE knowledge_id='"+kid+"'AND content ||| 'redisrecoverycanary'")!='0'
  if done=='success'and published:path='durable-outbox-recovery'
  else:
   path='explicit-retry-after-terminal';status,data=req('POST','/api/v1/datasource/'+ds+'/sync',{},owner);assert status==200 and terminal(data['id'])=='success'
 assert sql("SELECT count(*)FROM embeddings WHERE knowledge_id='"+kid+"'AND content ||| 'redisrecoverycanary'")!='0'
 assert sql("SELECT count(*)FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")=='1'
 checks['private_Redis_outage_outbox_and_recovery']='PASS'
 result={'exit_code':0,'candidate_sha256':m['candidate_sha256'],'checks':checks,'lease_fault':'synthetic age of one test log; no wall-clock claim','old_execution_fenced':recovered,'outage_status':outage,'recovery_path':path,'shared_Redis_or_APP_stopped':False,'temporary_no_port_probe_only':True,'production_accessed':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'traceback':traceback.format_exc(),'checks':checks}
finally:
 for name in reversed(started):
  subprocess.run(['docker','stop','--time','3',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=20)
 (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
