"""Approved APP-only update and standard migrate up 1, fail closed on dirty."""
import os
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
assert json.loads((B/'deployment-state.json').read_text())['stage']=='BACKUP_PASS_APP_STOPPED'
worker=r'''import os,pathlib,json,subprocess,datetime,time,urllib.parse,urllib.request,urllib.error,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');S=json.loads((B/'deployment-state.json').read_text());D=pathlib.Path(S['backup_directory']);G=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green')
APP='knowhub-company-test-app-1';PG='knowhub-company-test-postgres-1';FRONT='knowhub-company-test-frontend-1';ENTRY='knowhub-company-entry-entry-1'
NEW=S['candidate_image'];OLD=S['expected_old_app'];SHA=S['source_sha']
out={'result':'RUNNING','stage':'pre_migration','real_sid_requests':0,'rollback_triggered':False,'migration_executed':False}
def save():(B/'deployment-result.json').write_text(json.dumps(out))
def call(args,data=None,timeout=120):
 z=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_upgrade_operation_failed')
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
save()
try:
 assert schema()=='3029|false' and meta(APP,'.Image')==OLD and not meta(APP,'.State.Running')
 assert json.loads((B/'deployment-backup-result.json').read_text())['result']=='PASS'
 assert json.loads(call(['docker','image','inspect',NEW]))[0]['Config']['Labels']['org.opencontainers.image.revision']==SHA
 protected=json.loads((D/'app-config.json').read_text())
 db={k:v for item in protected['Env'] for k,_,v in [item.partition('=')] if k in ('DB_HOST','DB_PORT','DB_USER','DB_PASSWORD','DB_NAME')}
 assert set(db)=={'DB_HOST','DB_PORT','DB_USER','DB_PASSWORD','DB_NAME'} and all('\n' not in v and '\r' not in v for v in db.values())
 (D/'migration-db.env').write_text('\n'.join(k+'='+v for k,v in db.items())+'\n');(D/'migration-db.env').chmod(0o600);db=None;protected=None
 code="import os,urllib.parse; e=os.environ; u='postgres://'+urllib.parse.quote(e['DB_USER'],safe='')+':'+urllib.parse.quote(e['DB_PASSWORD'],safe='')+'@'+e['DB_HOST']+':'+e['DB_PORT']+'/'+e['DB_NAME']+'?sslmode=disable';os.execvp('migrate',['migrate','-path','/migration','-database',u,'up','1'])"
 out['stage']='standard_003030_up';save()
 call(['docker','run','--rm','--network','container:knowhub-company-test-sid-network-1','--log-driver','none','--env-file',str(D/'migration-db.env'),'--mount','type=bind,source='+str(B/'source/migrations/versioned')+',target=/migration,readonly','--entrypoint','python3',NEW,'-c',code],timeout=120)
 out['migration_executed']=True;assert schema()=='3030|false'
 out['stage']='app_only_recreate';save()
 call(compose('app-pin.json')+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'])
 ready();assert meta(APP,'.Image')==NEW
 assert json.loads(call(['docker','image','inspect',NEW]))[0]['Config']['Labels']['org.opencontainers.image.revision']==SHA
 assert meta(FRONT,'.Id')==json.loads((D/'containers-before.json').read_text())[FRONT]['id']
 call(['docker','exec',FRONT,'nginx','-t']);call(['docker','exec',FRONT,'nginx','-s','reload'])
 out['stage']='entry_route_verify';save()
 config=G.parent/'entry/nginx.conf';before=config.read_text();assert 'company-test-f40-db3029' in before
 config.write_text(before.replace('company-test-f40-db3029','company-test-verified-email-db3030'))
 call(['docker','exec',ENTRY,'nginx','-t']);call(['docker','exec',ENTRY,'nginx','-s','reload']);time.sleep(2)
 class NoRedirect(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a,**kw):return None
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect());routes=[]
 for port in (80,8080):
  for path in (('/login',) if port==80 else ())+('/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
   try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
   except urllib.error.HTTPError as e:r=e
   with r:
    out['last_route_probe']={'port':port,'path':path,'status':r.status};save()
    assert r.status==(302 if path.endswith('/callback') else 200)
    assert r.headers.get('X-KnowHub-Environment')=='company-test-verified-email-db3030'
    routes.append({'port':port,'kind':'callback_without_parameters' if path.endswith('/callback') else path,'status':r.status})
 flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_VERIFIED_EMAIL_POLICY_REF']
 env=json.loads(call(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(flags)]))
 assert env==dict(zip(flags,['false','true','false','false','false','false','external','false','false','oauth_form','false','false','trusted_ownership','false','']))
 out.update(result='PASS',stage='deployed_gates_closed',app_revision=SHA,app_image=NEW,frontend_revision='832b6652adf541892545a7eac8b68733f846ab95',db_schema=3030,dirty=False,gates=env,routes=routes,frontend_container_unchanged=True,app_only=True,rollback_pin_ready=True,actual_rollback_executed=False,backup_directory=str(D))
 S['stage']='UPGRADE_PASS';(B/'deployment-state.json').write_text(json.dumps(S))
except Exception as e:
 out.update(result='FAIL',failure_class=type(e).__name__,failure_lines=[v.lineno for v in traceback.extract_tb(e.__traceback__) if v.filename==__file__])
 try:
  out['database_state_after_failure']=schema()
  if out['database_state_after_failure'] not in ('3029|false','3030|false'):out['rollback_status']='STOPPED_DIRTY_REQUIRES_RECOVERY'
  else:
   out['rollback_triggered']=True
   call(compose('app-rollback-pin.json')+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app']);ready();assert meta(APP,'.Image')==OLD
   (G.parent/'entry/nginx.conf').write_bytes((D/'entry-nginx.conf').read_bytes());call(['docker','exec',ENTRY,'nginx','-t']);call(['docker','exec',ENTRY,'nginx','-s','reload']);call(['docker','exec',FRONT,'nginx','-s','reload'])
   out['rollback_status']='OLD_APP_RESTORED_DATABASE_RETAINED';S['stage']='ROLLED_BACK_DATABASE_RETAINED';(B/'deployment-state.json').write_text(json.dumps(S))
 except Exception:out['rollback_status']='RECOVERY_FAILED_REQUIRES_ATTENTION'
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
'''
(B/'deployment-upgrade-worker.py').write_text(worker)
subprocess.Popen(['python3',str(B/'deployment-upgrade-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','action':'approved_003030_up_and_app_only_upgrade'}))
