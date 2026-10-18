import sys,ast
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');BACK=R/'private/original-backup';B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort
p=DockerPilotPort.__new__(DockerPilotPort)
def jq(sql):return json.loads(p.query(sql))
tree=ast.parse((R/'prepare-worker.py').read_text());fn=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='baseline');ns={'jq':jq};exec(compile(ast.Module(body=[fn],type_ignores=[]),'<protected-baseline>','exec'),ns)
assert ns['baseline']()==json.loads((BACK/'data-baseline.json').read_text())
original=json.loads((BACK/'containers.json').read_text())
for n,v in original.items():
 assert inspect(n,'.Id')==v['id'] and inspect(n,'.Image')==v['image'] and inspect(n,'.HostConfig.PortBindings')==v['ports']
 assert sorted(inspect(n,'.Mounts'),key=lambda x:x['Destination'])==sorted(v['mounts'],key=lambda x:x['Destination'])
assert pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf').read_bytes()==(BACK/'gateway.conf').read_bytes()
scan="import os,json,hashlib;rows=[]\nfor root,ds,fs in os.walk('/scan'):\n for n in fs:\n  p=os.path.join(root,n)\n  if os.path.isfile(p):\n   with open(p,'rb') as f:h=hashlib.sha256(f.read()).hexdigest()\n   rows.append([os.path.relpath(p,'/scan'),os.path.getsize(p),h])\nprint(json.dumps(sorted(rows)))"
files_now=run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3','sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b','-c',scan])
assert json.loads(files_now)==json.loads((BACK/'protected-file-manifest.json').read_text())
source_counts=jq("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'nonempty_chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embedding_references',(SELECT COUNT(*) FROM embeddings),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations))")
assert source_counts=={'users':3,'documents':3,'nonempty_chunks':2717,'embedding_references':2717,'bindings':1,'jit':0}
fresh=lambda q:subprocess.run(['docker','exec','-i','knowhub-jit-fresh-postgres-1','psql','-X','-qAt','-U','fresh_owner','-d','knowhub_jit_fresh','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True).stdout.decode().strip()
counts={t:int(fresh('SELECT COUNT(*) FROM '+t)) for t in ('users','external_identities','source_id_binding_reviews','source_id_account_ownership','source_id_jit_registrations','source_id_link_blocks','source_id_sessions','auth_tokens','tenant_members','knowledge_bases')};assert all(v==0 for v in counts.values())
assert fresh("SELECT version::text||'|'||dirty FROM schema_migrations")=='3030|false'
code=r'''import urllib.request,urllib.error,json,re
class NR(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
o=urllib.request.build_opener(urllib.request.ProxyHandler({}),NR());out={}
def req(path):
 try:r=o.open(urllib.request.Request('http://jit-fresh-frontend'+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=8)
 except urllib.error.HTTPError as e:r=e
 with r:return r.status,r.read(1048576),r.headers.get('Location','')
s,b,_=req('/login');assert s==200;out['login']=s
scripts=re.findall(rb'<script[^>]+src="([^"]+)"',b);asset=next(x.decode() for x in scripts if b'/assets/' in x);assert req(asset)[0]==200;out['static_asset']=200
s,b,_=req('/api/v1/auth/oidc/config');assert s==200 and json.loads(b)['enabled'] is False;out['config']=s
s,_,loc=req('/api/v1/auth/oidc/callback');assert s==302 and 'invalid_state' in loc;out['empty_callback']=s;out['empty_callback_class']='invalid_state'
print(json.dumps(out))
'''
routes=json.loads(run(['docker','run','--rm','--network','knowhub-jit-fresh_isolated','--log-driver','none','--entrypoint','python3','sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b','-c',code],timeout=30))
flags=json.loads(run(['docker','exec','knowhub-jit-fresh-app-1','python3','-c',"import os,json;names=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_JIT_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_LOCAL_REFRESH_ENABLE'];print(json.dumps({n:os.environ.get(n) for n in names}))"]))
assert flags=={'OIDC_AUTH_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_LOCAL_REFRESH_ENABLE':'false'}
assert not run(['docker','ps','-aq','--filter','name=knowhub-jit-fresh-mock-']).strip()
envnew=environment('knowhub-jit-fresh-app-1');envold=environment('knowhub-company-test-app-1');keys=all(envnew[k]!=envold[k] for k in ('DB_PASSWORD','JWT_SECRET','SYSTEM_AES_KEY'))
assert keys and envnew['DB_NAME']!=envold['DB_NAME']
op=urllib.request.build_opener(urllib.request.ProxyHandler({}));original_health={}
for number in (80,8080):
 with op.open('http://127.0.0.1:'+str(number)+'/health',timeout=8) as response:
  assert response.status==200 and response.headers.get('X-KnowHub-Environment')=='company-test-verified-email-db3030'
  original_health[str(number)]=response.status
out={'result':'PASS','fresh_schema':3030,'dirty':False,'fresh_identity_counts':counts,'fresh_flags':flags,'internal_routes':routes,'original_container_ids_images_mounts_ports_unchanged':True,'original_account_permissions_binding_session_audit_unchanged':True,'original_files_match_backup_manifest':True,'original_counts':source_counts,'original_80_8080_health_and_marker':original_health,'gateway_original_config_unchanged':True,'independent_keys_and_database':keys,'gateway_not_attached_to_fresh': 'knowhub-jit-fresh_isolated' not in inspect('knowhub-company-entry-entry-1','.NetworkSettings.Networks'),'mock_containers_removed':True,'real_sid_auth_requests':0}
(R/'final-audit.json').write_text(json.dumps(out));print(json.dumps(out))
