"""Read-only source snapshot, migrate a network-isolated disposable DB only."""
import os,time
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017')
private=B/'private';private.mkdir(mode=0o700,exist_ok=True)
SOURCE='knowhub-company-test-postgres-1';TARGET='knowhub-email-policy-migration-pg'
DB='knowhub_oa_sso_pilot';CLONE='verified_clone'
APPIMAGE='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
def call(args,data=None,timeout=90):
 z=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_isolated_migration_failed')
 return z.stdout
def query(pg,sql):
 return call(['docker','exec','-i',pg,'psql','-X','-U','clean_owner' if pg==SOURCE else 'verified_mock','-d',DB if pg==SOURCE else CLONE,'-qAt','-v','ON_ERROR_STOP=1'],('BEGIN READ ONLY;SET LOCAL TIME ZONE \'UTC\';SET LOCAL log_statement=\'none\';SET LOCAL log_min_error_statement=\'panic\';'+sql+';ROLLBACK;').encode()).decode().strip()
def baseline(pg):
 out={}
 selections={'users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants,deleted_at','tenants':'id,name','tenant_members':'user_id,tenant_id,role,status','knowledge_bases':'id,tenant_id,deleted_at','knowledges':'id,knowledge_base_id,deleted_at'}
 for table,cols in selections.items():out[table]=json.loads(query(pg,'SELECT COALESCE(json_agg(x),\'[]\') FROM (SELECT '+cols+' FROM '+table+' ORDER BY '+cols.split(',')[0]+')x'))
 tables=json.loads(query(pg,"SELECT COALESCE(json_agg(table_name),'[]') FROM information_schema.tables WHERE table_schema='public'"))
 for table in ('kb_shares','knowledge_base_shares','knowledge_base_members','knowledge_base_permissions','organization_tenant_members','embeddings'):
  if table not in tables:continue
  cols=json.loads(query(pg,"SELECT COALESCE(json_agg(column_name),'[]') FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'"))
  fields=[c for c in ('id','user_id','tenant_id','knowledge_base_id','organization_id','source_tenant_id','permission','knowledge_id','chunk_id','role','status','permission','access_level','deleted_at') if c in cols]
  if fields:
   rows=json.loads(query(pg,"SELECT COALESCE(json_agg(x),'[]') FROM (SELECT "+','.join(fields)+" FROM "+table+")x"))
   out[table+'_protected_references']=sorted(rows,key=lambda x:json.dumps(x,sort_keys=True))
 for table in ('chunks','embeddings','external_identities','source_id_sessions','source_id_account_ownership','source_id_jit_registrations'):
  out[table]=int(query(pg,'SELECT COUNT(*) FROM '+table))
 out['nonempty_chunks']=int(query(pg,"SELECT COUNT(*) FROM chunks WHERE content IS NOT NULL AND length(trim(content))>0 AND deleted_at IS NULL"))
 return out
owned=False;out={'result':'RUNNING','source_written':False,'source_app_recreated':False,'real_sid_requests':0}
try:
 assert query(SOURCE,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3029|false'
 out['stage']='source_baseline';before=baseline(SOURCE)
 out['stage']='protected_dump';path=private/'company-3029-before-policy.dump'
 with path.open('wb') as f:
  z=subprocess.run(['docker','exec',SOURCE,'pg_dump','-U','clean_owner','-d',DB,'-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120)
  assert z.returncode==0
 path.chmod(0o600)
 assert subprocess.run(['docker','inspect',TARGET],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
 image=json.loads(call(['docker','inspect',SOURCE]))[0]['Image']
 call(['docker','run','-d','--name',TARGET,'--network','none','--memory','768m','--log-driver','none','--tmpfs','/var/lib/postgresql/data:rw','-e','PGDATA=/var/lib/postgresql/data/mock','-e','POSTGRES_USER=verified_mock','-e','POSTGRES_PASSWORD=synthetic-only','-e','POSTGRES_DB='+CLONE,image]);owned=True
 for _ in range(30):
  if subprocess.run(['docker','exec',TARGET,'pg_isready','-h','127.0.0.1','-U','verified_mock','-d',CLONE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('disposable_database_not_ready')
 out['stage']='restore_clone'
 with path.open('rb') as f:
  z=subprocess.run(['docker','exec','-i',TARGET,'pg_restore','--clean','--if-exists','-U','verified_mock','-d',CLONE,'--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
  if z.returncode:
   text=z.stderr.decode('utf-8','replace').lower()
   out['restore_failure_categories']=[label for marker,label in [('already exists','ALREADY_EXISTS'),('terminating connection','STARTUP_RACE'),('connection to server','CONNECTIVITY'),('extension','EXTENSION'),('shared_preload','PRELOAD_REQUIRED'),('unrecognized configuration parameter','PARAMETER_VERSION'),('permission denied','PERMISSION'),('role','ROLE'),('schema "public" already exists','PUBLIC_SCHEMA_EXISTS'),('unsupported version','DUMP_VERSION'),('out of shared memory','SHARED_MEMORY')] if marker in text]
  assert z.returncode==0
 out['stage']='compare_restored_clone'
 assert before==baseline(TARGET)
 out['stage']='standard_003030_up'
 call(['docker','run','--rm','--network','container:'+TARGET,'--log-driver','none','--mount','type=bind,source='+str(B/'source/migrations/versioned')+',target=/migration,readonly','--entrypoint','migrate',APPIMAGE,'-path','/migration','-database','postgres://verified_mock:synthetic-only@127.0.0.1:5432/'+CLONE+'?sslmode=disable','up','1'],timeout=120)
 out['stage']='verify_post_migration'
 assert query(TARGET,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3030|false'
 assert baseline(TARGET)==before==baseline(SOURCE)
 assert query(SOURCE,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3029|false'
 for table in ('source_id_link_decisions','source_id_local_link_policies','source_id_link_blocks'):assert query(TARGET,'SELECT COUNT(*) FROM '+table)=='0'
 assert query(TARGET,'SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready')=='0'
 assert query(TARGET,'SELECT enabled FROM source_id_link_policy_state WHERE id=1')=='t'
 migrate=['docker','run','--rm','--network','container:'+TARGET,'--log-driver','none','--mount','type=bind,source='+str(B/'source/migrations/versioned')+',target=/migration,readonly','--entrypoint','migrate',APPIMAGE,'-path','/migration','-database','postgres://verified_mock:synthetic-only@127.0.0.1:5432/'+CLONE+'?sslmode=disable']
 call(migrate+['down','1']);assert query(TARGET,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3029|false';assert query(TARGET,'SELECT enabled FROM source_id_link_policy_state WHERE id=1')=='f'
 assert baseline(TARGET)==before
 call(migrate+['up','1']);assert query(TARGET,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3030|false';assert query(TARGET,'SELECT enabled FROM source_id_link_policy_state WHERE id=1')=='t'
 assert baseline(TARGET)==before
 out['retaining_down_up_pass']=True
 out.update(result='PASS',target_schema=3030,target_dirty=False,source_schema=3029,source_dirty=False,original_user_roles_memberships_kb_document_metadata_identical=True,acl_and_embedding_reference_metadata_identical=True,source_unchanged=True,counts={'users':len(before['users']),'knowledge_bases':len(before['knowledge_bases']),'documents':len(before['knowledges']),'chunks':before['chunks'],'nonempty_chunks':before['nonempty_chunks'],'embeddings':before['embeddings']},new_policy_tables_empty=True,invalid_indexes=0,network='none',files_mounted=False,real_identity_writes=False,protected_dump_retained=True)
except Exception:out.update(result='FAIL',failure='CONTROLLED_ISOLATED_MIGRATION_FAILURE')
finally:
 if owned:subprocess.run(['docker','rm','-f',TARGET],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=30)
 out['disposable_database_removed']=owned;out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 (B/'migration-result.json').write_text(json.dumps(out))
print(json.dumps(out))
