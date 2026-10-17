"""Read-only deployment checks and private metadata staging; never SID calls."""
import os,shutil
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
G=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green')
P='knowhub-company-test';APP=P+'-app-1';PG=P+'-postgres-1'
OLD='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
NEW='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
SHA='5f988be51779f1a22bd9d027b7503fe6a8749f62'
FRONT='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
def sql(q):
 z=subprocess.run(['docker','exec','-i',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 assert z.returncode==0
 return z.stdout.decode().strip()
assert inspect(APP,'.Image')==OLD and inspect(APP,'.State.Health.Status')=='healthy'
assert inspect(P+'-frontend-1','.Image')==FRONT
assert json.loads(run(['docker','image','inspect',NEW]))[0]['Config']['Labels']['org.opencontainers.image.revision']==SHA
assert json.loads(run(['docker','image','inspect',OLD]))[0]['Id']==OLD
assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3029|false'
tx=int(sql('SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()'));assert tx==0
flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE']
env=json.loads(run(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(flags)]))
assert env==dict(zip(flags,['false','true','false','false','false','false','external','false','false','oauth_form','false','false']))
candidate=json.loads((B/'app-pin.json').read_text());rollback=json.loads((B/'app-rollback-pin.json').read_text())
assert candidate['services']['app']['image']==NEW and rollback['services']['app']['image']==OLD
assert candidate['services']['app']['environment']=={'SOURCEID_ACCOUNT_LINK_MODE':'trusted_ownership','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'false','SOURCEID_VERIFIED_EMAIL_POLICY_REF':''}
assert inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files']==str(G/'compose.json')
assert all(not inspect(P+'-'+s+'-1','.HostConfig.PortBindings') for s in ('app','postgres','redis','docreader'))
assert shutil.disk_usage(B).free>8*1024**3
backup=B/'private'/('deployment-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'));backup.mkdir(mode=0o700)
containers={name:metadata(name) for name in run(['docker','ps','--format','{{.Names}}']).decode().split()}
(backup/'containers-before.json').write_text(json.dumps(containers))
(backup/'app-config.json').write_text(json.dumps(inspect(APP,'.Config')))
(backup/'app-host-config.json').write_text(json.dumps(inspect(APP,'.HostConfig')))
for name,path in [('compose.json',G/'compose.json'),('app.env',G/'private/app.env'),('app-pin.json',B/'app-pin.json'),('app-rollback-pin.json',B/'app-rollback-pin.json'),('entry-nginx.conf',G.parent/'entry/nginx.conf'),('entry-compose.json',G.parent/'entry/compose.json')]:shutil.copyfile(path,backup/name)
cmd=['docker','compose','--project-name',P,'--project-directory',str(G),'-f',str(G/'compose.json'),'-f',str(B/'app-pin.json')]
(backup/'effective-compose.json').write_bytes(run(cmd+['config','--format','json']))
resolved=json.loads((backup/'effective-compose.json').read_text());assert resolved['services']['app']['image']==NEW and resolved['services']['frontend']['image']==FRONT
baseline=json.loads(sql("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'members',(SELECT COUNT(*) FROM tenant_members),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND length(trim(content))>0),'embeddings',(SELECT COUNT(*) FROM embeddings),'identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership))"))
assert baseline['users']==3 and baseline['documents']==3 and baseline['chunks']==baseline['embeddings']==2717 and all(baseline[k]==0 for k in ('identities','sessions','jit','ownership'))
connections={str(p):sum(1 for line in run(['ss','-Htn','state','established']).decode().splitlines() if len(line.split())>=4 and line.split()[-2].endswith(':'+str(p))) for p in (80,8080)}
activity=int(sql("SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active'"));assert activity==0
(B/'deployment-state.json').write_text(json.dumps({'stage':'PREFLIGHT_PASS','backup_directory':str(backup),'expected_old_app':OLD,'candidate_image':NEW,'source_sha':SHA,'baseline_counts':baseline}))
print(json.dumps({'result':'PREFLIGHT_PASS','backup_directory':str(backup),'app':'f40b1602f','frontend':'832b6652a','db':3029,'dirty':False,'candidate_image':NEW,'candidate_revision':SHA,'active_authorization_transactions':tx,'active_other_db_queries':activity,'entry_established_connections':connections,'gates':env,'counts':baseline,'internal_services_not_published':True,'free_gib':round(shutil.disk_usage(B).free/1024**3,1),'real_sid_requests':0}))
