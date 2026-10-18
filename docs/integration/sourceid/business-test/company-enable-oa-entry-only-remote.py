"""User-authorized OIDC entry only. Business gates stay closed, no SID login."""
import os
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
G=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green')
APP='knowhub-company-test-app-1';PG='knowhub-company-test-postgres-1'
image='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
assert inspect(APP,'.Image')==image and inspect(APP,'.State.Health.Status')=='healthy'
assert inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files']==','.join([str(G/'compose.json'),str(B/'app-pin.json')])
q="SELECT json_build_object('schema',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations),'active_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()))"
db=json.loads(run(['docker','exec',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-c',q]));assert db=={'schema':3030,'dirty':False,'active_authorizations':0}
record=json.loads((B/'reliable-deploy-result.json').read_text());old=dict(x.split('=',1) for x in inspect(APP,'.Config.Env'));assert all(old.get(k)==v for k,v in record['gates'].items())
P=B/'private'/('oa-entry-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'));P.mkdir(mode=0o700)
(P/'app-config-before.json').write_text(json.dumps(inspect(APP,'.Config')))
(P/'approved-entry-only.json').write_text(json.dumps({'services':{'app':{'environment':{'OIDC_AUTH_ENABLE':'true'}}}}))
containers={n:{'id':inspect(n,'.Id'),'image':inspect(n,'.Image')} for n in ('knowhub-company-test-frontend-1',PG,'knowhub-company-test-redis-1','knowhub-company-test-docreader-1','knowhub-company-test-sid-network-1','knowhub-company-entry-entry-1')}
(P/'other-services-before.json').write_text(json.dumps(containers))
command=['docker','compose','--project-name','knowhub-company-test','--project-directory',str(G),'-f',str(G/'compose.json'),'-f',str(B/'app-pin.json')]
(P/'compose-order.json').write_text(json.dumps(command+['-f',str(P/'approved-entry-only.json')]))
worker=r'''import os,json,pathlib,subprocess,time,datetime,urllib.request,traceback
os.umask(0o077)
P=pathlib.Path(PRIVATE);B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');APP='knowhub-company-test-app-1'
out={'result':'RUNNING','stage':'app_only_config_reload','business_gates_opened':False,'real_sid_login_started_by_agent':False}
def save():(P/'result.json').write_text(json.dumps(out))
def call(args):
 z=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
 if z.returncode:raise RuntimeError('controlled_entry_config_failed')
 return z.stdout
def meta(name,field):return json.loads(call(['docker','inspect','--format','{{json '+field+'}}',name]))
cmd=json.loads((P/'compose-order.json').read_text());base=cmd[:-2]
def ready():
 for _ in range(60):
  if meta(APP,'.State').get('Health',{}).get('Status')=='healthy':return
  time.sleep(1)
 raise RuntimeError('health_timeout')
save()
try:
 call(cmd+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app']);ready()
 old=dict(x.split('=',1) for x in json.loads((P/'app-config-before.json').read_text())['Env']);new=dict(x.split('=',1) for x in meta(APP,'.Config.Env'));old['OIDC_AUTH_ENABLE']='true';assert old==new
 assert meta(APP,'.Image')=='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
 for name,v in json.loads((P/'other-services-before.json').read_text()).items():assert v=={'id':meta(name,'.Id'),'image':meta(name,'.Image')}
 call(['docker','exec','knowhub-company-test-frontend-1','nginx','-s','reload'])
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}));checks=[]
 for port in (80,8080):
  with op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+'/api/v1/auth/oidc/config',headers={'Host':'knowhub.ruijie.com.cn'}),timeout=15) as r:
   v=json.load(r);assert r.status==200 and v.get('success') is True and v.get('enabled') is True and v.get('sourceid_basic_encoding')=='oauth_form';assert r.headers.get('X-KnowHub-Environment')=='company-test-verified-email-db3030';checks.append({'port':port,'oidc_enabled':True})
 with op.open(urllib.request.Request('http://127.0.0.1/login',headers={'Host':'knowhub.ruijie.com.cn'}),timeout=15) as r:assert r.status==200
 gates=json.loads((B/'reliable-deploy-result.json').read_text())['gates'];gates['OIDC_AUTH_ENABLE']='true'
 assert all(new.get(k)==v for k,v in gates.items())
 out.update(result='PASS',stage='entry_visible_protocol_only',app_revision='5f988be51779f1a22bd9d027b7503fe6a8749f62',configuration_changes=['OIDC_AUTH_ENABLE=false->true'],gates=gates,config_api_checks=checks,other_containers_unchanged=True,protected_directory=str(P),policy_approval_still_required_for_business_session=True)
except Exception as e:
 out.update(result='FAIL',failure_class=type(e).__name__)
 try:call(base+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app']);ready();call(['docker','exec','knowhub-company-test-frontend-1','nginx','-s','reload']);out['rollback']='OIDC_FALSE_RESTORED_SAME_IMAGE'
 except Exception:out['rollback']='RECOVERY_REQUIRED'
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
'''.replace('PRIVATE',repr(str(P)))
(P/'worker.py').write_text(worker)
(B/'oa-entry-operation.json').write_text(json.dumps({'private_directory':str(P)}))
subprocess.Popen(['python3',str(P/'worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','scope':'OIDC_ENTRY_ONLY','business_gates_unchanged':True}))
