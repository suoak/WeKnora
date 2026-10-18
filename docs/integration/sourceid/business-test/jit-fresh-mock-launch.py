"""Synthetic SID only, separate disposable databases; fresh real DB stays empty."""
import os
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
assert json.loads((R/'prepare-result.json').read_text())['result']=='PASS'
worker=r'''import os,pathlib,json,subprocess,secrets,time,ssl,base64,traceback
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');P=R/'private';F=P/'mock';F.mkdir(mode=0o700,exist_ok=True)
I='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b';PG='knowhub-jit-fresh-postgres-1'
PREFIX='knowhub-jit-fresh-mock-';owned=[];stage='prepare_fixture';out={'result':'RUNNING','real_sid_requests':0,'original_entry_changed':False}
def save():(R/'mock-result.json').write_text(json.dumps(out))
def cmd(args,data=None,timeout=60):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if p.returncode:raise RuntimeError('controlled_mock_operation_failed')
 return p.stdout
def sql(db,q):return cmd(['docker','exec','-i',PG,'psql','-X','-qAt','-U','fresh_owner','-d',db,'-v','ON_ERROR_STOP=1'],q.encode()).decode().strip()
def inspect(name,field):return json.loads(cmd(['docker','inspect','--format','{{json '+field+'}}',name]))
save()
try:
 source=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f/jit-capacity-mock.py');assert source.exists()
 (F/'provider.py').write_bytes(source.read_bytes())
 openssl="openssl genrsa -out /fixture/signing-key.pem 2048 >/dev/null 2>&1 && openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/tls-key.pem -out /fixture/ca.pem -days 2 -subj /CN=127.0.0.1 -addext subjectAltName=IP:127.0.0.1 >/dev/null 2>&1"
 cmd(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=bind,source='+str(F)+',target=/fixture','--entrypoint','sh',I,'-c',openssl])
 modulus=cmd(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=bind,source='+str(F)+',target=/fixture,readonly','--entrypoint','openssl',I,'rsa','-in','/fixture/signing-key.pem','-noout','-modulus']).decode().strip().split('=')[1]
 n=bytes.fromhex(modulus);(F/'jwks.json').write_text(json.dumps({'keys':[{'kty':'RSA','kid':'synthetic-key','use':'sig','alg':'RS256','n':base64.urlsafe_b64encode(n).decode().rstrip('='),'e':'AQAB'}]}))
 client=r"""import os,json,ssl,urllib.request,urllib.parse,urllib.error,http.cookiejar,secrets
ctx=ssl.create_default_context(cafile='/fixture/ca.pem')
class NR(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
jar=http.cookiejar.CookieJar();o=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(jar),urllib.request.HTTPSHandler(context=ctx),NR())
def req(path,method='GET',body=None,token=None,extra=None):
 h={'Host':'knowhub.ruijie.com.cn','Content-Type':'application/json'}
 if token:h['Authorization']='Bearer '+token
 if extra:h.update(extra)
 url=path if path.startswith('https://127.0.0.1:8443/') else 'http://127.0.0.1'+path
 try:r=o.open(urllib.request.Request(url,headers=h,method=method,data=json.dumps(body).encode() if body is not None else None),timeout=20)
 except urllib.error.HTTPError as e:r=e
 with r:
  data=r.read();v=json.loads(data) if data[:1] in (b'{',b'[') else {}
  return r.status,v,r.headers.get('Location','')
case=os.environ['SYNTHETIC_CASE'];prior=None
if case=='existing':
 status,b,_=req('/api/v1/auth/register','POST',{'username':'synthetic-existing','email':'capacity-jit-isolated@example.test','password':secrets.token_urlsafe(16)+'Aa9!'})
 if status!=201:
  text=json.dumps(b).lower();category=next((x for x,phrase in [('WORKSPACE','failed to create workspace'),('PASSWORD','password'),('INVALID_INPUT','invalid registration parameters'),('USER_CREATE','failed to create user'),('DISABLED','registration is disabled')] if phrase in text),'UNCLASSIFIED')
  raise RuntimeError('synthetic_register_status_'+str(status)+'_category_'+category)
 prior=b['user']
results=[];uid=None
for i in range(2):
 st,b,_=req('/api/v1/auth/oidc/url');assert st==200
 auth=b['authorization_url'];assert urllib.parse.urlsplit(auth).hostname=='127.0.0.1'
 assert 'prompt' not in urllib.parse.parse_qs(urllib.parse.urlsplit(auth).query)
 auth+='&synthetic_user=capacity-jit-isolated'
 st,_,loc=req(auth);assert st==302
 parsed=urllib.parse.urlsplit(loc);assert parsed.hostname=='knowhub.ruijie.com.cn'
 st,_,loc=req(parsed.path+'?'+parsed.query);assert st==302 and '#oidc_complete=1' in loc
 st,b,_=req('/api/v1/auth/oidc/result','POST',{},extra={'X-KnowHub-OIDC-Result':'1','Origin':'http://knowhub.ruijie.com.cn'})
 assert st==200 and b.get('success') and b.get('token') and 'refresh_token' not in b
 assert not any('refresh' in c.name.lower() for c in jar)
 u=b['user'];token=b['token']
 if uid is None:uid=u['id']
 assert u['id']==uid
 if case=='new':assert u['tenant_id']==0 and not u['is_system_admin'] and not u['can_access_all_tenants'] and b.get('is_new_user',False)==(i==0)
 else:
  for k in ('id','tenant_id','is_system_admin','can_access_all_tenants'):assert u[k]==prior[k]
 assert req('/api/v1/auth/me',token=token)[0]==200
 assert req('/login')[0]==200
 if case=='new':assert req('/api/v1/auth/switch-tenant','POST',{'tenant_id':10000},token)[0]==403
 assert req('/api/v1/auth/refresh','POST',{'refresh_token':token})[0] in (400,401,403)
 assert req('/api/v1/auth/logout','POST',{},token)[0]==200
 assert req('/api/v1/auth/me',token=token)[0]==401
 results.append({'login':True,'same_user_id':True,'auth_me':True,'no_refresh':True,'logout_old_access_rejected':True,'static_login_shell':True})
print(json.dumps({'result':'PASS','case':case,'iterations':results,'browser_portal_rendering':'NOT_EXECUTED','zero_privilege_new_user':case=='new','original_user_and_flags_preserved':case=='existing'}))
"""
 (F/'client.py').write_text(client)
 base={line.split('=',1)[0]:line.rstrip('\n').split('=',1)[1] for line in (P/'app.env').read_text().splitlines() if '=' in line}
 frontimage=inspect('knowhub-company-test-frontend-1','.Image');redisimage=inspect('knowhub-company-test-redis-1','.Image')
 with (F/'empty-fresh.dump').open('wb') as f:
  z=subprocess.run(['docker','exec',PG,'pg_dump','-U','fresh_owner','-d','knowhub_jit_fresh','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
 outcomes={}
 for case in ('new','existing'):
  stage='mock_'+case;out.update(stage=stage);save();db='jit_mock_'+case
  assert sql('postgres',"SELECT COUNT(*) FROM pg_database WHERE datname='"+db+"'")=='0'
  cmd(['docker','exec',PG,'createdb','-U','fresh_owner',db])
  with (F/'empty-fresh.dump').open('rb') as f:
   z=subprocess.run(['docker','exec','-i',PG,'pg_restore','-U','fresh_owner','-d',db,'--clean','--if-exists','--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
  if case=='existing':
   # Synthetic fixture only: use the normal registration API, enabling it
   # exclusively in this disposable DB, never the original/fresh-real DB.
   sql(db,"INSERT INTO system_settings(key,value,value_type,category) VALUES ('auth.registration_mode','\"self_serve\"','string','auth') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value")
   sql(db,"INSERT INTO system_settings(key,value,value_type,category) VALUES ('auth.default_tenant_mode','\"tenantless\"','string','auth') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value")
  env=dict(base);ref='synthetic-isolated-mock-only'
  env.update(DB_HOST='127.0.0.1',DB_NAME=db,REDIS_ADDR='127.0.0.1:6379',REDIS_PASSWORD='',JWT_SECRET=secrets.token_urlsafe(48),SYSTEM_AES_KEY=secrets.token_hex(16),SSL_CERT_FILE='/fixture/ca.pem',OIDC_AUTH_ENABLE='true',OIDC_AUTH_ISSUER_URL='https://127.0.0.1:8443',OIDC_AUTH_CLIENT_ID='knowhub',OIDC_AUTH_CLIENT_SECRET='synthetic-business-secret',OIDC_AUTH_AUTHORIZATION_ENDPOINT='https://127.0.0.1:8443/authorize',OIDC_AUTH_TOKEN_ENDPOINT='https://127.0.0.1:8443/token',OIDC_AUTH_USER_INFO_ENDPOINT='https://127.0.0.1:8443/profile',OIDC_AUTH_JWKS_URI='https://127.0.0.1:8443/jwks',OIDC_AUTH_REDIRECT_URI='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback',OIDC_AUTH_SCOPES='openid',OIDC_AUTH_PKCE_ENABLE='false',OIDC_AUTH_CLIENT_AUTH_METHOD='client_secret_basic',OIDC_AUTH_USER_INFO_TRANSPORT='bearer_get',SOURCEID_PROTOCOL_ONLY='false',SOURCEID_CONTRACT_CONFIRMED='true',SOURCEID_EMAIL_BINDING_ENABLE='true',SOURCEID_EMAIL_CONTRACT_CONFIRMED='true',SOURCEID_AUTO_BINDING_ENABLE='true',SOURCEID_AUTO_BINDING_POLICY_REF=ref,SOURCEID_ACCOUNT_LINK_MODE='verified_email_policy',SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED='true',SOURCEID_VERIFIED_EMAIL_POLICY_REF=ref,SOURCEID_CLAIMS_SOURCE='userinfo',SOURCEID_EMPLOYEE_CLAIM='attributes.RJGH',SOURCEID_SESSION_MODE='local_approved',SOURCEID_LOCAL_POLICY_REF=ref,SOURCEID_LOCAL_ACCESS_TTL='60m',SOURCEID_LOCAL_ABSOLUTE_TTL='60m',SOURCEID_LOCAL_IDLE_TTL='30m',SOURCEID_ADMISSION_MODE='oidc_sso_approved',SOURCEID_ADMISSION_POLICY_REF=ref,SOURCEID_ADMISSION_MAX_AGE='60m',SOURCEID_SSO_POLICY_CONFIRMED='true',SOURCEID_JIT_ENABLE='true',SOURCEID_JIT_POLICY_CONFIRMED='true',SOURCEID_JIT_POLICY_REF=ref,SOURCEID_JIT_EMAIL_DOMAINS='example.test',SYNTHETIC_CASE=case)
  # Only the isolated Mock container may call its loopback TLS provider.
  env['SSRF_WHITELIST']='127.0.0.1'
  ef=F/(case+'.env');ef.write_text(''.join(k+'='+v+'\n' for k,v in env.items()))
  names=[PREFIX+x for x in ('redis','provider','app','frontend')]
  for n in names:assert subprocess.run(['docker','inspect',n],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
  cmd(['docker','run','-d','--name',names[0],'--network','container:'+PG,'--log-driver','none',redisimage]);owned.append(names[0])
  cmd(['docker','run','-d','--name',names[1],'--network','container:'+PG,'--log-driver','none','--mount','type=bind,source='+str(F)+',target=/fixture,readonly','--entrypoint','python3',I,'/fixture/provider.py']);owned.append(names[1])
  cmd(['docker','run','-d','--name',names[2],'--network','container:'+PG,'--log-driver','none','--env-file',str(ef),'--tmpfs','/data/files:rw','--mount','type=bind,source='+str(F)+',target=/fixture,readonly',I]);owned.append(names[2])
  cmd(['docker','run','-d','--name',names[3],'--network','container:'+PG,'--log-driver','none','-e','APP_HOST=127.0.0.1','-e','APP_PORT=8080','-e','APP_SCHEME=http',frontimage]);owned.append(names[3])
  for _ in range(60):
   z=subprocess.run(['docker','exec',names[2],'python3','-c',"import urllib.request;urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:8080/health',timeout=1)"],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
   if z.returncode==0:break
   time.sleep(1)
  else:raise RuntimeError('mock_app_not_ready')
  z=subprocess.run(['docker','exec',names[2],'python3','/fixture/client.py'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
  if z.returncode:
   out['client_failure_lines']=__import__('re').findall(r'client.py", line ([0-9]+)',z.stderr.decode());out['register_status']=__import__('re').findall(r'synthetic_register_status_([0-9]+)',z.stderr.decode());out['register_category']=__import__('re').findall(r'_category_([A-Z_]+)',z.stderr.decode());raise RuntimeError('mock_api_assertion_failed')
  outcome=json.loads(z.stdout);assert outcome['result']=='PASS'
  counts=json.loads(sql(db,"SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'bindings',(SELECT COUNT(*) FROM external_identities),'active_sessions',(SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()),'active_access',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked AND expires_at>NOW()),'refresh',(SELECT COUNT(*) FROM auth_tokens WHERE token_type='refresh_token'),'memberships',(SELECT COUNT(*) FROM tenant_members),'privileged_users',(SELECT COUNT(*) FROM users WHERE is_system_admin OR can_access_all_tenants),'knowledge_bases',(SELECT COUNT(*) FROM knowledge_bases))"))
  assert counts['users']==counts['bindings']==1 and counts['jit']==(1 if case=='new' else 0) and counts['active_sessions']==counts['active_access']==counts['refresh']==0
  if case=='new':assert counts['memberships']==counts['privileged_users']==counts['knowledge_bases']==0
  outcome['database_counts']=counts;outcomes[case]=outcome;out['cases']=outcomes;save()
  for n in reversed(names):cmd(['docker','rm','-f',n]);owned.remove(n)
 assert sql('knowhub_jit_fresh','SELECT COUNT(*) FROM users')=='0' and sql('knowhub_jit_fresh','SELECT COUNT(*) FROM external_identities')=='0'
 out.update(result='PASS',stage='complete',cases=outcomes,fresh_real_database_still_empty=True,real_sid_requests=0,real_company_eligibility_verified=False)
except BaseException as e:out.update(result='FAIL',stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
finally:
 for n in reversed(owned):subprocess.run(['docker','rm','-f',n],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 out['utc']=__import__('datetime').datetime.now(__import__('datetime').timezone.utc).isoformat();save()
'''
(R/'mock-worker.py').write_text(worker)
subprocess.Popen(['python3',str(R/'mock-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'MOCK_STARTED','real_sid_requests':0,'fresh_real_database_written':False,'original_entry_changed':False}))
