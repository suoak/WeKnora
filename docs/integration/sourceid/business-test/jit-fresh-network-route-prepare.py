"""Prepare fresh-only constrained egress and inert gateway candidate."""
import os,shutil,time
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');NEW='knowhub-jit-fresh'
assert json.loads((R/'prepare-result.json').read_text())['result']=='PASS'
original=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf');old=original.read_bytes()
origids={n:inspect(n,'.Id') for n in ('knowhub-company-test-app-1','knowhub-company-test-frontend-1','knowhub-company-test-postgres-1','knowhub-company-test-redis-1','knowhub-company-test-docreader-1','knowhub-company-entry-entry-1')}
base=json.loads(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green/compose.json').read_text());c=json.loads((R/'compose.json').read_text());E=R/'egress';E.mkdir(mode=0o700)
source=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green/egress')
for name in ('netguard.py','sid-upstream.json'):shutil.copyfile(source/name,E/name)
cfg=json.loads((E/'sid-upstream.json').read_text());network=json.loads(run(['docker','network','inspect',NEW+'_isolated']))[0];cfg['internal_subnet']=network['IPAM']['Config'][0]['Subnet'];(E/'sid-upstream.json').write_text(json.dumps(cfg))
side=base['services']['sid-network'];side['volumes']=[str(E)+':/egress:ro','/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools:/nettools:ro'];side['networks']={'isolated':{'aliases':['app','jit-fresh-app']},'sid-egress':{}}
c['services']['sid-network']=side;c['networks']['sid-egress']={'enable_ipv6':False}
c['services']['app'].pop('networks',None);c['services']['app']['network_mode']='service:sid-network'
c['services']['frontend']['networks']={'isolated':{'aliases':['jit-fresh-frontend']}}
(R/'compose.json').write_text(json.dumps(c,indent=2))
compose=['docker','compose','-p',NEW,'--project-directory',str(R),'-f',str(R/'compose.json')]
run(compose+['up','-d','--no-deps','--no-build','--pull','never','sid-network'],timeout=90)
for _ in range(45):
 if inspect(NEW+'-sid-network-1','.State').get('Health',{}).get('Status')=='healthy':break
 time.sleep(1)
else:raise RuntimeError('fresh_egress_not_healthy')
run(compose+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app','frontend'],timeout=90)
for _ in range(45):
 if inspect(NEW+'-app-1','.State').get('Health',{}).get('Status')=='healthy':break
 time.sleep(1)
else:raise RuntimeError('fresh_app_not_healthy')
assert old.count(b'set $upstream frontend:80;')==1 and old.count(b'set $upstream app:8080;')==1
candidate=old.replace(b'set $upstream frontend:80;',b'set $upstream jit-fresh-frontend:80;').replace(b'set $upstream app:8080;',b'set $upstream jit-fresh-app:8080;').replace(b'company-test-verified-email-db3030',b'jit-fresh-isolated-db3030')
G=R/'private/route';G.mkdir(mode=0o700);(G/'original.conf').write_bytes(old);(G/'candidate.conf').write_bytes(candidate)
run(['docker','run','--rm','--network',NEW+'_isolated','--log-driver','none','--mount','type=bind,source='+str(G/'candidate.conf')+',target=/etc/nginx/nginx.conf,readonly','--entrypoint','nginx',inspect('knowhub-company-entry-entry-1','.Image'),'-t'],timeout=30)
assert original.read_bytes()==old and all(inspect(n,'.Id')==cid for n,cid in origids.items())
assert NEW+'_isolated' not in inspect('knowhub-company-entry-entry-1','.NetworkSettings.Networks')
print(json.dumps({'result':'PREPARED_NOT_SWITCHED','fresh_app_healthy':True,'constrained_sid_egress_cloned':True,'original_gateway_config_unchanged':True,'original_containers_unchanged':True,'gateway_not_connected_to_fresh_network':True,'candidate_nginx_syntax':'PASS','original_entry_ports_unchanged':True,'real_auth_requests':0}))
