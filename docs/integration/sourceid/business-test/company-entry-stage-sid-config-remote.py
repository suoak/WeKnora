"""Stage existing approved SID config in green only; OIDC remains disabled."""
import os,shutil,time,hmac,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');G=B/'green';app='knowhub-company-test-app-1';old='knowhub-sourceid-business-app-1';out={'result':'FAIL','new_sid_requests':0,'business_gates_enabled':False};changed=False
cmd=['docker','compose','-p','knowhub-company-test','--project-directory',str(G),'-f',str(G/'compose.json')]
keys=('OIDC_AUTH_ISSUER_URL','OIDC_AUTH_DISCOVERY_URL','OIDC_AUTH_PROVIDER_DISPLAY_NAME','OIDC_AUTH_CLIENT_ID','OIDC_AUTH_CLIENT_SECRET','OIDC_AUTH_AUTHORIZATION_ENDPOINT','OIDC_AUTH_TOKEN_ENDPOINT','OIDC_AUTH_USER_INFO_ENDPOINT','OIDC_AUTH_JWKS_URI','OIDC_AUTH_SCOPES','OIDC_AUTH_REDIRECT_URI','OIDC_AUTH_CLIENT_AUTH_METHOD','OIDC_AUTH_USER_INFO_TRANSPORT','OIDC_AUTH_PKCE_ENABLE','SOURCEID_ENABLE')
backup=B/'private/green-before-real-sid.env'
try:
 env=environment(app);source=environment(old)
 assert source.get('OIDC_AUTH_CLIENT_ID')=='knowhub' and source.get('OIDC_AUTH_CLIENT_SECRET')
 assert source.get('OIDC_AUTH_ISSUER_URL')=='https://sid.ruijie.com.cn'
 assert source.get('OIDC_AUTH_REDIRECT_URI')=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
 assert env.get('OIDC_AUTH_ENABLE')=='false' and env.get('SOURCEID_PROTOCOL_ONLY')=='true' and env.get('SOURCEID_AUTO_BINDING_ENABLE')=='false'
 assert not backup.exists();shutil.copyfile(G/'private/app.env',backup)
 for k in keys:
  if k in source:env[k]=source[k]
 for k in ('OIDC_AUTH_ISSUER_URL','OIDC_AUTH_DISCOVERY_URL','OIDC_AUTH_AUTHORIZATION_ENDPOINT','OIDC_AUTH_TOKEN_ENDPOINT','OIDC_AUTH_USER_INFO_ENDPOINT','OIDC_AUTH_JWKS_URI'):
  if env.get(k):
   u=urllib.parse.urlsplit(env[k]);assert u.scheme=='https' and u.hostname=='sid.ruijie.com.cn' and not u.username and not u.password and not u.query
 assert all('\n' not in v and '\r' not in v for v in env.values())
 (G/'private/app.env').write_text('\n'.join(k+'='+v for k,v in env.items())+'\n');changed=True
 run(cmd+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=90)
 for _ in range(75):
  if inspect(app,'.State.Health.Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('green_health_after_staging_failed')
 run(['docker','exec','knowhub-company-test-frontend-1','nginx','-s','reload'])
 loaded=environment(app)
 assert all(loaded.get(k)==source.get(k) for k in keys if k in source)
 for k,v in {'OIDC_AUTH_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_SESSION_MODE':'external','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_BASIC_ENCODING':'oauth_form'}.items():assert loaded.get(k)==v
 out.update(result='PASS',client_id_matches_default=True,secret_matches_default_boolean_only=hmac.compare_digest(loaded['OIDC_AUTH_CLIENT_SECRET'],source['OIDC_AUTH_CLIENT_SECRET']),approved_callback_preserved=True,approved_protocol_config_preserved=True,oidc_remains_disabled=True,all_business_gates_closed=True,no_claim_of_new_token_success=True)
except Exception as e:
 out.update(failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>'])
 if changed:
  shutil.copyfile(backup,G/'private/app.env');run(cmd+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=90);out['staging_config_restored']=True
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'sid-staging-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
