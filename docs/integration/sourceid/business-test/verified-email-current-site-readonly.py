"""Final unchanged-site check. No authorize, credentials, app restart or writes."""
import urllib.request,urllib.error
PROJECT='knowhub-company-test';APP=PROJECT+'-app-1';PG=PROJECT+'-postgres-1'
def run(args):
 z=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if z.returncode:raise RuntimeError('controlled_site_readonly_check_failed')
 return z.stdout
def meta(name,field):return json.loads(run(['docker','inspect','--format','{{json '+field+'}}',name]))
flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED']
script="import os,json,sys;print(json.dumps({k:os.environ.get(k,'false') for k in json.loads(sys.argv[1])}))"
env=json.loads(run(['docker','exec',APP,'python3','-c',script,json.dumps(flags)]))
expected={'OIDC_AUTH_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_SESSION_MODE':'external','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_DIAGNOSTICS_ENABLE':'false','AUTO_MIGRATE':'false','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'false'}
assert env==expected
image=meta(APP,'.Image');assert image=='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
front=meta(PROJECT+'-frontend-1','.Image');assert front=='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
assert meta(APP,'.State.Health.Status')=='healthy'
sql="SELECT json_build_object('version',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations),'enterprise_sessions',(SELECT COUNT(*) FROM source_id_sessions),'identities',(SELECT COUNT(*) FROM external_identities),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'active_transactions',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()))"
db=json.loads(run(['docker','exec',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-c',sql]))
assert db['version']==3029 and db['dirty']==False and all(db[k]==0 for k in ('enterprise_sessions','identities','ownership','jit','active_transactions'))
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**kw):return None
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect());routes=[]
for port in (80,8080):
 for path in ('/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
  try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
  except urllib.error.HTTPError as e:r=e
  with r:
   assert r.status==(302 if path.endswith('/callback') else 200)
   assert r.headers.get('X-KnowHub-Environment')=='company-test-f40-db3029'
   routes.append({'port':port,'kind':'callback_without_parameters' if path.endswith('/callback') else 'health' if path=='/health' else 'oidc_config','status':r.status})
print(json.dumps({'result':'PASS','source_app_revision':'f40b1602f','source_frontend_revision':'832b6652a','app_image':image,'frontend_image':front,'database':db,'gate_values':env,'routes':routes,'real_sid_requests':0,'configuration_changed':False,'app_recreated':False,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}))
