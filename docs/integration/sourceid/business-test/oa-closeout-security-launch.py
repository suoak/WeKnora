"""New synthetic security coverage only; no real SID or business DB access."""
import os
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
assert json.loads((W/'recovery.json').read_text())['result']=='RESTORED'
T=R/'private/security-closeout';T.mkdir(mode=0o700,exist_ok=True)
if (T/'result.json').exists():
 previous=json.loads((T/'result.json').read_text());assert previous['result']!='RUNNING'
 (T/('attempt-'+datetime.datetime.now(datetime.timezone.utc).strftime('%H%M%S')+'.json')).write_text(json.dumps(previous))
worker=r'''import os,sys,json,pathlib,subprocess,time,secrets,traceback,threading,re
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');T=R/'private/security-closeout';F=R/'private/mock'
IMAGE='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b';PG='knowhub-oa-closeout-pg';APP='knowhub-oa-closeout-app';owned=[]
out={'result':'RUNNING','stage':'prepare','synthetic_only':True,'real_sid_calls':0,'real_database_access':False}
def save():(T/'result.json').write_text(json.dumps(out))
def cmd(args,data=None,timeout=90):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if p.returncode:raise RuntimeError('controlled_synthetic_operation_failed')
 return p.stdout
def inspect(n,f):return json.loads(cmd(['docker','inspect','--format','{{json '+f+'}}',n]))
def db(q):return cmd(['docker','exec','-i',PG,'psql','-X','-qAt','-U','security_mock','-d','jit_mock_security','-v','ON_ERROR_STOP=1'],q.encode()).decode().strip()
save();stream=None;stop=threading.Event();stages={};safe_http=[]
try:
 for n in (PG,APP,'knowhub-oa-closeout-redis','knowhub-oa-closeout-provider','knowhub-oa-closeout-frontend'):assert subprocess.run(['docker','inspect',n],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
 pgimage=inspect('knowhub-jit-fresh-postgres-1','.Image');redisimage=inspect('knowhub-jit-fresh-redis-1','.Image');frontimage=inspect('knowhub-jit-fresh-frontend-1','.Image')
 cmd(['docker','run','-d','--name',PG,'--network','none','--log-driver','none','--memory','768m','--tmpfs','/var/lib/postgresql/data:rw','-e','PGDATA=/var/lib/postgresql/data/mock','-e','POSTGRES_USER=security_mock','-e','POSTGRES_PASSWORD=synthetic-only','-e','POSTGRES_DB=jit_mock_security',pgimage]);owned.append(PG)
 for _ in range(45):
  if subprocess.run(['docker','exec',PG,'pg_isready','-U','security_mock','-d','jit_mock_security'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('mock_pg_not_ready')
 with (F/'empty-fresh.dump').open('rb') as f:
  p=subprocess.run(['docker','exec','-i',PG,'pg_restore','-U','security_mock','-d','jit_mock_security','--clean','--if-exists','--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90);assert p.returncode==0
 assert db('SELECT COUNT(*) FROM users')=='0'
 base=dict(x.split('=',1) for x in (F/'new.env').read_text().splitlines() if '=' in x)
 base.update(DB_HOST='127.0.0.1',DB_NAME='jit_mock_security',DB_USER='security_mock',DB_PASSWORD='synthetic-only',REDIS_ADDR='127.0.0.1:6379',REDIS_PASSWORD='',JWT_SECRET=secrets.token_urlsafe(48),SYSTEM_AES_KEY=secrets.token_hex(16),LOG_LEVEL='warn',SOURCEID_DIAGNOSTICS_ENABLE='true')
 (T/'mock.env').write_text(''.join(k+'='+v+'\n' for k,v in base.items()))
 code=(F/'client.py').read_text().split("case=os.environ['SYNTHETIC_CASE']")[0]
 code+=r"""
import subprocess,uuid,base64,time,hmac,hashlib
assert os.environ['DB_NAME']=='jit_mock_security' and os.environ['OIDC_AUTH_ISSUER_URL']=='https://127.0.0.1:8443'
def q(sql):
 e=dict(os.environ);e['PGPASSWORD']=e['DB_PASSWORD']
 p=subprocess.run(['psql','-X','-qAt','-h','127.0.0.1','-U','security_mock','-d','jit_mock_security','-v','ON_ERROR_STOP=1'],input=("SET log_statement='none';SET log_min_error_statement='panic';"+sql).encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=e)
 if p.returncode:
  import re
  columns=re.findall(r'null value in column "([a-z_]+)"',p.stderr.decode())
  raise RuntimeError('synthetic_sql_columns_'+','.join(columns))
 return p.stdout.decode().strip()
def payload(t):return json.loads(base64.urlsafe_b64decode(t.split('.')[1]+'=='))
def login(who):
 st,b,_=req('/api/v1/auth/oidc/url');assert st==200
 auth=b['authorization_url'];assert urllib.parse.urlsplit(auth).hostname=='127.0.0.1'
 st,_,loc=req(auth+'&'+urllib.parse.urlencode({'synthetic_user':'capacity-security-'+who}));assert st==302
 u=urllib.parse.urlsplit(loc);assert u.hostname=='knowhub.ruijie.com.cn'
 st,_,loc=req(u.path+'?'+u.query);assert st==302 and '#oidc_complete=1' in loc
 st,b,_=req('/api/v1/auth/oidc/result','POST',{},extra={'X-KnowHub-OIDC-Result':'1','Origin':'http://knowhub.ruijie.com.cn'})
 assert st==200 and b.get('token') and 'refresh_token' not in b;return b['token'],b['user']
def member(uid,tenant,role):
 q("INSERT INTO tenant_members(user_id,tenant_id,role,status,joined_at,created_at,updated_at) VALUES ('"+uid+"',"+str(tenant)+",'"+role+"','active',NOW(),NOW(),NOW())")
def switch(t,tenant):
 st,b,_=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':tenant},t);assert st==200 and b.get('token') and 'refresh_token' not in b;return b['token']
allowed=int(q("INSERT INTO tenants(name,business,status) VALUES ('synthetic-allowed','synthetic','active') RETURNING id"))
denied=int(q("INSERT INTO tenants(name,business,status) VALUES ('synthetic-denied','synthetic','active') RETURNING id"))
def kb(tenant):
  uid=str(uuid.uuid4());q("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES ('"+uid+"','synthetic-kb',"+str(tenant)+",'','')");return uid
ak=kb(allowed);dk=kb(denied)
owner,ou=login('resource-owner');member(ou['id'],denied,'owner');owner=switch(owner,denied)
assert req('/api/v1/knowledge-bases/'+dk,token=owner)[0]==200
viewer,vu=login('viewer');member(vu['id'],allowed,'viewer');viewer=switch(viewer,allowed)
assert req('/api/v1/knowledge-bases/'+ak,token=viewer)[0]==200
assert q("SELECT COUNT(*) FROM tenant_members WHERE user_id='"+vu['id']+"' AND tenant_id="+str(denied))=='0'
assert q("SELECT COUNT(*) FROM kb_shares WHERE knowledge_base_id='"+dk+"' AND deleted_at IS NULL")=='0'
kb_denied=req('/api/v1/knowledge-bases/'+dk,token=viewer)[0]
switch_denied=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':denied},viewer)[0]
assert kb_denied in (403,404) and switch_denied==403
results={'permission':{'resource_exists':True,'authorized_owner_read':200,'allowed_kb':200,'forbidden_kb_status':kb_denied,'forbidden_existing_space_switch':switch_denied,'no_membership_or_share':True,'original_admin_modified':False},'expiry':{}}
for scenario in ('access','idle','absolute','admission','policy'):
 t,u=login(scenario);member(u['id'],allowed,'viewer');sid=payload(t)['session_id'];assert str(uuid.UUID(sid))==sid
 until=q("SELECT expires_at::text FROM source_id_sessions WHERE id='"+sid+"'")
 t=switch(t,allowed);assert payload(t)['session_id']==sid and q("SELECT expires_at::text FROM source_id_sessions WHERE id='"+sid+"'")==until
 assert req('/api/v1/auth/me',token=t)[0]==200
 if scenario=='access':
  p=payload(t);p['exp']=int(time.time())-120
  enc=lambda b:base64.urlsafe_b64encode(b).decode().rstrip('=')
  body=t.split('.')[0]+'.'+enc(json.dumps(p,separators=(',',':')).encode());expired=body+'.'+enc(hmac.new(os.environ['JWT_SECRET'].encode(),body.encode(),hashlib.sha256).digest())
  q("UPDATE auth_tokens SET token='"+expired+"' WHERE token='"+t+"'");t=expired
 elif scenario=='idle':q("UPDATE source_id_local_sessions SET last_seen_at=NOW()-INTERVAL '31 minutes' WHERE id='"+sid+"'")
 elif scenario=='absolute':q("UPDATE source_id_sessions SET expires_at=NOW()-INTERVAL '1 second' WHERE id='"+sid+"'")
 elif scenario=='admission':q("UPDATE source_id_local_sessions SET admission_valid_until=NOW()-INTERVAL '1 second' WHERE id='"+sid+"'")
 else:q("UPDATE source_id_local_link_policies SET policy_ref='synthetic-changed-policy' WHERE id='"+sid+"'")
 assert req('/api/v1/auth/me',token=t)[0]==401
 assert req('/api/v1/auth/switch-tenant','POST',{'tenant_id':allowed},t)[0]==401
 assert req('/login')[0]==200 and req('/api/v1/auth/me',token=t)[0]==401
 assert req('/api/v1/auth/refresh','POST',{'refresh_token':t})[0] in (400,401,403)
 if scenario=='policy':
  q("UPDATE source_id_local_link_policies SET policy_ref='synthetic-isolated-mock-only' WHERE id='"+sid+"'")
  assert req('/api/v1/auth/me',token=t)[0]==401
 results['expiry'][scenario]={'denied':True,'switch_cannot_extend':True,'page_refresh_cannot_restore':True,'refresh_denied':True,'absolute_deadline_preserved_before_expiry':True,'method':'synthetic JWT/DB time or policy mutation; not natural expiry'}
assert q("SELECT COUNT(*) FROM auth_tokens WHERE token_type='refresh_token'")=='0'
assert req('/api/v1/auth/logout','POST',{},owner)[0]==200 and req('/api/v1/auth/me',token=owner)[0]==401
assert req('/api/v1/auth/logout','POST',{},viewer)[0]==200 and req('/api/v1/auth/me',token=viewer)[0]==401
results.update(result='PASS',real_sid_called=False,real_database_access=False)
print(json.dumps(results))
"""
 (T/'client.py').write_text(code)
 mounts=['--mount','type=bind,source='+str(F)+',target=/fixture,readonly','--mount','type=bind,source='+str(T)+',target=/security,readonly']
 for n,image,extra in [('knowhub-oa-closeout-redis',redisimage,[]),('knowhub-oa-closeout-provider',IMAGE,mounts+['--entrypoint','python3']), (APP,IMAGE,mounts+['--env-file',str(T/'mock.env'),'--tmpfs','/data/files:rw']),('knowhub-oa-closeout-frontend',frontimage,['-e','APP_HOST=127.0.0.1','-e','APP_PORT=8080','-e','APP_SCHEME=http'])]:
  args=['docker','run','-d','--name',n,'--network','container:'+PG,'--log-driver','none']+extra+[image]+(['/fixture/provider.py'] if 'provider' in n else [])
  cmd(args);owned.append(n)
 for _ in range(60):
  if subprocess.run(['docker','exec',APP,'python3','-c',"import urllib.request;urllib.request.urlopen('http://127.0.0.1:8080/health',timeout=1)"],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('synthetic_app_not_ready')
 sys.path.insert(0,'/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/monitor/business-test');from export_token_diagnostics import extract
 stream=subprocess.Popen(['docker','attach','--no-stdin','--sig-proxy=false',APP],stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 def collect():
  for raw in stream.stdout:
   if len(raw)>65536:continue
   line=raw.decode('utf-8','replace');safe=extract(line)
   if safe and safe['event']=='sourceid_token_http':safe_http.append(safe)
   pos=line.find('{"event"')
   if pos<0:continue
   try:v=json.JSONDecoder().raw_decode(line[pos:])[0]
   except (ValueError,TypeError):continue
   if isinstance(v,dict) and v.get('event')=='sourceid_stage' and v.get('stage') in ('authorize','callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result'):
    k=v['stage']+(':pass' if v.get('pass') is True else ':fail');stages[k]=stages.get(k,0)+1
 thread=threading.Thread(target=collect,daemon=True);thread.start();out['stage']='api_security';save()
 p=subprocess.run(['docker','exec',APP,'python3','/security/client.py'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
 if p.returncode:
  out['client_failure_lines']=re.findall(r'client.py", line ([0-9]+)',p.stderr.decode());out['synthetic_sql_columns']=re.findall(r'synthetic_sql_columns_([a-z_,]+)',p.stderr.decode());raise RuntimeError('synthetic_client_assertion_failed')
 out.update(result='PASS',stage='complete',checks=json.loads(p.stdout),diagnostic_stage_counts=stages,diagnostic_http_event_count=len(safe_http),warn_level_captures_diagnostics=bool(stages))
except BaseException as e:out.update(result='FAIL',failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__],diagnostic_stage_counts=stages)
finally:
 if stream is not None:stream.terminate()
 for n in reversed(owned):subprocess.run(['docker','rm','-f',n],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 out['containers_removed']=True;out['utc']=__import__('datetime').datetime.now(__import__('datetime').timezone.utc).isoformat();save()
'''
(T/'worker.py').write_text(worker)
subprocess.Popen(['python3',str(T/'worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED_SYNTHETIC_SECURITY_CHECKS','real_sid_calls':0,'original_environment_changed':False}))
