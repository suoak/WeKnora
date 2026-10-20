"""Prepare a separate green stack; never bind 80/8080 or change old services."""
import os,shutil,secrets,time,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');P=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');G=B/'green';PROJECT='knowhub-company-test'
IMAGE='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413';FRONT='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
stage='preflight';out={'result':'RUNNING','old_entry_changed':False,'real_sid_login':False}
def progress():
 (B/'prepare-status.json').write_text(json.dumps(dict(out,stage=stage)))
def sql(pg,q):
 z=subprocess.run(['docker','exec','-i',pg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
 if z.returncode:raise RuntimeError('controlled_sql_failed')
 return z.stdout.decode().strip()
def archive(volume,path):
 with path.open('wb') as f:
  z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source='+volume+',target=/scan,readonly','--entrypoint','tar',IMAGE,'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=120)
 assert z.returncode==0 and path.stat().st_size>0
def dump(pg,path):
 with path.open('wb') as f:
  z=subprocess.run(['docker','exec',pg,'sh','-c','exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120)
 assert z.returncode==0 and path.stat().st_size>0
try:
 resume=G.exists() and (B/'prepare-result.json').exists() and json.loads((B/'prepare-result.json').read_text()).get('stage')=='restore_3029_copy'
 if resume:
  backup=B/'private/pre-switch';prior=json.loads((backup/'containers.json').read_text())
  cmd=['docker','compose','-p',PROJECT,'--project-directory',str(G),'-f',str(G/'compose.json')]
  assert not run(['docker','ps','-a','--filter','name=^'+PROJECT+'-app-1$','--format','{{.ID}}']).strip()
 else:
  progress();assert inspect('knowhub-sourceid-business-app-1','.Image')=='sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c'
  assert inspect('knowhub-oa-sso-pilot-app-1','.Image')==IMAGE
  assert sql('knowhub-sourceid-business-postgres-1',"SELECT version::text||'|'||dirty FROM schema_migrations")=='3027|false'
  assert sql('knowhub-oa-sso-pilot-postgres-1',"SELECT version::text||'|'||dirty FROM schema_migrations")=='3029|false'
  assert not run(['docker','ps','-a','--filter','label=com.docker.compose.project='+PROJECT,'--format','{{.ID}}']).strip()
  assert os.statvfs(str(B)).f_bavail*os.statvfs(str(B)).f_frsize>12*1024**3
  G.mkdir(mode=0o700);(G/'private').mkdir(mode=0o700);backup=B/'private/pre-switch';backup.mkdir(mode=0o700)
  prior={project:{s:metadata(project+'-'+s+'-1') for s in ('app','frontend','postgres','redis','docreader')} for project in ('knowhub-sourceid-business','knowhub-oa-sso-pilot')}
  (backup/'containers.json').write_text(json.dumps(prior))
  for project,items in prior.items():
   for s in ('app','frontend'):
    for i,f in enumerate(items[s]['compose_files'].split(',')):shutil.copyfile(f,backup/(project+'-'+s+'-compose-'+str(i)+pathlib.Path(f).suffix))
   cfg={s:inspect(m['id'],'.Config') for s,m in items.items()}
   (backup/(project+'-protected-config.json')).write_text(json.dumps(cfg))
   for s in ('app','postgres'):
    (backup/(project+'-'+s+'.env')).write_text('\n'.join(inspect(items[s]['id'],'.Config.Env'))+'\n')
  stage='fresh_consistent_snapshots';progress()
  # Auth gates closed, no active access; freeze both APP writers for snapshots,
  # then immediately resume both original stacks before preparing green.
  frozen=[]
  try:
   for project in prior:run(['docker','pause',project+'-app-1']);frozen.append(project+'-app-1')
   for project,volume in [('knowhub-sourceid-business','knowhub-sourceid-business_files'),('knowhub-oa-sso-pilot','knowhub-oa-sso-pilot-files')]:
    dump(project+'-postgres-1',backup/(project+'.dump'));archive(volume,backup/(project+'-files.tar'))
  finally:
   for name in frozen:run(['docker','unpause',name])
  stage='copy_green_configuration';progress()
  c=json.loads((P/'compose.json').read_text());c['services']['app']['image']=IMAGE;c['services']['frontend']['image']=FRONT
  c['services']['frontend']['ports']=['127.0.0.1:18091:80']
  c['volumes']['postgres']={'name':PROJECT+'-pgdata'};c['volumes']['files']={'name':PROJECT+'-files'}
  for service in c['services'].values():service['restart']='unless-stopped'
  c['services']['app']['volumes']=[x.replace('files:/data/files:ro','files:/data/files') for x in c['services']['app']['volumes']]
  for service in c['services'].values():
   if 'volumes' in service:service['volumes']=[x.replace('./egress:',str(P/'egress')+':').replace('./egress/nettools:',str(P/'egress/nettools')+':') for x in service['volumes']]
  (G/'compose.json').write_text(json.dumps(c,indent=2))
  # Independent PG and JWT; candidate AES stays unchanged for its encrypted
  # configuration compatibility. Never import old default env or Redis.
  env=environment('knowhub-oa-sso-pilot-app-1');pgenv=environment('knowhub-oa-sso-pilot-postgres-1')
  password=secrets.token_urlsafe(40);pgenv={k:v for k,v in pgenv.items() if k.startswith('POSTGRES_')};pgenv['POSTGRES_PASSWORD']=password
  env['DB_PASSWORD']=password;env['JWT_SECRET']=secrets.token_hex(32)
  for k in ('OIDC_AUTH_ENABLE','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_JIT_POLICY_CONFIRMED','SOURCEID_ENTERPRISE_POLICY_CONFIRMED','SOURCEID_SSO_POLICY_CONFIRMED','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED','SOURCEID_LOGOUT_ENABLE','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE'):env[k]='false'
  env['SOURCEID_PROTOCOL_ONLY']='true';env['SOURCEID_SESSION_MODE']='external';env['SOURCEID_ACCOUNT_SCOPE']='enterprise_all';env['SOURCEID_ADMISSION_MODE']='oidc_sso_approved';env['SOURCEID_BASIC_ENCODING']='oauth_form'
  for name,values in [('app.env',env),('postgres.env',pgenv)]:
   assert all('\n' not in v and '\r' not in v for v in values.values())
   (G/'private'/name).write_text('\n'.join(k+'='+v for k,v in values.items())+'\n')
  shutil.copytree(P/'private/ownership',G/'private/ownership')
  cmd=['docker','compose','-p',PROJECT,'--project-directory',str(G),'-f',str(G/'compose.json')]
  (B/'private/green-compose-order.json').write_text(json.dumps(cmd))
  stage='start_green_dependencies';progress();run(cmd+['up','-d','--no-build','--pull','never','postgres','redis','docreader','sid-network'],timeout=120)
  for _ in range(90):
   if inspect(PROJECT+'-postgres-1','.State.Health.Status')=='healthy':break
   time.sleep(1)
  else:raise RuntimeError('green_postgres_health_timeout')
 stage='restore_3029_copy';progress()
 with (backup/'knowhub-oa-sso-pilot.dump').open('rb') as f:
  z=subprocess.run(['docker','exec','-i',PROJECT+'-postgres-1','pg_restore','-U','clean_owner','-d','knowhub_oa_sso_pilot','--no-owner','--no-acl','--clean','--if-exists','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
 if z.returncode:
  errors=z.stderr.decode('utf-8','replace')
  out['restore_error_categories']=[x for x in ('already exists','does not exist','permission denied','not available','unrecognized','unsupported version','could not','violates') if x in errors]
  raise RuntimeError('green_restore_failed')
 run(['docker','volume','create',PROJECT+'-files'])
 with (backup/'knowhub-oa-sso-pilot-files.tar').open('rb') as f:
  z=subprocess.run(['docker','run','--rm','-i','--network','none','--log-driver','none','--mount','type=volume,source='+PROJECT+'-files,target=/target','--entrypoint','tar',IMAGE,'-C','/target','-xf','-'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
 assert z.returncode==0
 # Defense in depth, new target only; sessions table is conversation data,
 # not auth sessions, and is deliberately preserved.
 sql(PROJECT+'-postgres-1',"BEGIN;UPDATE auth_tokens SET is_revoked=TRUE;UPDATE tenant_api_keys SET revoked_at=COALESCE(revoked_at,NOW());UPDATE source_id_sessions SET revoked=TRUE;DELETE FROM mcp_oauth_tokens;UPDATE oidc_transactions SET consumed=TRUE,result='',nonce='',verifier='';COMMIT;")
 stage='start_green_app_frontend';progress();run(cmd+['up','-d','--no-build','--pull','never','app','frontend'],timeout=120)
 for _ in range(90):
  if inspect(PROJECT+'-app-1','.State.Health.Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('green_app_health_timeout')
 assert sql(PROJECT+'-postgres-1',"SELECT version::text||'|'||dirty FROM schema_migrations")=='3029|false'
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}));checks={}
 for path in ('/login','/health','/api/v1/auth/oidc/config'):
  with op.open('http://127.0.0.1:18091'+path,timeout=15) as r:checks[path]=r.status
 assert all(v==200 for v in checks.values())
 stage='green_integrity';progress()
 support=(P/'pilot-support.py').read_text().replace("PG='knowhub-oa-sso-pilot-prepare-pg'","PG='"+PROJECT+"-postgres-1'")
 ns={};exec(compile(support,'private_support','exec'),ns)
 baseline=json.loads((P/'private/selected-data-baseline.json').read_text());current=ns['signatures'](baseline['columns'])
 assert current==baseline['signatures']
 assert ns['files'](PROJECT+'-files')==ns['files']('knowhub-oa-sso-pilot-files')
 q="SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embeddings',(SELECT COUNT(*) FROM embeddings),'active_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'active_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'identities',(SELECT COUNT(*) FROM external_identities),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'enterprise_sessions',(SELECT COUNT(*) FROM source_id_sessions))"
 counts=json.loads(sql(PROJECT+'-postgres-1',q));assert counts['documents']==3 and counts['chunks']==counts['embeddings']==2717
 assert all(counts[k]==0 for k in ('active_tokens','active_keys','identities','ownership','jit','enterprise_sessions'))
 for project,items in prior.items():
  for s,m in items.items():assert metadata(project+'-'+s+'-1')['id']==m['id'] and inspect(m['id'],'.State.Running')
 out.update(result='PASS',stage='green_ready_not_published',green_project=PROJECT,green_root=str(G),backup_directory=str(backup),image_id=IMAGE,frontend_image_id=FRONT,schema=3029,dirty=False,counts=counts,data_permission_signatures_match=True,files_match=True,original_stacks_unchanged=True,health=checks,db_and_file_backups_bytes={x.name:x.stat().st_size for x in backup.iterdir() if x.suffix in ('.dump','.tar')})
except Exception as e:
 out.update(result='FAIL',stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'prepare-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
