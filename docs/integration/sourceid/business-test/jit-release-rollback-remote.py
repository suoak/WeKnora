"""Actual pilot APP rollback/re-upgrade; keep DB3029 and every other service."""
import time,traceback
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');app='knowhub-oa-sso-pilot-app-1'
args=['docker','compose','-p','knowhub-oa-sso-pilot','--project-directory',str(p),'-f',str(p/'compose.json')]
out={'result':'FAIL','database_downgraded':False,'real_authentication':False};stage='preflight'
new=json.loads((b/'build-status.json').read_text())['image_id'];old='sha256:dbaff4ef55df5759c30e3535bf8be89882bd7ac74f1010d96875b7a3dbb21a6e'
def switch(pin,image):
 run(args+['-f',str(b/pin),'up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=60)
 for _ in range(65):
  if inspect(app,'.State.Health.Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('rollback_health_failed')
 assert inspect(app,'.Image')==image
 run(['docker','exec','knowhub-oa-sso-pilot-frontend-1','nginx','-s','reload'])
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 status={path:op.open('http://127.0.0.1:18090'+path,timeout=8).status for path in ('/health','/login','/api/v1/auth/oidc/config')};assert all(v==200 for v in status.values());return status
try:
 assert json.loads((b/'validation-result.json').read_text())['result']=='PASS'
 before=json.loads((b/'private/before-upgrade/other-container-metadata.json').read_text())
 env=(p/'private/app.env').read_bytes()
 stage='old_app_on_3029';out['old_app_health']=switch('jit-app-rollback-pin.json',old)
 stage='restore_enterprise_candidate';out['candidate_health']=switch('jit-app-pin.json',new)
 assert env==(p/'private/app.env').read_bytes()
 for prefix,services in before.items():
  for name,v in services.items():assert metadata(prefix+'-'+name+'-1')['id']==v['id']
 schema=run(['docker','exec','knowhub-oa-sso-pilot-postgres-1','psql','-X','-U','clean_owner','-d','knowhub_oa_sso_pilot','-qAt','-c',"SELECT version::text||'|'||dirty::text FROM schema_migrations"]).decode().strip();assert schema=='3029|false'
 out.update(result='PASS',old_revision='09e6d86db731953834916e58ac5f3f5985ad0aa1',candidate_revision='f40b1602f69ce5d88e1e87dc3cefae36e9bde356',candidate_restored=True,other_container_ids_unchanged=True,environment_bytes_unchanged=True,schema=schema)
except Exception as e:out.update(failed_stage=stage,failure_class=type(e).__name__)
finally:
 if inspect(app,'.Image')!=new:
  try:switch('jit-app-pin.json',new);out['candidate_recovery']=True
  except Exception:out['candidate_recovery']=False
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(b/'rollback-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
