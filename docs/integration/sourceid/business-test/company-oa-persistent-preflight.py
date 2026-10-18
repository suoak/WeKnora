"""Read-only persistent test OA preflight. No authentication requests."""
import sys,os
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
sys.path.insert(0,str(R/'real-window'))
from runtime import jq,env,health,IMAGE,REV,baseline
APP='knowhub-company-test-app-1'
assert any(a.get('local')=='172.30.232.42' for n in json.loads(run(['ip','-j','-4','address','show'])) for a in n.get('addr_info',[]))
assert inspect(APP,'.Image')==IMAGE
assert inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REV
assert inspect(APP,'.State.Health.Status')=='healthy'
assert inspect('knowhub-company-test-frontend-1','.Image')=='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
schema=jq("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations",False)
assert schema=={'version':3030,'dirty':False}
counts=jq("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'vectors',(SELECT COUNT(*) FROM embeddings),'live_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()),'active_sessions',(SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()))",False)
e=env(APP)
keys=('OIDC_AUTH_ENABLE','SOURCEID_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_ACCOUNT_SCOPE','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_SESSION_MODE','SOURCEID_ADMISSION_MODE','SOURCEID_LOCAL_ACCESS_TTL','SOURCEID_LOCAL_ABSOLUTE_TTL','SOURCEID_LOCAL_IDLE_TTL','SOURCEID_ADMISSION_MAX_AGE','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_JIT_POLICY_CONFIRMED','SOURCEID_JIT_EMAIL_DOMAINS','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_BASIC_ENCODING','SOURCEID_LOGOUT_ENABLE','AUTO_MIGRATE','LOG_LEVEL','SOURCEID_DIAGNOSTICS_ENABLE')
assert e.get('OIDC_AUTH_REDIRECT_URI')=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
assert e.get('OIDC_AUTH_CLIENT_ID')=='knowhub' and e.get('OIDC_AUTH_CLIENT_SECRET')
assert e.get('SOURCEID_BASIC_ENCODING')=='oauth_form'
routes=health('company-test-verified-email-db3030')
labels=inspect(APP,'.Config.Labels')
print(json.dumps({'result':'PREFLIGHT_READONLY','revision':REV,'schema':schema,'counts':counts,'routes':routes,'flags':{k:e.get(k,'NOT_SET') for k in keys},'compose_files':labels['com.docker.compose.project.config_files'].split(','),'working_dir':labels['com.docker.compose.project.working_dir'],'callback_matches':True,'client_config_present':True,'fresh_network_detached':'knowhub-jit-fresh_isolated' not in inspect('knowhub-company-entry-entry-1','.NetworkSettings.Networks'),'real_sid_auth_calls':0}))
