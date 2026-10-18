import time,sys
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');P=R/'private';BACK=P/'original-backup'
out=json.loads((R/'prepare-result.json').read_text());assert out['stage']=='standard_fresh_migrations'
NEW='knowhub-jit-fresh';I='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
def fresh(sql):return subprocess.run(['docker','exec','-i',NEW+'-postgres-1','psql','-X','-qAt','-U','fresh_owner','-d','knowhub_jit_fresh','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+sql+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True).stdout.decode().strip()
assert fresh("SELECT version::text||'|'||dirty FROM schema_migrations")=='3030|false'
tables=('users','external_identities','source_id_binding_reviews','source_id_account_ownership','source_id_sessions','source_id_jit_registrations','source_id_link_blocks','auth_tokens','tenant_members','knowledge_bases')
counts={t:int(fresh('SELECT COUNT(*) FROM '+t)) for t in tables};assert all(v==0 for v in counts.values())
cmd=['docker','compose','-p',NEW,'--project-directory',str(R),'-f',str(R/'compose.json')]
c=json.loads((R/'compose.json').read_text());c['services']['app']['healthcheck']={'test':['CMD','python3','-c',"import urllib.request;urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:8080/health',timeout=3)"],'interval':'5s','timeout':'4s','retries':12,'start_period':'10s'};(R/'compose.json').write_text(json.dumps(c,indent=2))
run(cmd+['up','-d','--no-build','--pull','never','app','frontend'],timeout=120)
for _ in range(60):
 if inspect(NEW+'-app-1','.State').get('Health',{}).get('Status')=='healthy':break
 time.sleep(1)
else:raise RuntimeError('fresh_app_not_healthy')
assert counts=={t:int(fresh('SELECT COUNT(*) FROM '+t)) for t in tables}
original=json.loads((BACK/'containers.json').read_text())
for name,old in original.items():
 assert inspect(name,'.Id')==old['id'] and inspect(name,'.Image')==old['image'] and inspect(name,'.HostConfig.PortBindings')==old['ports']
assert pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf').read_bytes()==(BACK/'gateway.conf').read_bytes()
probe="import urllib.request,json; o=urllib.request.build_opener(urllib.request.ProxyHandler({}));r=o.open('http://127.0.0.1:8080/api/v1/auth/oidc/config');v=json.load(r);assert v['enabled'] is False;print(json.dumps({'config_status':r.status,'oidc_enabled':v['enabled']}))"
health=json.loads(run(['docker','exec',NEW+'-app-1','python3','-c',probe]))
for name in ('postgres','redis','docreader','app','frontend'):assert not inspect(NEW+'-'+name+'-1','.HostConfig.PortBindings')
assert inspect(NEW+'-app-1','.Image')==I
out.update(result='PASS',stage='FRESH_CLOSED_READY',project=NEW,app_revision='5f988be51779f1a22bd9d027b7503fe6a8749f62',schema=3030,dirty=False,empty_identity_counts=counts,health=health,independent_database_redis_files_and_keys=True,internal_network_only=True,all_host_ports_unpublished=True,original_containers_and_routes_unchanged=True,empty_fresh_instance_not_company_eligibility_proof=True,migration_initial_start_failure=True,migration_force_or_version_skip=False)
out.pop('failure_class',None);out.pop('failure_lines',None)
(R/'prepare-result.json').write_text(json.dumps(out));print(json.dumps(out))
