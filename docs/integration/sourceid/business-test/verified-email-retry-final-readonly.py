"""Read-only business verification after automatic rollback; no auth or writes."""
import os,urllib.error,hashlib,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');S=json.loads((B/'retry-state.json').read_text());D=pathlib.Path(S['backup_directory'])
APP='knowhub-company-test-app-1';PG='knowhub-company-test-postgres-1';P='knowhub-company-test'
out={'result':'RUNNING','real_sid_requests':0,'business_identity_writes':False,'stage':'data_baseline'}
def sql(q):
 z=subprocess.run(['docker','exec','-i',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 assert z.returncode==0
 return z.stdout.decode().strip()
try:
 assert inspect(APP,'.Image')==S['expected_old_app'] and inspect(APP,'.State.Health.Status')=='healthy'
 assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3030|false'
 before=json.loads((D/'protected-data-baseline.json').read_text())
 for table,expected in before.items():
  if table.endswith('_count'):actual=int(sql('SELECT COUNT(*) FROM '+table[:-6]))
  elif table=='nonempty_chunks':actual=int(sql("SELECT COUNT(*) FROM chunks WHERE content IS NOT NULL AND length(trim(content))>0 AND deleted_at IS NULL"))
  elif expected:
   actual=sorted(json.loads(sql("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT "+','.join(expected[0])+' FROM '+table+')x')),key=lambda x:json.dumps(x,sort_keys=True))
  else:actual=json.loads(sql("SELECT COALESCE(json_agg(x),'[]') FROM (SELECT 1 FROM "+table+')x'))
  assert actual==expected
 out['original_user_roles_members_acl_documents_vector_references_unchanged']=True
 out['stage']='file_volume_compare'
 current=run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',S['expected_old_app'],'-c',(D/'file-baseline-code.txt').read_text()],timeout=90)
 assert json.loads(current)==json.loads((D/'protected-file-baseline.json').read_text());out['file_contents_unchanged']=True
 out['stage']='other_services_compare'
 prior=json.loads((D/'containers-before.json').read_text());names=list(prior)
 fmt='{"id":{{json .Id}},"image_id":{{json .Image}},"mounts":{{json .Mounts}},"ports":{{json .NetworkSettings.Ports}}}'
 values=[json.loads(x) for x in run(['docker','inspect','--format',fmt]+names).decode().splitlines()]
 for name,now in zip(names,values):
  if name==APP:continue
  old=prior[name];assert now['id']==old['id'] and now['image_id']==old['image_id']
  for m in old['mounts']:
   assert any(all(n.get(k)==m.get(k) for k in ('Type','Name','Source','Destination','RW')) for n in now['mounts'])
  assert now['ports']==old['ports']
 out['other_containers_images_mounts_ports_unchanged']=True
 out['stage']='firewall_compare'
 tooling='/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools'
 current=json.loads(run(['docker','run','--rm','--network','host','--cap-add','NET_ADMIN','--tmpfs','/nettools/run','--log-driver','none','--mount','type=bind,source='+tooling+',target=/nettools,readonly','--entrypoint','python3',S['expected_old_app'],'-c',(D/'firewall-read-code.txt').read_text()]))
 assert current==json.loads((D/'protected-firewall-before.json').read_text());out['firewall_rules_unchanged']=True
 out['stage']='routes'
 class NoRedirect(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a,**kw):return None
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect());routes=[];out['routes']=routes
 for port in (80,8080):
  for path in ('/login','/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback','/api/v1/auth/me','/api/v1/knowledge-bases','/api/v1/system/admin/feishu','/api/v1/me/feishu','/mcp/nonexistent-deployment-probe'):
   try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
   except urllib.error.HTTPError as e:r=e
   with r:
    body=r.read(1048576);status=r.status;routes.append({'port':port,'path':path,'status':status})
    if path=='/login':
     assert (status==200 if port==80 else status in (401,404))
     if port==80:
      assets=re.findall(r'(?:src|href)="(/assets/[^"?]+\.(?:js|css))"',body.decode());assert assets
      for asset in assets[:3]:
       with op.open(urllib.request.Request('http://127.0.0.1'+asset,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=15) as a:assert a.status==200
    elif path.endswith('/callback'):assert status==302
    elif path=='/health' or path.endswith('/oidc/config'):assert status==200
    else:assert status in (401,403,404)
    assert r.headers.get('X-KnowHub-Environment')=='company-test-f40-db3030'
 out['routes']=routes
 flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_VERIFIED_EMAIL_POLICY_REF','FEISHU_PERSONAL_ENABLE']
 env=json.loads(run(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(flags)]))
 assert [env[k] for k in flags[:15]]==['false','true','false','false','false','false','external','false','false','oauth_form','false','false','trusted_ownership','false','']
 assert sql('SELECT COUNT(*) FROM source_id_link_decisions')=='0' and sql('SELECT COUNT(*) FROM source_id_local_link_policies')=='0'
 out.update(result='PASS',stage='rollback_verified',current_app_revision='f40b1602f69ce5d88e1e87dc3cefae36e9bde356',current_app_image=S['expected_old_app'],frontend_revision='832b6652adf541892545a7eac8b68733f846ab95',schema=3030,dirty=False,gates=env,users=3,members=5,active_kbs=1,documents=3,nonempty_chunks=2717,embeddings=2717,actual_app_only_rollback_verified=True,root_cause='VERIFIER_COMPARED_UNORDERED_DOCKER_MOUNTS_AS_ORDERED_ARRAY',new_policy_implementation_not_currently_running=True,full_mcp_feishu_business_not_executed=True)
 S['stage']='ROLLED_BACK_VERIFIED_DB3030';(B/'retry-state.json').write_text(json.dumps(S))
except Exception as e:out.update(result='FAIL',failure_class=type(e).__name__,failure_lines=[v.lineno for v in traceback.extract_tb(e.__traceback__) if v.filename=='<stdin>'])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'retry-acceptance-result.json').write_text(json.dumps(out));print(json.dumps(out))
