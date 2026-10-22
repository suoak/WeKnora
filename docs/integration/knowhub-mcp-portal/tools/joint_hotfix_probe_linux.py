"""Cold-start the independent RC3 hotfix and its real Worker in a synthetic clone.

No host port, no production input; new DB/Redis/files. No Go MCP source added.
"""
from pathlib import Path
import hashlib,json,os,subprocess,sys,time,urllib.parse
base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
instance=json.loads((base/'results/deploy/instance.json').read_text());project=instance['project'];assert project.startswith('knowhub-v1-feishu-')
source=json.loads((base/'feishu-source-current.json').read_text());assert json.loads((base/'results'/source['results_dir']/'result.json').read_text())['exit_code']==0
hot=json.loads((base/'independent-hotfix-image.json').read_text());assert hot['exit_code']==0 and not hot['includes_personal_Go_MCP']
assert json.loads(subprocess.check_output(['docker','image','inspect',hot['image']]))[0]['Id']==hot['image_id']
out=base/'results/independent-hotfix-offline';out.mkdir(exist_ok=False)
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
app=project+'-hotfix-offline-probe';redis=project+'-hotfix-offline-redis';db='knowhub_hotfix_offline_probe';files=project+'_hotfix_offline_files';network=project+'_isolated';rows=[];started=[]
env=dict(x.split('=',1)for x in(base/'private/app.env').read_text().splitlines())
reference=Path(source['revoked_file_reference']).resolve();assert reference.parent==base/'private'
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

def sql(q):return subprocess.check_output(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d',db,'-Atc',q]).decode().strip()
def api(name,method,path,body=None):
 code="""import urllib.request,urllib.error,json,os
origin=ORIGIN
def decode(raw):
 try:return json.loads(raw or b'null')
 except (ValueError,TypeError):return raw.decode(errors='replace')
def req(method,path,body=None,token=None):
 h={'Content-Type':'application/json','X-Tenant-ID':'99'}
 if token:h['Authorization']='Bearer '+token
 r=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,method=method,headers=h)
 try:
  with urllib.request.urlopen(r,timeout=30)as s:return s.status,decode(s.read())
 except urllib.error.HTTPError as e:return e.code,decode(e.read())
status,auth=req('POST','/api/v1/auth/login',{'email':'owner@example.invalid','password':os.environ['KNOWHUB_V1_TEST_PASSWORD']});assert status==200
status,data=req(METHOD,PATH,BODY,auth['token']);print(json.dumps({'status':status,'data':data}))
""".replace('ORIGIN',repr('http://'+app+':8080')).replace('METHOD',repr(method)).replace('PATH',repr(path)).replace('BODY',repr(body))
 args=['docker','run','--rm','--network',network,'--env-file',str(base/'private/app.env'),'--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','-c',code]
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
result={}
try:
 run('create-clone',compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE '+db+' TEMPLATE template0'])
 backup=Path(source['backup']).resolve();assert backup.parent==base/'private'
 run('restore-synthetic-clone',compose+['exec','-T','postgres','pg_restore','-U','postgres','-d',db],backup.read_bytes())
 run('clone-synthetic-files',['docker','run','--rm','--network','none','-v',project+'_files:/old:ro','-v',files+':/new','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','sh','-c','cp -a /old/. /new/'])
 run('start-private-redis',['docker','run','-d','--name',redis,'--label','knowhub.v1=hotfix-probe','--network',network,'redis:7.0-alpine','redis-server','--save','','--appendonly','no']);started.append(redis)
 args=['docker','run','-d','--name',app,'--label','knowhub.v1=hotfix-probe','--network',network,'--env-file',str(base/'private/app.env'),'-e','DB_DRIVER=postgres','-e','DB_HOST=postgres','-e','DB_PORT=5432','-e','DB_USER=postgres','-e','DB_NAME='+db,'-e','REDIS_ADDR='+redis+':6379','-e','REDIS_PASSWORD=','-e','DUCKDB_SKIP_EXTENSION_LOAD=1','-e','AUTO_RECOVER_DIRTY=false','-e','FEISHU_LEGACY_SYNC_SUSPENDED=false','-e','FEISHU_SAFE_INCREMENTAL_ENABLED=true','-e','FEISHU_DOCX_PARSE_MODE=blocks','-e','FEISHU_API_RETRY_MAX=0','-e','WEKNORA_TENANT_ENABLE_RBAC=true','-e','RETRIEVE_DRIVER=postgres','-e','STORAGE_TYPE=local','-e','LOCAL_STORAGE_BASE_DIR=/data/files','-e','SSRF_WHITELIST=model,feishu','-v',files+':/data/files','--entrypoint','/app/WeKnora',hot['image']]
 run('start-independent-hotfix',args);started.append(app)
 healthy=False
 for _ in range(60):
  state=json.loads(subprocess.check_output(['docker','inspect',app]))[0]
  if not state['State']['Running']:break
  r=subprocess.run(['docker','exec',app,'curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if r.returncode==0 and r.stdout==b'200':healthy=True;break
  time.sleep(1)
 assert healthy,'independent RC3 hotfix did not cold-start'
 assert api('no-personal-Go-MCP','GET','/api/v1/mcp/personal')['status']==404
 assert api('revoked-source-REST-denied','GET','/api/v1/knowledge/'+source['sources']['drive']['knowledge_id'])['status']in(403,404)
 config=('url="http://127.0.0.1:8080'+revoked_link+'"\noutput="/dev/null"\nsilent\nmax-time=10\nwrite-out="%{http_code}"\n').encode()
 assert run('unexpired-anonymous-file-denied',['docker','exec','-i',app,'curl','--config','-'],config).stdout.decode().strip()in('403','404')
 wiki=source['sources']['wiki'];before=wiki['knowledge_id']
 fixture('wiki',1792555500,'wiki hotfixcanary verified source text');sync(wiki['source_id'],'wiki-update')
 assert sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+wiki['source_id']+"'")==before
 assert sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+before+"' AND content ||| 'hotfixcanary'")!='0'
 sql("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy) SELECT 'hotfix-drive-probe','Synthetic hotfix Drive',99,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy FROM knowledge_bases WHERE id='public-kb'")
 fixture('drive',1792555500,'drive hotfixcanary verified source text')
 created=api('drive-source-create','POST','/api/v1/datasource',{'name':'Synthetic independent Drive','type':'feishu_drive','knowledge_base_id':'hotfix-drive-probe','sync_mode':'incremental','sync_deletions':True,'config':{'type':'feishu_drive','credentials':{'app_id':'synthetic','app_secret':'synthetic-not-secret','base_url':'http://feishu:8000'},'resource_ids':['root']}});assert created['status']==201
 ds=created['data']['id'];sync(ds,'drive-first');kid=sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
 fixture('drive',1792555560,'drive hotfixupdatedcanary verified source text');sync(ds,'drive-update')
 assert sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")==kid
 assert sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"' AND content ||| 'hotfixupdatedcanary'")!='0'
 assert sql("SELECT version||':'||dirty FROM schema_migrations")=='3036:false'
 run('enabled-hotfix-log',['docker','logs',app])
 run('stop-enabled-hotfix',['docker','stop','--time','5',app])
 offargs=list(args);app=app+'-off';offargs[offargs.index('--name')+1]=app
 offargs=['FEISHU_SAFE_INCREMENTAL_ENABLED=false'if x=='FEISHU_SAFE_INCREMENTAL_ENABLED=true'else'FEISHU_LEGACY_SYNC_SUSPENDED=true'if x=='FEISHU_LEGACY_SYNC_SUSPENDED=false'else x for x in offargs]
 run('start-hardened-RC3-feature-off',offargs);started.append(app)
 offhealthy=False
 for _ in range(60):
  r=subprocess.run(['docker','exec',app,'curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if r.returncode==0 and r.stdout==b'200':offhealthy=True;break
  time.sleep(1)
 assert offhealthy
 assert api('off-revoked-REST-denied','GET','/api/v1/knowledge/'+source['sources']['drive']['knowledge_id'])['status']in(403,404)
 assert run('off-unexpired-HMAC-denied',['docker','exec','-i',app,'curl','--config','-'],config).stdout.decode().strip()in('403','404')
 state=json.loads(subprocess.check_output(['docker','inspect',app]))[0];assert not state['HostConfig']['PortBindings']
 result=dict(exit_code=0,candidate_sha256=hot['candidate_sha256'],runtime_source_sha256=hot['runtime_source_sha256'],image=hot['image'],image_id=hot['image_id'],cold_start=True,duckdb_extensions='SKIP_existing_offline_startup_option_excel_not_verified',real_Wiki_Drive_Worker=True,stable_knowledge_ids=True,personal_Go_MCP_included=False,revoked_REST_denied=True,original_unexpired_HMAC_denied=True,feature_off_hardened_reads=True,schema='3036:false',source_data='synthetic backup clone only',production_changes=False,formal_cloud='NOT_TESTED')
except Exception as error:result=dict(exit_code=1,error_class=type(error).__name__,message=str(error),production_changes=False)
finally:
 if app in started:run('hotfix-app-log',['docker','logs',app],require=False)
 for name in reversed(started):run('stop-'+name,['docker','stop','--time','5',name],require=False)
 (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
