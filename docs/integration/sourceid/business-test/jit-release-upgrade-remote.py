"""Only approved independent pilot APP + additive 3029. Fail closed."""
import os,shutil,time,traceback
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');project='knowhub-oa-sso-pilot';app=project+'-app-1';pg=project+'-postgres-1'
out={'result':'FAIL','real_authentication':False,'default_changed':False};stage='preflight';stopped=False
args=['docker','compose','-p',project,'--project-directory',str(p),'-f',str(p/'compose.json')]
oldimage='sha256:dbaff4ef55df5759c30e3535bf8be89882bd7ac74f1010d96875b7a3dbb21a6e'
def sql(q):
 z=subprocess.run(['docker','exec','-i',pg,'psql','-X','-U','clean_owner','-d','knowhub_oa_sso_pilot','-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
 if z.returncode:raise RuntimeError('protected_database_operation_failed')
 return z.stdout.decode().strip()
def healthy():
 for _ in range(65):
  if inspect(app,'.State.Health.Status')=='healthy':return
  time.sleep(1)
 raise RuntimeError('app_health_timeout')
try:
 build=json.loads((b/'build-status.json').read_text());assert build['result']=='PASS';newimage=build['image_id']
 assert build['revision']=='f40b1602f69ce5d88e1e87dc3cefae36e9bde356'
 assert inspect(app,'.Image')==oldimage and inspect(app,'.State.Health.Status')=='healthy'
 assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3028|false'
 assert not any(json.loads(sql("SELECT json_build_array((SELECT COUNT(*) FROM external_identities),(SELECT COUNT(*) FROM source_id_local_sessions),(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),(SELECT COUNT(*) FROM source_id_account_ownership))")))
 for k,want in {'OIDC_AUTH_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_SSO_POLICY_CONFIRMED':'false','SOURCEID_SESSION_MODE':'external','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_BASIC_ENCODING':'oauth_form','AUTO_MIGRATE':'false'}.items():assert environment(app).get(k)==want
 assert not environment(app).get('SOURCEID_ACCOUNT_SCOPE') or environment(app)['SOURCEID_ACCOUNT_SCOPE']=='pilot'
 assert run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',newimage]).decode().strip()==build['revision']
 before={prefix:{s:metadata(prefix+'-'+s+'-1') for s in services} for prefix,services in [(project,('frontend','postgres','redis','docreader','sid-network')),('knowhub-sourceid-business',('app','frontend','postgres','redis','docreader'))]}
 assert before[project]['frontend']['image_id']=='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
 stat=os.statvfs(str(b));free=stat.f_bavail*stat.f_frsize;assert free>8*1024**3;out['free_bytes_before']=free
 private=b/'private';private.mkdir(mode=0o700,exist_ok=True)
 backup=private/'before-upgrade';backup.mkdir(mode=0o700)
 (backup/'other-container-metadata.json').write_text(json.dumps(before))
 shutil.copyfile(p/'compose.json',backup/'compose.json');shutil.copyfile(p/'private/app.env',backup/'app.env');shutil.copyfile(p/'private/postgres.env',backup/'postgres.env')
 (backup/'app-metadata.json').write_text(json.dumps(metadata(app)))
 support=(p/'pilot-support.py').read_text().replace("PG='knowhub-oa-sso-pilot-prepare-pg'","PG='knowhub-oa-sso-pilot-postgres-1'")
 ns={};exec(compile(support,'pilot_support','exec'),ns)
 baseline=json.loads((p/'private/selected-data-baseline.json').read_text())
 assert ns['signatures'](baseline['columns'])==baseline['signatures']
 selected=ns['signatures'](baseline['columns']);file_before=ns['files']('knowhub-oa-sso-pilot-files');counts=ns['counts']()
 (backup/'integrity.json').write_text(json.dumps({'columns':baseline['columns'],'signatures':selected,'files':file_before,'counts':counts}))
 stage='freeze_pilot_app';run(['docker','stop',app]);stopped=True
 with (backup/'database3028.dump').open('wb') as f:
  z=subprocess.run(['docker','exec',pg,'pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
 with (backup/'files.tar').open('wb') as f:
  z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-oa-sso-pilot-files,target=/scan,readonly','--entrypoint','tar',oldimage,'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=90);assert z.returncode==0
 assert selected==ns['signatures'](baseline['columns']) and file_before==ns['files']('knowhub-oa-sso-pilot-files')
 assert (backup/'database3028.dump').stat().st_size>0 and (backup/'files.tar').stat().st_size>0
 stage='standard_migrate_003029_up'
 migrate="import os,subprocess,urllib.parse;u='postgres://clean_owner:'+urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe='')+'@127.0.0.1:5432/knowhub_oa_sso_pilot?sslmode=disable';z=subprocess.run(['migrate','-path','/migration','-database',u,'up','1'],stdout=subprocess.PIPE,stderr=subprocess.PIPE);raise SystemExit(z.returncode)"
 run(['docker','run','--rm','--network','container:'+pg,'--log-driver','none','--env-file',str(p/'private/postgres.env'),'--mount','type=bind,source='+str(b/'source/migrations/versioned')+',target=/migration,readonly','--entrypoint','python3',oldimage,'-c',migrate],timeout=120)
 assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3029|false'
 assert int(sql("SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready"))==0
 assert selected==ns['signatures'](baseline['columns']) and counts==ns['counts']()
 stage='app_only_upgrade'
 pin=b/'jit-app-pin.json';pin.write_text(json.dumps({'services':{'app':{'image':newimage}}},indent=2))
 (b/'jit-app-rollback-pin.json').write_text(json.dumps({'services':{'app':{'image':oldimage}}},indent=2))
 run(args+['-f',str(pin),'up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=60);stopped=False;healthy()
 # Nginx resolver may retain the old APP IP even when only APP was recreated.
 run(['docker','exec',project+'-frontend-1','nginx','-s','reload'])
 assert inspect(app,'.Image')==newimage
 assert (p/'private/app.env').read_bytes()==(backup/'app.env').read_bytes()
 for prefix,services in before.items():
  for name,v in services.items():assert metadata(prefix+'-'+name+'-1')['id']==v['id']
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 statuses={path:op.open('http://127.0.0.1:18090'+path,timeout=10).status for path in ('/login','/health','/api/v1/auth/oidc/config')};assert all(v==200 for v in statuses.values())
 out.update(result='PASS',revision=build['revision'],image_id=newimage,frontend_revision='832b6652adf541892545a7eac8b68733f846ab95',schema={'version':3029,'dirty':False},backup_directory=str(backup),database_backup_bytes=(backup/'database3028.dump').stat().st_size,file_backup_bytes=(backup/'files.tar').stat().st_size,selected_data_unchanged=True,counts=counts,other_container_ids_unchanged=True,environment_bytes_unchanged=True,health=statuses,account_scope='pilot DEFAULT',enterprise_all_enabled=False)
except Exception as e:
 out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>'])
 # Never force dirty migration state. Restart old APP only with a clean schema.
 try:
  schema=sql("SELECT version::text||'|'||dirty::text FROM schema_migrations");out['failure_schema']=schema
  if schema in ('3028|false','3029|false') and (b/'jit-app-rollback-pin.json').exists():
   run(args+['-f',str(b/'jit-app-rollback-pin.json'),'up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=60);healthy();out['old_app_restored']=True
  elif stopped and schema=='3028|false':run(['docker','start',app]);healthy();out['old_app_restored']=True
 except Exception:out['old_app_restored']=False
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(b/'upgrade-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
