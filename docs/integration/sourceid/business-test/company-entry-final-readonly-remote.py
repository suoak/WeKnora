"""Published entry final whitelist evidence; no authentication or raw logs."""
import time
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');G='knowhub-company-test';out={'result':'PASS','real_sid_login':False,'production_accessed':False}
assert json.loads((B/'publish-result.json').read_text())['result']=='PASS' and json.loads((B/'validation-result.json').read_text())['result']=='PASS'
out['services']={s:metadata(G+'-'+s+'-1') for s in ('app','frontend','postgres','redis','docreader','sid-network')}
out['images']={s:run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',out['services'][s]['image_id']]).decode().strip() for s in ('app','frontend')}
assert out['images']['app']=='f40b1602f69ce5d88e1e87dc3cefae36e9bde356' and out['images']['frontend']=='832b6652adf541892545a7eac8b68733f846ab95'
env=environment(G+'-app-1');expected={'OIDC_AUTH_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_EMAIL_CONTRACT_CONFIRMED':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_JIT_POLICY_CONFIRMED':'false','SOURCEID_ENTERPRISE_POLICY_CONFIRMED':'false','SOURCEID_SSO_POLICY_CONFIRMED':'false','SOURCEID_SESSION_MODE':'external','SOURCEID_ACCOUNT_SCOPE':'enterprise_all','SOURCEID_ADMISSION_MODE':'oidc_sso_approved','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED':'false','SOURCEID_LOGOUT_ENABLE':'false','SOURCEID_DIAGNOSTICS_ENABLE':'false','SOURCEID_BASIC_ENCODING':'oauth_form','AUTO_MIGRATE':'false'}
assert all(env.get(k)==v for k,v in expected.items());out['flags']=expected
oldenv=environment('knowhub-sourceid-business-app-1');pilotenv=environment('knowhub-oa-sso-pilot-app-1')
out['jwt_isolated_from_old_and_pilot']=bool(env.get('JWT_SECRET')) and env['JWT_SECRET']!=oldenv.get('JWT_SECRET') and env['JWT_SECRET']!=pilotenv.get('JWT_SECRET');assert out['jwt_isolated_from_old_and_pilot']
out['callback_matches_approved']=env.get('OIDC_AUTH_REDIRECT_URI')=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback';assert out['callback_matches_approved']
for s in ('app','postgres','redis','docreader','sid-network'):assert not inspect(G+'-'+s+'-1','.HostConfig.PortBindings')
out['internal_services_no_host_ports']=True
prior=json.loads((B/'private/pre-switch/containers.json').read_text())
out['old_stack']={s:{'container_id_preserved':inspect('knowhub-sourceid-business-'+s+'-1','.Id')==m['id'],'running':inspect(m['id'],'.State.Running')} for s,m in prior['knowhub-sourceid-business'].items()}
out['pilot_container_ids_unchanged']=all(inspect('knowhub-oa-sso-pilot-'+s+'-1','.Id')==m['id'] and inspect(m['id'],'.State.Running') for s,m in prior['knowhub-oa-sso-pilot'].items());assert out['pilot_container_ids_unchanged']
op=urllib.request.build_opener(urllib.request.ProxyHandler({}));out['unauthenticated_api']={}
for path in ('/api/v1/auth/me','/api/v1/knowledge-bases','/api/v1/mcp-endpoints','/api/v1/me/feishu/status','/api/v1/system/admin/feishu/config'):
 try:r=op.open('http://127.0.0.1'+path,timeout=10)
 except urllib.error.HTTPError as e:r=e
 out['unauthenticated_api'][path]=r.code
 assert r.code in (401,403)
# Real rejected-source probes from an unrelated Docker bridge, no auth.
targets=[('172.30.232.42',80),('172.30.232.42',8080)]
for s,port in (('postgres',5432),('redis',6379)):
 ip=next(iter(inspect(G+'-'+s+'-1','.NetworkSettings.Networks').values()))['IPAddress'];targets.append((ip,port))
code="""import socket,json,urllib.request,urllib.error
out=[]
for host,port in TARGETS:
 if port in (80,8080):
  try:
   with urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://'+host+':'+str(port)+'/health',timeout=2) as r:denied=False
  except urllib.error.HTTPError as e:denied=e.code==403
  except OSError:denied=True
 else:
  s=socket.socket();s.settimeout(1)
  try:s.connect((host,port));denied=False
  except OSError:denied=True
  finally:s.close()
 out.append({'port':port,'denied':denied})
print(json.dumps(out))
""".replace('TARGETS',repr(targets))
out['unapproved_bridge_probes']=json.loads(run(['docker','run','--rm','--network','bridge','--log-driver','none','--entrypoint','python3',out['services']['app']['image_id'],'-c',code],timeout=20));(B/'unapproved-probes.json').write_text(json.dumps(out['unapproved_bridge_probes']));assert all(x['denied'] for x in out['unapproved_bridge_probes'])
out['entry_bindings']=inspect('knowhub-company-entry-entry-1','.HostConfig.PortBindings');assert all(x['HostIp']=='0.0.0.0' for values in out['entry_bindings'].values() for x in values)
out['rollback_script_ready']=(B/'private/rollback-entry.sh').is_file()
# Safe booleans from previously accepted business fixtures, not raw responses.
r=json.loads((B/'validation/acceptance-result.json').read_text());out['business_fixture_summary']={k:v for k,v in r.items() if isinstance(v,(bool,int)) or (k=='result' and v in ('PASS','FAIL'))}
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'final-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
