"""Enable synthetic-only OIDC in isolated clone; never changes running project."""
import importlib.util,uuid,time,hashlib
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
private=m.BASE/'private';fixture=private/'mock';app='oa-legacy79-app-20261016'
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
admin=json.loads((private/'original-admin-baseline.json').read_text())
kb=m.scalar("SELECT row_to_json(s) FROM (SELECT b.id,b.tenant_id,b.embedding_model_id FROM knowledge_bases b WHERE b.deleted_at IS NULL AND EXISTS (SELECT 1 FROM knowledges k WHERE k.knowledge_base_id=b.id AND k.deleted_at IS NULL) LIMIT 1) s")
operator=m.sql("SELECT user_id FROM tenant_members WHERE tenant_id="+str(kb['tenant_id'])+" AND role='owner' AND status='active' AND deleted_at IS NULL LIMIT 1")
assert operator
cli=pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016/artifacts/sourceid-test-account')
assert hashlib.sha256(cli.read_bytes()).hexdigest()=='cb7fd6d852eb76b179cef93346367da0401dfb9eb3e75a464566b0d717194500'
op='b1046483-2d4e-427b-bf8f-a1b1b1c03b18'
args=['docker','run','--rm','-i','--network','container:'+m.PG,'--log-driver','none','--mount','type=bind,source='+str(cli)+',target=/fixture-cli,readonly','--entrypoint','/fixture-cli',m.IMAGE,'--test-host','172.30.232.42','--tenant',str(kb['tenant_id']),'--operator',operator,'--operation',op,'--approval','synthetic-clone-fixture-only','--role','viewer','--protected-json-stdin']
payload=json.dumps({'email':'isolated-viewer@example.test','database_dsn':'postgres://clone:synthetic-clone-only@127.0.0.1:5432/clone?sslmode=disable'}).encode()
m.execute(args,input=payload);m.execute(args+['--apply'],input=payload)
viewer=m.sql("SELECT id FROM users WHERE email='isolated-viewer@example.test' AND is_active")
docs=json.loads(m.sql("SELECT json_agg(id) FROM knowledges WHERE knowledge_base_id='"+kb['id']+"' AND deleted_at IS NULL"))
baseline={'admin_user_id':admin['id'],'admin_tenant_id':admin['tenant_id'],'knowledge_base_id':kb['id'],'knowledge_tenant_id':kb['tenant_id'],'embedding_model_id':kb['embedding_model_id'],'document_ids':docs,'viewer_user_id':viewer}
(fixture/'acceptance-baseline.json').write_text(json.dumps(baseline))
paths=json.loads(m.sql("SELECT json_agg(COALESCE(r.physical_path,k.file_path)) FROM knowledges k LEFT JOIN resources r ON k.file_path='resource://'||r.handle AND r.deleted_at IS NULL WHERE k.deleted_at IS NULL"))
readable=0;root=(m.BASE/'files').resolve()
for raw in paths:
    if raw.startswith('storage://') and '/local://' in raw:raw=raw[raw.index('/local://')+1:]
    if raw.startswith('local://'):rel=raw[8:]
    elif raw.startswith('/data/files/'):rel=raw[len('/data/files/'):]
    else:rel=raw
    p=(root/rel).resolve()
    try:p.relative_to(root)
    except ValueError:continue
    if p.is_file():
        with p.open('rb') as f:
            if f.read(4096):readable+=1
assert readable==len(paths)==3
m.execute(['docker','run','-d','--name','oa-legacy79-mock-20261016','--network','container:'+m.PG,'--log-driver','none','--mount','type=bind,source='+str(fixture)+',target=/fixture,readonly','--entrypoint','python3',m.IMAGE,'/fixture/legacy-clone-mock-sid.py'])
env=dict(line.split('=',1) for line in (private/'app-disabled.env').read_text().splitlines())
issuer='https://127.0.0.1:18443'
env.update({'OIDC_AUTH_ENABLE':'true','OIDC_AUTH_CLIENT_ID':'knowhub','OIDC_AUTH_CLIENT_SECRET':'synthetic-only','OIDC_AUTH_ISSUER_URL':issuer,'OIDC_AUTH_DISCOVERY_URL':issuer+'/.well-known/openid-configuration','OIDC_AUTH_AUTHORIZATION_ENDPOINT':issuer+'/authorize','OIDC_AUTH_TOKEN_ENDPOINT':issuer+'/token','OIDC_AUTH_USER_INFO_ENDPOINT':issuer+'/userinfo','OIDC_AUTH_JWKS_URI':issuer+'/jwks','OIDC_AUTH_CLIENT_AUTH_METHOD':'client_secret_basic','OIDC_AUTH_USER_INFO_TRANSPORT':'bearer_get','OIDC_AUTH_REDIRECT_URI':'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback','OIDC_AUTH_SCOPES':'openid','OIDC_AUTH_PKCE_ENABLE':'false','SSL_CERT_FILE':'/fixture/ca.pem','SOURCEID_PROTOCOL_ONLY':'false','SOURCEID_CONTRACT_CONFIRMED':'true','SOURCEID_EMAIL_BINDING_ENABLE':'true','SOURCEID_EMAIL_CONTRACT_CONFIRMED':'true','SOURCEID_AUTO_BINDING_ENABLE':'true','SOURCEID_AUTO_BINDING_POLICY_REF':'synthetic-all-roles-fixture-only','SOURCEID_SESSION_MODE':'local_approved','SOURCEID_EMPLOYEE_CLAIM':'attributes.RJGH','SOURCEID_CLAIMS_SOURCE':'userinfo','SOURCEID_LOCAL_POLICY_REF':'synthetic-local-policy-only','SOURCEID_ADMISSION_POLICY_REF':'synthetic-admission-only','SOURCEID_ADMISSION_MODE':'authorization','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED':'true','SOURCEID_LOCAL_ACCESS_TTL':'2m','SOURCEID_LOCAL_ABSOLUTE_TTL':'2m','SOURCEID_LOCAL_IDLE_TTL':'2m','SOURCEID_ADMISSION_MAX_AGE':'2m','SOURCEID_LOCAL_REFRESH_ENABLE':'false'})
(private/'app-mock.env').write_text('\n'.join(k+'='+v for k,v in env.items())+'\n')
old_id=inspect(app,'.Id');assert inspect(old_id,'.HostConfig.NetworkMode')=='container:'+inspect(m.PG,'.Id') or inspect(old_id,'.HostConfig.NetworkMode')=='container:'+m.PG
m.execute(['docker','rm','-f',old_id])
image='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
m.execute(['docker','run','-d','--name',app,'--network','container:'+m.PG,'--log-driver','none','--cpus','2','--memory','4g','--env-file',str(private/'app-mock.env'),'--mount','type=bind,source='+str(m.BASE/'files')+',target=/data/files,readonly','--mount','type=bind,source='+str(fixture)+',target=/fixture,readonly','--tmpfs','/tmp:rw,size=268435456','--entrypoint','/app/WeKnora',image])
probe="import urllib.request;assert urllib.request.urlopen('http://127.0.0.1:8080/health',timeout=2).status==200"
for _ in range(40):
    p=subprocess.run(['docker','exec',app,'python3','-c',probe],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    if p.returncode==0:break
    time.sleep(1)
else:raise RuntimeError('mock_app_health_timeout')
m.execute(['docker','run','-d','--name','oa-legacy79-frontend-20261016','--network','container:'+m.PG,'--log-driver','none','--memory','256m','-e','APP_HOST=127.0.0.1','-e','APP_PORT=8080','sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'])
out={'result':'PASS','synthetic_viewer_created_in_clone_only':True,'original_documents_readable_local_scheme':readable,'synthetic_oidc_only':True,'local_policy_is_synthetic_not_real_approval':True,'live_configuration_changed':False,'network':'none-loopback-only'}
(m.BASE/'mock-setup-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
