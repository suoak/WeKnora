"""One authorized APP-only retry. No schema-changing command is present."""
import os
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
assert json.loads((B/'retry-state.json').read_text())['stage']=='BACKUP_PASS_APP_STOPPED'
assert not (B/'retry-deploy-result.json').exists(), 'single_attempt_only'
worker=r'''import os,pathlib,json,subprocess,datetime,time,traceback,sys,re,urllib.request,urllib.error
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');S=json.loads((B/'retry-state.json').read_text());D=pathlib.Path(S['backup_directory']);G=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green')
sys.path.insert(0,str(B/'retry-controls'))
from verified_email_deployment_contract import PATHS,validate,same_container_inventory
APP='knowhub-company-test-app-1';PG='knowhub-company-test-postgres-1';FRONT='knowhub-company-test-frontend-1';ENTRY='knowhub-company-entry-entry-1'
NEW=S['candidate_image'];OLD=S['expected_old_app'];SHA=S['source_sha']
out={'result':'RUNNING','stage':'pre_replace','schema_commands_executed':0,'real_sid_requests':0,'rollback_triggered':False,'deploy_attempts':0}
def save():(B/'retry-deploy-result.json').write_text(json.dumps(out))
def call(args,data=None,timeout=120):
 z=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_app_only_retry_failed')
 return z.stdout
def meta(name,field):return json.loads(call(['docker','inspect','--format','{{json '+field+'}}',name]))
def sql(q):return call(['docker','exec','-i',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-v','ON_ERROR_STOP=1'],('BEGIN READ ONLY;'+q+';ROLLBACK;').encode()).decode().strip()
def schema():return sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")
def compose(pin):return ['docker','compose','--project-name','knowhub-company-test','--project-directory',str(G),'-f',str(G/'compose.json'),'-f',str(B/pin)]
def ready():
 for _ in range(60):
  if meta(APP,'.State').get('Health',{}).get('Status')=='healthy':return
  time.sleep(1)
 raise RuntimeError('app_health_timeout')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**kw):return None
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
def routes(expected_label):
 rows=[]
 for port in (80,8080):
  for path in PATHS:
   try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
   except urllib.error.HTTPError as e:r=e
   with r:
    out['last_route_probe']={'port':port,'path':path,'status':r.status};save()
    validate(port,path,r.status,r.headers.get('Location'))
    assert r.headers.get('X-KnowHub-Environment')==expected_label
    if path=='/login' and port==80:
     assets=re.findall(r'(?:src|href)="(/assets/[^"?]+\.(?:js|css))"',r.read(1048576).decode());assert assets
     for asset in assets[:3]:
      with op.open(urllib.request.Request('http://127.0.0.1'+asset,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=15) as a:assert a.status==200
    rows.append({'port':port,'path':path,'status':r.status})
 return rows
def verify_data():
 before=json.loads((D/'protected-data-baseline.json').read_text())
 for table,expected in before.items():
  if table.endswith('_count'):actual=int(sql('SELECT COUNT(*) FROM '+table[:-6]))
  elif table=='nonempty_chunks':actual=int(sql("SELECT COUNT(*) FROM chunks WHERE content IS NOT NULL AND length(trim(content))>0 AND deleted_at IS NULL"))
  elif expected:actual=sorted(json.loads(sql("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT "+','.join(expected[0])+' FROM '+table+')x')),key=lambda x:json.dumps(x,sort_keys=True))
  else:actual=json.loads(sql("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT 1 FROM "+table+')x'))
  assert actual==expected
 current=call(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',OLD,'-c',(D/'file-baseline-code.txt').read_text()],timeout=90)
 assert json.loads(current)==json.loads((D/'protected-file-baseline.json').read_text())
 return True
save()
try:
 assert schema()=='3030|false' and meta(APP,'.Image')==OLD and not meta(APP,'.State.Running')
 assert json.loads((B/'retry-backup-result.json').read_text())['result']=='PASS'
 assert json.loads((B/'retry-controls/calibration-result.json').read_text())['result']=='PASS'
 assert json.loads(call(['docker','image','inspect',NEW]))[0]['Config']['Labels']['org.opencontainers.image.revision']==SHA
 out['stage']='app_only_recreate';out['deploy_attempts']=1;save()
 call(compose('app-pin.json')+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'])
 ready();assert meta(APP,'.Image')==NEW and schema()=='3030|false'
 oldenv=dict(v.split('=',1) for v in json.loads((D/'app-config.json').read_text())['Env'])
 newenv=dict(v.split('=',1) for v in meta(APP,'.Config.Env'))
 assert newenv==oldenv
 assert meta(APP,'.HostConfig.NetworkMode')==json.loads((D/'app-host-config.json').read_text())['NetworkMode']
 out['protected_runtime_configuration_unchanged']=True
 call(['docker','exec',FRONT,'nginx','-t']);call(['docker','exec',FRONT,'nginx','-s','reload'])
 out['stage']='verify_before_label_change';save()
 out['routes_before_label_update']=routes('company-test-f40-db3030')
 assert verify_data();out['original_data_permissions_and_file_contents_unchanged']=True
 before=json.loads((D/'containers-before.json').read_text());names=list(before)
 fmt='{"id":{{json .Id}},"image_id":{{json .Image}},"mounts":{{json .Mounts}},"ports":{{json .NetworkSettings.Ports}}}'
 now=dict(zip(names,[json.loads(v) for v in call(['docker','inspect','--format',fmt]+names).decode().splitlines()]))
 for name in names:
  if name!=APP:assert same_container_inventory(before[name],now[name])
 assert not meta(APP,'.HostConfig.PortBindings')
 out['other_containers_images_mounts_ports_unchanged']=True
 tooling='/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools'
 firewall=json.loads(call(['docker','run','--rm','--network','host','--cap-add','NET_ADMIN','--tmpfs','/nettools/run','--log-driver','none','--mount','type=bind,source='+tooling+',target=/nettools,readonly','--entrypoint','python3',OLD,'-c',(D/'firewall-read-code.txt').read_text()]))
 assert firewall==json.loads((D/'protected-firewall-before.json').read_text());out['firewall_rules_unchanged']=True
 counts=json.loads(sql("SELECT json_build_object('identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'policy_decisions',(SELECT COUNT(*) FROM source_id_link_decisions),'policy_sessions',(SELECT COUNT(*) FROM source_id_local_link_policies),'active_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()))"));assert all(v==0 for v in counts.values())
 flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_VERIFIED_EMAIL_POLICY_REF']
 gates={k:newenv.get(k) for k in flags};assert list(gates.values())==['false','true','false','false','false','false','external','false','false','oauth_form','false','false','trusted_ownership','false','']
 out.update(gates=gates,identity_counts=counts)
 # Only now, after immutable image/health/data/gates/routes passed, update label.
 out['stage']='update_verified_gateway_label';save()
 conf=G.parent/'entry/nginx.conf';text=conf.read_text();assert 'company-test-f40-db3030' in text
 conf.write_text(text.replace('company-test-f40-db3030','company-test-verified-email-db3030'))
 call(['docker','exec',ENTRY,'nginx','-t']);call(['docker','exec',ENTRY,'nginx','-s','reload']);time.sleep(2)
 out['routes_after_label_update']=routes('company-test-verified-email-db3030')
 assert schema()=='3030|false'
 out.update(result='PASS',stage='completed',app_revision=SHA,app_image=NEW,frontend_revision='832b6652adf541892545a7eac8b68733f846ab95',frontend_image=now[FRONT]['image_id'],db_schema=3030,dirty=False,verified_email_deployed_but_disabled=True,users=3,members=5,active_kbs=1,documents=3,nonempty_chunks=2717,embeddings=2717,backup_directory=str(D),gateway_label_changed_after_verification=True,refresh=False,actual_rollback_this_attempt=False)
 S['stage']='APP_ONLY_RETRY_PASS';(B/'retry-state.json').write_text(json.dumps(S))
except Exception as e:
 out.update(result='FAIL',failure_class=type(e).__name__,failure_lines=[v.lineno for v in traceback.extract_tb(e.__traceback__) if v.filename==__file__]);save()
 try:
  assert schema()=='3030|false'
  out['rollback_triggered']=True;save()
  call(compose('app-rollback-pin.json')+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app']);ready();assert meta(APP,'.Image')==OLD
  (G.parent/'entry/nginx.conf').write_bytes((D/'entry-nginx.conf').read_bytes());call(['docker','exec',ENTRY,'nginx','-t']);call(['docker','exec',ENTRY,'nginx','-s','reload']);call(['docker','exec',FRONT,'nginx','-s','reload']);time.sleep(2)
  routes('company-test-f40-db3030');assert verify_data();out['rollback_status']='OLD_APP_RESTORED_DB3030_RETAINED_VERIFIED'
 except Exception:out['rollback_status']='RECOVERY_REQUIRES_ATTENTION'
 S['stage']='STOPPED_AFTER_ONE_ATTEMPT';(B/'retry-state.json').write_text(json.dumps(S))
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
'''
(B/'retry-app-only-worker.py').write_text(worker)
subprocess.Popen(['python3',str(B/'retry-app-only-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','authorized_app_replacements':1,'schema_commands_executed':0}))
