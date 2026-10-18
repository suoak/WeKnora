"""User-authorized one real JIT window. Never initiates SID authentication."""
import os
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
assert json.loads((R/'final-audit.json').read_text())['result']=='PASS'
assert not (R/'real-window').exists(), 'one_window_only'
W=R/'real-window';W.mkdir(mode=0o700)
module=r'''import os,sys,pathlib,json,subprocess,time,datetime,fcntl,uuid,select,re,signal,shutil
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
OLD='knowhub-company-test';NEW='knowhub-jit-fresh';ENTRY='knowhub-company-entry-entry-1'
CONFIG=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf')
IMAGE='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b';REV='5f988be51779f1a22bd9d027b7503fe6a8749f62'
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');sys.path.insert(0,str(B/'monitor/business-test'))
from export_token_diagnostics import extract
from protocol_monitor import KNOWN,CATEGORIES
REF='user-conversation:2026-10-18:fresh-jit-single-business-window-v1'
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def atomic(path,v):
 raw=json.dumps(v).encode();assert len(raw)<=1048576
 tmp=path.with_suffix('.tmp');tmp.write_bytes(raw);tmp.chmod(0o600);tmp.replace(path)
def cmd(args,data=None,timeout=60):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if p.returncode:raise RuntimeError('controlled_operation_failed')
 return p.stdout
def inspect(n,f):return json.loads(cmd(['docker','inspect','--format','{{json '+f+'}}',n]))
def env(n):return dict(x.split('=',1) for x in inspect(n,'.Config.Env') if '=' in x)
def sql(q,fresh=True,write=False):
 pg=NEW+'-postgres-1' if fresh else OLD+'-postgres-1';u='fresh_owner' if fresh else 'clean_owner';db='knowhub_jit_fresh' if fresh else 'knowhub_oa_sso_pilot'
 prefix="SET log_statement='none';SET log_min_error_statement='panic';SET log_parameter_max_length_on_error=0;"
 return cmd(['docker','exec','-i',pg,'psql','-X','-qAt','-U',u,'-d',db,'-v','ON_ERROR_STOP=1'],(prefix+('BEGIN;' if write else 'BEGIN READ ONLY;')+q+';'+('COMMIT;' if write else 'ROLLBACK;')).encode()).decode().strip()
def jq(q,fresh=True):return json.loads(sql(q,fresh))
def compose_app():
 cmd(['docker','compose','-p',NEW,'--project-directory',str(R),'-f',str(R/'compose.json'),'up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=90)
 for _ in range(45):
  if inspect(NEW+'-app-1','.State').get('Health',{}).get('Status')=='healthy':return
  time.sleep(1)
 raise RuntimeError('fresh_app_not_healthy')
def set_status(v):v['utc']=utc();atomic(W/'status.json',v)
def baseline():
 tables=('users','tenants','tenant_members','knowledge_bases','knowledges','kb_shares','external_identities','source_id_binding_reviews','source_id_sessions','source_id_jit_registrations','source_id_link_decisions')
 return {t:jq("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT to_jsonb(v)-'password_hash'-'email'-'username'-'api_key'-'subject'-'employee_no' AS row FROM "+t+" v ORDER BY id)x",False) for t in tables}
def health(marker):
 import urllib.request,urllib.error
 class NR(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a,**k):return None
 o=urllib.request.build_opener(urllib.request.ProxyHandler({}),NR());out={}
 for number,path,want in ((80,'/login',200),(8080,'/login',401),(80,'/health',200),(8080,'/health',200),(80,'/api/v1/auth/oidc/config',200),(8080,'/api/v1/auth/oidc/config',200),(80,'/api/v1/auth/oidc/callback',302),(8080,'/api/v1/auth/oidc/callback',302)):
  try:r=o.open('http://127.0.0.1:'+str(number)+path,timeout=8)
  except urllib.error.HTTPError as e:r=e
  with r:
   body=r.read(1048576);assert r.status==want and r.headers.get('X-KnowHub-Environment')==marker
   if path.endswith('/callback'):assert 'invalid_state' in r.headers.get('Location','')
   if number==80 and path=='/login':
    asset=next(x.decode() for x in re.findall(rb'<script[^>]+src="([^"]+)"',body) if b'/assets/' in x)
    with o.open('http://127.0.0.1'+asset,timeout=8) as a:assert a.status==200 and a.headers.get('X-KnowHub-Environment')==marker
   if path.endswith('/config'):
    cfg=json.loads(body);assert cfg.get('sourceid_basic_encoding')=='oauth_form'
    if marker.startswith('jit-fresh'):assert cfg['enabled'] is True and cfg['redirect_uri']=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
   out[str(number)+path]=r.status
 return out
def fresh_meta():
 return jq("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'identities',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'members',(SELECT COUNT(*) FROM tenant_members),'kbs',(SELECT COUNT(*) FROM knowledge_bases),'privileged',(SELECT COUNT(*) FROM users WHERE is_system_admin OR can_access_all_tenants OR tenant_id<>0),'active_sessions',(SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()),'active_access',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked AND expires_at>NOW()),'refresh',(SELECT COUNT(*) FROM auth_tokens WHERE token_type='refresh_token'),'authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>''),'consumed_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>'' AND consumed))")
def restore(reason):
 with (W/'restore.lock').open('a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX)
  if (W/'recovery.json').exists() and json.loads((W/'recovery.json').read_text())['result']=='RESTORED':return
  result={'reason':reason,'steps':{},'utc':utc()}
  def step(name,action):
   try:action();result['steps'][name]='PASS'
   except BaseException:result['steps'][name]='FAIL'
   atomic(W/'recovery.json',dict(result,result='RESTORING'))
  def save_evidence():atomic(W/'before-restore-meta.json',fresh_meta())
  step('save_sanitized_evidence',save_evidence)
  def close():
   if (W/'fresh-before.env').exists():(R/'private/app.env').write_bytes((W/'fresh-before.env').read_bytes());compose_app()
   e=env(NEW+'-app-1');assert e['OIDC_AUTH_ENABLE']=='false' and e['SOURCEID_JIT_ENABLE']=='false' and e['SOURCEID_PROTOCOL_ONLY']=='true'
  step('close_fresh_gates',close)
  def revoke():
   rows=jq("SELECT COALESCE(json_agg(json_build_object('id',s.id,'user_id',s.user_id)),'[]') FROM source_id_sessions s JOIN source_id_jit_registrations j ON j.user_id=s.user_id")
   total=int(sql('SELECT COUNT(*) FROM source_id_sessions'));assert total==len(rows) and len(rows)<=1
   for row in rows:
    assert all(str(uuid.UUID(row[k]))==row[k] for k in ('id','user_id'))
    sql("UPDATE source_id_sessions SET revoked=TRUE WHERE id='"+row['id']+"' AND user_id='"+row['user_id']+"';UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id='"+row['id']+"'",write=True)
   m=fresh_meta();assert m['active_sessions']==m['active_access']==0
  step('revoke_exact_jit_session_and_access',revoke)
  def gateway():
   if (W/'original-gateway.conf').exists():
    CONFIG.write_bytes((W/'original-gateway.conf').read_bytes());cmd(['docker','exec',ENTRY,'nginx','-t'])
    # Disconnect before resolving generic app/frontend aliases: both projects
    # have these service names. Restart also clears resolver/keepalive state.
    if NEW+'_isolated' in inspect(ENTRY,'.NetworkSettings.Networks'):cmd(['docker','network','disconnect',NEW+'_isolated',ENTRY])
    cmd(['docker','restart','--time','5',ENTRY],timeout=40)
  step('restore_gateway_and_drain_connections',gateway)
  def unpause():
   if inspect(OLD+'-app-1','.State.Paused'):cmd(['docker','unpause',OLD+'-app-1'])
  step('resume_original_app',unpause)
  def verify():
   health('company-test-verified-email-db3030')
   if (W/'original-baseline.json').exists():assert baseline()==json.loads((W/'original-baseline.json').read_text())
   if (W/'original-containers.json').exists():
    for n,v in json.loads((W/'original-containers.json').read_text()).items():assert inspect(n,'.Id')==v['id'] and inspect(n,'.Image')==v['image']
   assert sql("SELECT version::text||'|'||dirty FROM schema_migrations",False)=='3030|false'
   counts=jq("SELECT json_build_object('documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'vectors',(SELECT COUNT(*) FROM embeddings))",False);assert counts=={'documents':3,'chunks':2717,'vectors':2717}
   atomic(W/'original-restored-health.json',{'health':True,'original_baseline_unchanged':True,'counts':counts})
  step('verify_original_routes_data_and_permissions',verify)
  def disconnect():
   if NEW+'_isolated' in inspect(ENTRY,'.NetworkSettings.Networks'):cmd(['docker','network','disconnect',NEW+'_isolated',ENTRY])
  step('disconnect_only_fresh_network',disconnect)
  result['result']='RESTORED' if all(v=='PASS' for v in result['steps'].values()) else 'MANUAL_RECOVERY_REQUIRED';atomic(W/'recovery.json',result)
def watch():
 while True:
  if (W/'recovery.json').exists() and json.loads((W/'recovery.json').read_text()).get('result') in ('RESTORED','MANUAL_RECOVERY_REQUIRED'):return
  s=json.loads((W/'watch.json').read_text())
  try:os.kill(s['worker_pid'],0);alive=True
  except ProcessLookupError:alive=False
  if not alive or time.time()>s['hard_deadline'] or time.time()-s['heartbeat']>240:restore('WATCHDOG_RECOVERY');return
  time.sleep(2)
def execute():
 processes=[];reason='PREPARATION_FAILED';events=[];http=[];gateway=[];snapshot={};runid=str(uuid.uuid4())
 def heartbeat():atomic(W/'watch.json',{'worker_pid':os.getpid(),'hard_deadline':start+900,'heartbeat':time.time()})
 start=time.time();heartbeat()
 try:
  assert inspect(OLD+'-app-1','.Image')==inspect(NEW+'-app-1','.Image')==IMAGE
  assert inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REV
  assert sql("SELECT version::text||'|'||dirty FROM schema_migrations")=='3030|false'
  m=fresh_meta();assert m['users']==m['identities']==m['jit']==m['authorizations']==m['active_sessions']==0
  assert int(sql("SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>'' AND NOT consumed AND expires_at>NOW()",False))==0
  atomic(W/'approval.json',{'reference':REF,'approval_source':'current_user_explicit_message','approved_by':'current_requesting_user_for_this_test','executor':'Codex controlled process on 172.30.232.42 as aqyfzx','recorded_at':utc(),'scopes':['jit_policy','session_policy','temporary_80_8080_maintenance','one_real_sid_transaction'],'domain':'ruijie.com.cn','minutes':[60,60,30,60],'refresh':False,'prompt':False,'production_approval':False})
  (W/'fresh-before.env').write_bytes((R/'private/app.env').read_bytes());(W/'original-gateway.conf').write_bytes(CONFIG.read_bytes())
  originals={}
  for x in ('app','frontend','postgres','redis','docreader','sid-network'):
   n=OLD+'-'+x+'-1';originals[n]={'id':inspect(n,'.Id'),'image':inspect(n,'.Image')}
  atomic(W/'original-containers.json',originals)
  maintenance=CONFIG.read_bytes();assert maintenance.count(b'proxy_pass http://$upstream;')==2
  CONFIG.write_bytes(maintenance.replace(b'proxy_pass http://$upstream;',b'return 503;'));cmd(['docker','exec',ENTRY,'nginx','-t']);cmd(['docker','restart','--time','5',ENTRY],timeout=40)
  cmd(['docker','pause',OLD+'-app-1']);heartbeat();atomic(W/'original-baseline.json',baseline())
  # Original writers frozen throughout this short maintenance/test window.
  with (W/'original-db3030.dump').open('wb') as f:
   z=subprocess.run(['docker','exec',OLD+'-postgres-1','pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=150);assert z.returncode==0
  with (W/'original-files.tar').open('wb') as f:
   z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','tar',IMAGE,'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=90);assert z.returncode==0
  heartbeat();e=env(OLD+'-app-1');new=dict(line.split('=',1) for line in (R/'private/app.env').read_text().splitlines() if '=' in line)
  (W/'original-app-env-protected.json').write_text(json.dumps(e))
  files=inspect(OLD+'-app-1','.Config.Labels')['com.docker.compose.project.config_files'].split(',');atomic(W/'original-compose-order.json',files)
  for i,f in enumerate(files):(W/('original-compose-'+str(i)+'.json')).write_bytes(pathlib.Path(f).read_bytes())
  new.update({k:v for k,v in e.items() if k.startswith('OIDC_AUTH_') or k.startswith('OIDC_USER_INFO_')})
  new.update(OIDC_AUTH_ENABLE='true',SOURCEID_ENABLE='true',SOURCEID_PROTOCOL_ONLY='false',SOURCEID_CONTRACT_CONFIRMED='true',SOURCEID_CLAIMS_SOURCE='userinfo',SOURCEID_EMPLOYEE_CLAIM='attributes.RJGH',SOURCEID_EMAIL_BINDING_ENABLE='true',SOURCEID_EMAIL_CONTRACT_CONFIRMED='true',SOURCEID_AUTO_BINDING_ENABLE='true',SOURCEID_AUTO_BINDING_POLICY_REF=REF,SOURCEID_ACCOUNT_SCOPE='enterprise_all',SOURCEID_ACCOUNT_LINK_MODE='verified_email_policy',SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED='true',SOURCEID_VERIFIED_EMAIL_POLICY_REF=REF,SOURCEID_SESSION_MODE='local_approved',SOURCEID_LOCAL_POLICY_REF=REF,SOURCEID_LOCAL_ACCESS_TTL='60m',SOURCEID_LOCAL_ABSOLUTE_TTL='60m',SOURCEID_LOCAL_IDLE_TTL='30m',SOURCEID_LOCAL_REFRESH_ENABLE='false',SOURCEID_ADMISSION_MODE='oidc_sso_approved',SOURCEID_ADMISSION_POLICY_REF=REF,SOURCEID_ADMISSION_MAX_AGE='60m',SOURCEID_SSO_POLICY_CONFIRMED='true',SOURCEID_FORCE_REAUTH_PROMPT_LOGIN='false',SOURCEID_BASIC_ENCODING='oauth_form',SOURCEID_JIT_ENABLE='true',SOURCEID_JIT_POLICY_CONFIRMED='true',SOURCEID_JIT_POLICY_REF=REF,SOURCEID_JIT_EMAIL_DOMAINS='ruijie.com.cn',SOURCEID_DIAGNOSTICS_ENABLE='true',SOURCEID_LOGOUT_ENABLE='false',SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED='false',DISABLE_REGISTRATION='true',AUTO_MIGRATE='false')
  # Diagnostics are emitted at WARN; an inherited ERROR level suppresses them.
  # Applies only to a future authorized window; never replays this real test.
  new['LOG_LEVEL']='warn'
  assert new['JWT_SECRET']!=e['JWT_SECRET'] and new['SYSTEM_AES_KEY']!=e['SYSTEM_AES_KEY'] and new['DB_NAME']=='knowhub_jit_fresh'
  assert new['OIDC_AUTH_REDIRECT_URI']=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback' and new['OIDC_AUTH_CLIENT_SECRET']
  (R/'private/app.env').write_text(''.join(k+'='+v+'\n' for k,v in new.items()));compose_app();loaded=env(NEW+'-app-1');assert all(loaded.get(k)==v for k,v in new.items())
  assert fresh_meta()['users']==0;heartbeat()
  assert NEW+'_isolated' not in inspect(ENTRY,'.NetworkSettings.Networks')
  cmd(['docker','network','connect',NEW+'_isolated',ENTRY])
  candidate=(R/'private/route/candidate-observed.conf').read_bytes()
  CONFIG.write_bytes(candidate);cmd(['docker','exec',ENTRY,'nginx','-t']);cmd(['docker','restart','--time','5',ENTRY],timeout=40)
  routes=health('jit-fresh-isolated-db3030');assert fresh_meta()['authorizations']==0
  # Probe disabled registration with an empty invalid body: never creates a user.
  check="import urllib.request,urllib.error; r=urllib.request.Request('http://127.0.0.1:8080/api/v1/auth/register',data=b'{}',headers={'Content-Type':'application/json'},method='POST');\ntry: urllib.request.urlopen(r,timeout=5);raise AssertionError()\nexcept urllib.error.HTTPError as e: assert e.code==403"
  cmd(['docker','exec',NEW+'-app-1','python3','-c',check]);assert fresh_meta()['users']==0
  buffers={};seen=set();stage_traces={};ready=time.time()
  for name in (NEW+'-app-1',ENTRY):
   proc=subprocess.Popen(['docker','attach','--no-stdin','--sig-proxy=false',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);os.set_blocking(proc.stdout.fileno(),False);processes.append(proc);buffers[proc.pid]=b''
  def evidence():
   atomic(W/'evidence.json',{'run_id':runid,'events':events,'http_events':http,'gateway_events':gateway,'database':snapshot,'raw_logs_saved':False,'real_sid_requests_by_agent':0,'business_routes':routes,'portal_rendered':'PENDING_USER_CONFIRMATION'})
  set_status({'phase':'READY','run_id':runid,'real_sid_requests_by_agent':0,'deadline_utc':datetime.datetime.fromtimestamp(ready+480,datetime.timezone.utc).isoformat(),'new_database_empty':True,'public_password_registration_disabled':True,'watchdog_armed':True,'gateway_marker':'jit-fresh-isolated-db3030'});evidence()
  while time.time()<ready+480:
   heartbeat()
   for proc in processes:
    if proc.poll() is not None:raise RuntimeError('observation_stream_ended')
    data=buffers[proc.pid]
    if select.select([proc.stdout],[],[],0)[0]:data+=os.read(proc.stdout.fileno(),262144)
    while b'\n' in data:
     raw,data=data.split(b'\n',1)
     if len(raw)>65536:raise RuntimeError('observer_line_budget')
     line=raw.decode('utf-8','replace');safe=extract(line)
     if safe is not None:
      key=json.dumps(safe,sort_keys=True)
      if key not in seen:seen.add(key);http.append(safe)
     pos=line.find('{"event"')
     if pos<0:continue
     try:item=json.JSONDecoder().raw_decode(line[pos:])[0]
     except (ValueError,TypeError):continue
     if not isinstance(item,dict):continue
     if item.get('event')=='sourceid_stage':
      x={k:item.get(k) for k in ('trace','stage','pass','duration_ms','category')}
      if not isinstance(x['trace'],str) or not re.fullmatch('[a-f0-9-]{36}',x['trace']) or x['stage'] not in KNOWN or x['category'] not in CATEGORIES or type(x['pass']) is not bool or type(x['duration_ms']) is not int:continue
      key=json.dumps(x,sort_keys=True)
      if key not in seen:
       seen.add(key);events.append(x);stage_traces.setdefault(x['stage'],set()).add(x['trace'])
       if len(stage_traces[x['stage']])>1:reason='CORRELATION_AMBIGUOUS';raise RuntimeError('multiple_stage_traces')
       if not x['pass']:reason='PROTOCOL_OR_IDENTITY_FAILURE';raise RuntimeError('strict_stage_failure')
     if item.get('event')=='company_business_http' and set(item)=={'event','kind','status','method','source_approved','request_id'}:
      if item['source_approved'] is True and item['kind'] in ('auth_me','logout','oidc_result','oidc_callback','oidc_config') and type(item['status']) is int and item['method'] in ('GET','POST') and isinstance(item['request_id'],str) and re.fullmatch('[a-f0-9]{32}',item['request_id']):
       key=json.dumps(item,sort_keys=True)
       if key not in seen:seen.add(key);gateway.append(item)
    if len(data)>65536:raise RuntimeError('observer_buffer_budget')
    buffers[proc.pid]=data
   snapshot=fresh_meta()
   if snapshot['authorizations']>1:reason='CORRELATION_AMBIGUOUS';raise RuntimeError('second_authorization')
   if snapshot['users']>1 or snapshot['privileged'] or snapshot['members'] or snapshot['kbs'] or snapshot['refresh']:reason='JIT_PRIVILEGE_INVARIANT_FAILURE';raise RuntimeError('jit_invariant_failure')
   if snapshot['jit']==1:
    olduid=json.loads(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/first-account-preparation/selected-account.json').read_text())['existing_user_id']
    assert int(sql("SELECT COUNT(*) FROM source_id_jit_registrations WHERE user_id='"+olduid+"'"))==0
    snapshot['different_from_original_user_id']=True
   evidence()
   if sum(len(json.dumps(x)) for x in events+http+gateway)>786432:raise RuntimeError('observer_budget')
   if any(x['kind']=='logout' and x['status'] in (200,204) for x in gateway) and snapshot['jit']==snapshot['users']==snapshot['identities']==1 and snapshot['active_sessions']==snapshot['active_access']==0:
    reason='JIT_CREATED_AND_LOGOUT_OBSERVED';break
   if (W/'cancel').exists():reason='USER_CANCELLED';break
   time.sleep(.3)
  else:reason='WINDOW_TIMEOUT_INCOMPLETE'
  evidence();set_status({'phase':'RESTORING','run_id':runid,'reason':reason,'database':snapshot})
 except BaseException as e:
  atomic(W/'failure.json',{'reason':reason,'class':type(e).__name__,'stage_events':len(events),'http_events':len(http),'raw_exception_saved':False})
  atomic(W/'evidence.json',{'run_id':runid,'events':events,'http_events':http,'gateway_events':gateway,'database':snapshot,'raw_logs_saved':False})
  set_status({'phase':'RESTORING','run_id':runid,'reason':reason,'class':type(e).__name__})
 finally:
  for proc in processes:
   proc.terminate()
  restore(reason)
  recovery=json.loads((W/'recovery.json').read_text());set_status({'phase':'FINISHED' if recovery['result']=='RESTORED' else 'MANUAL_RECOVERY_REQUIRED','run_id':runid,'reason':reason,'database':snapshot,'recovery':recovery['result']})
'''
(W/'runtime.py').write_text(module)
(W/'worker.py').write_text("from runtime import execute\nexecute()\n")
(W/'watchdog.py').write_text("from runtime import watch\nimport time\ntime.sleep(2)\nwatch()\n")
p=subprocess.Popen(['python3',str(W/'worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
w=subprocess.Popen(['python3',str(W/'watchdog.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
(W/'processes.json').write_text(json.dumps({'worker_pid':p.pid,'watchdog_pid':w.pid}))
print(json.dumps({'result':'AUTHORIZED_WINDOW_PREPARING','project':'knowhub-jit-fresh','automatic_sid_login':False,'independent_watchdog_started':True}))
