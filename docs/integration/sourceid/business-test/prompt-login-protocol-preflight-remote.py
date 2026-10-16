"""Read-only preflight; no authorization URL, secret, code or token access."""
import yaml,stat
live=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
receipt=json.loads((root/'app-deployment-result.json').read_text());assert receipt['result']=='PASS'
ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project=knowhub-sourceid-business','--format','{{.ID}}']).decode().split()
current={x['service']:x for x in (metadata(cid) for cid in ids)}
app=current['app'];front=current['frontend'];pg=current['postgres']
assert app['id']==receipt['containers_after']['app']['id'] and app['health']=='healthy'
assert app['image_id']=='sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c'
revision=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',app['image_id']]).decode().strip()
assert revision=='479416c9086d91de4484dbbd2cc5f912cdcdb697'
assert front['id']==receipt['containers_before']['frontend']['id']
flags={}
for key in receipt['flags_after']:
 v=run(['docker','exec',app['id'],'sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode();assert v in ('true','false','NOT_SET','oauth_form','external');flags[key]=v
assert flags==receipt['flags_after'] and flags['SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']=='false'
q="""BEGIN READ ONLY;
SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;
SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'members',(SELECT COUNT(*) FROM tenant_members),'bindings',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions));
SELECT json_build_object('total',COUNT(*),'consumed',COUNT(*) FILTER (WHERE consumed),'unconsumed_expired',COUNT(*) FILTER (WHERE NOT consumed AND expires_at<=NOW()),'unconsumed_live',COUNT(*) FILTER (WHERE NOT consumed AND expires_at>NOW()),'authorization_live',COUNT(*) FILTER (WHERE NOT consumed AND expires_at>NOW() AND nonce<>''),'result_live',COUNT(*) FILTER (WHERE NOT consumed AND expires_at>NOW() AND COALESCE(nonce,'')='')) FROM oidc_transactions;
ROLLBACK;"""
p=subprocess.run(['docker','exec','-i',pg['id'],'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert p.returncode==0
db=[json.loads(x) for x in p.stdout.decode().splitlines() if x];assert db[0]=={'version':3027,'dirty':False}
assert db[1]=={'users':2,'members':2,'bindings':0,'reviews':0,'sessions':0,'local_sessions':0}
files=receipt['compose_files_in_order'];assert app['compose_files'].split(',')==files
cmd=['docker','compose','--project-name','knowhub-sourceid-business','--project-directory',str(live)]
for f in files:cmd+=['-f',f]
resolved=json.loads(run(cmd+['config','--format','json']))
# Resolve hypothetical last-layer override over stdin; config only, never up.
override={'services':{'app':{'image':app['image_id'],'environment':{'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'true'}}}}
p=subprocess.run(cmd+['-f','-','config','--format','json'],input=yaml.safe_dump(override).encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30);assert p.returncode==0
trial=json.loads(p.stdout)
before_env=resolved['services']['app']['environment'];after_env=trial['services']['app']['environment']
changed=[k for k in set(before_env)|set(after_env) if before_env.get(k)!=after_env.get(k)];assert changed==['SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']
assert after_env['SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']=='true'
assert trial['services']['app']['image']==app['image_id']
assert all(trial['services'][k]==v for k,v in resolved['services'].items() if k!='app')
assert {k:v for k,v in trial['services']['app'].items() if k!='environment'}=={k:v for k,v in resolved['services']['app'].items() if k!='environment'}
backup=pathlib.Path(receipt['backup_directory']);dump=backup/'current-db-3027.dump'
assert dump.is_file() and stat.S_IMODE(dump.stat().st_mode)==0o600
rollback=root/'prompt-login-app-rollback-pin.yml';assert rollback.is_file()
rollback_image=yaml.safe_load(rollback.read_text())['services']['app']['image']
assert rollback_image=='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
assert json.loads(run(['docker','image','inspect','--format','{{json .Id}}',rollback_image]))==rollback_image
public_keys=('OIDC_AUTH_CLIENT_ID','OIDC_AUTH_ISSUER_URL','OIDC_AUTH_DISCOVERY_URL','OIDC_AUTH_AUTHORIZATION_ENDPOINT','OIDC_AUTH_TOKEN_ENDPOINT','OIDC_AUTH_JWKS_URI','OIDC_AUTH_USER_INFO_ENDPOINT','OIDC_AUTH_REDIRECT_URI','OIDC_AUTH_SCOPES','OIDC_AUTH_CLIENT_AUTH_METHOD','OIDC_AUTH_USER_INFO_TRANSPORT')
settings={}
for k in public_keys:
 settings[k]=run(['docker','exec',app['id'],'sh','-c','printf "%s" "${'+k+'-}"']).decode()
assert settings['OIDC_AUTH_CLIENT_ID']=='knowhub'
assert settings['OIDC_AUTH_REDIRECT_URI']=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
assert settings['OIDC_AUTH_CLIENT_AUTH_METHOD']=='client_secret_basic' and settings['OIDC_AUTH_USER_INFO_TRANSPORT']=='bearer_get' and settings['OIDC_AUTH_SCOPES']=='openid'
public_probe=r'''
import ssl,urllib.request,json
o=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile='/etc/ssl/certs/ca-certificates.crt')))
with o.open('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration',timeout=15) as r:m=json.load(r);discovery=r.status
expected={'issuer':'https://sid.ruijie.com.cn','authorization_endpoint':'https://sid.ruijie.com.cn/oidc/authorize','token_endpoint':'https://sid.ruijie.com.cn/oidc/accessToken','jwks_uri':'https://sid.ruijie.com.cn/oidc/jwks','userinfo_endpoint':'https://sid.ruijie.com.cn/oidc/profile'}
assert all(m.get(k)==v for k,v in expected.items())
with o.open(expected['jwks_uri'],timeout=15) as r:j=json.load(r);status=r.status
keys=[{'kty':k.get('kty'),'alg':k.get('alg'),'kid_present':bool(k.get('kid')),'use':k.get('use')} for k in j['keys']]
assert any(k['kty']=='RSA' for k in keys)
print(json.dumps({'discovery_status':discovery,'jwks_status':status,'endpoints':expected,'jwks_key_structure':keys,'tls_verified_system_ca':True,'proxy_bypassed_for_public_probe':True,'auth_time_advertised':'auth_time' in m.get('claims_supported',[]),'auth_time_actual_return':'NOT_OBSERVED','rs256_advertised':'RS256' in m.get('id_token_signing_alg_values_supported',[])}))
'''
p=subprocess.run(['docker','exec','-i',app['id'],'python3','-'],input=public_probe.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=40);assert p.returncode==0
public=json.loads(p.stdout)
mapping={'OIDC_AUTH_ISSUER_URL':'issuer','OIDC_AUTH_AUTHORIZATION_ENDPOINT':'authorization_endpoint','OIDC_AUTH_TOKEN_ENDPOINT':'token_endpoint','OIDC_AUTH_JWKS_URI':'jwks_uri','OIDC_AUTH_USER_INFO_ENDPOINT':'userinfo_endpoint'}
config_match={key:(not settings[key] or settings[key]==public['endpoints'][name]) for key,name in mapping.items()};assert all(config_match.values())
for key in public_keys:
 v=settings[key]
 if key.endswith(('URL','ENDPOINT','URI')) and v:assert urllib.parse.urlsplit(v).query=='' and urllib.parse.urlsplit(v).username is None
checks={}
o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for name,url in [('frontend','http://127.0.0.1/'),('app_health','http://127.0.0.1:8080/health'),('oidc_config','http://127.0.0.1/api/v1/auth/oidc/config')]:
 with o.open(url,timeout=5) as r:
  checks[name]=r.status;assert r.status==200
print(json.dumps({'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'revision':revision,'app_id':app['id'],'app_image':app['image_id'],'frontend_id':front['id'],'frontend_revision':'1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b','flags':flags,'database':db[:2],'authorization_transactions':db[2],'public_settings':settings,'official_public_metadata':public,'configured_endpoints_match':config_match,'compose_files_in_order':files,'hypothetical_last_override':{'validated_config_only':True,'only_environment_change':changed,'same_image':True,'other_services_unchanged':True,'applied':False},'rollback':{'pin':str(rollback),'old_image_present':True,'backup':str(backup),'database_backup_bytes':dump.stat().st_size},'health':checks,'secret_read':False,'login_started':False,'runtime_configuration_changed':False}))
