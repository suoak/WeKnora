"""Adjust cloned namespace's internal subnet, never the original guard."""
import os,shutil,time,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');P=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');G=B/'green';project='knowhub-company-test'
out={'result':'FAIL','old_entry_changed':False,'real_sid_login':False};stage='green_namespace_subnet'
try:
 c=json.loads((G/'compose.json').read_text());e=G/'egress';e.mkdir(mode=0o700)
 shutil.copyfile(P/'egress/netguard.py',e/'netguard.py')
 cfg=json.loads((P/'egress/sid-upstream.json').read_text());net=json.loads(run(['docker','network','inspect',project+'_isolated']))[0];cfg['internal_subnet']=net['IPAM']['Config'][0]['Subnet']
 (e/'sid-upstream.json').write_text(json.dumps(cfg))
 c['services']['sid-network']['volumes']=[str(e)+':/egress:ro',str(P/'egress/nettools')+':/nettools:ro']
 (G/'compose.json').write_text(json.dumps(c,indent=2));cmd=['docker','compose','-p',project,'--project-directory',str(G),'-f',str(G/'compose.json')]
 run(cmd+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','sid-network'],timeout=90)
 for _ in range(60):
  if inspect(project+'-sid-network-1','.State.Health.Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('green_namespace_not_healthy')
 run(cmd+['up','-d','--no-build','--pull','never','--force-recreate','app','frontend'],timeout=120)
 assert inspect(project+'-app-1','.State.Health.Status')=='healthy'
 stage='green_integrity'
 support=(P/'pilot-support.py').read_text().replace("PG='knowhub-oa-sso-pilot-prepare-pg'","PG='"+project+"-postgres-1'");ns={};exec(compile(support,'private_support','exec'),ns)
 baseline=json.loads((P/'private/selected-data-baseline.json').read_text());assert ns['signatures'](baseline['columns'])==baseline['signatures']
 assert ns['files'](project+'-files')==ns['files']('knowhub-oa-sso-pilot-files')
 assert ns['sql']("SELECT version::text||'|'||dirty FROM schema_migrations")=='3029|false'
 counts=ns['scalar']("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embeddings',(SELECT COUNT(*) FROM embeddings),'active_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'active_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'identities',(SELECT COUNT(*) FROM external_identities),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'enterprise_sessions',(SELECT COUNT(*) FROM source_id_sessions))")
 assert counts['documents']==3 and counts['chunks']==counts['embeddings']==2717 and all(counts[k]==0 for k in ('active_tokens','active_keys','identities','ownership','jit','enterprise_sessions'))
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}));health={path:op.open('http://127.0.0.1:18091'+path,timeout=10).status for path in ('/login','/health','/api/v1/auth/oidc/config')};assert all(x==200 for x in health.values())
 prior=json.loads((B/'private/pre-switch/containers.json').read_text())
 for project_name,items in prior.items():
  for s,m in items.items():assert inspect(m['id'],'.State.Running') and inspect(project_name+'-'+s+'-1','.Id')==m['id']
 out.update(result='PASS',stage='green_ready_not_published',green_project=project,green_root=str(G),backup_directory=str(B/'private/pre-switch'),image_id=inspect(project+'-app-1','.Image'),frontend_image_id=inspect(project+'-frontend-1','.Image'),schema=3029,dirty=False,counts=counts,data_permission_signatures_match=True,files_match=True,original_stacks_unchanged=True,health=health,namespace_internal_subnet_corrected=True,db_and_file_backups_bytes={x.name:x.stat().st_size for x in (B/'private/pre-switch').iterdir() if x.suffix in ('.dump','.tar')})
except Exception as e:out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>'])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'prepare-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
