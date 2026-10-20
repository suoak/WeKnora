"""Safe final data/permission/index/config/container evidence, no identities."""
import os,stat,traceback
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');pg='knowhub-oa-sso-pilot-postgres-1';app='knowhub-oa-sso-pilot-app-1'
out={'result':'FAIL','real_sid_login':False,'real_identity_rows_imported':0,'default_changed':False}
try:
 assert json.loads((b/'validation-result.json').read_text())['result']=='PASS'
 assert json.loads((b/'rollback-result.json').read_text())['result']=='PASS'
 support=(p/'pilot-support.py').read_text().replace("PG='knowhub-oa-sso-pilot-prepare-pg'","PG='knowhub-oa-sso-pilot-postgres-1'");ns={};exec(compile(support,'private_support','exec'),ns)
 baseline=json.loads((b/'private/before-upgrade/integrity.json').read_text())
 original=json.loads((p/'private/selected-data-baseline.json').read_text())
 current=ns['signatures'](baseline['columns']);out['data_checks']={t:v==current[t] for t,v in baseline['signatures'].items()};assert all(out['data_checks'].values())
 out['source_original_unchanged']=ns['signatures'](original['columns'],source=True)==original['signatures'];assert out['source_original_unchanged']
 f=ns['files']('knowhub-oa-sso-pilot-files');out['file_checks']={'matches_before':f==baseline['files'],'matches_original':f==ns['files']('weknora_data-files'),'files':f['files'],'bytes':f['bytes']};assert out['file_checks']['matches_before'] and out['file_checks']['matches_original']
 out['schema']=ns['sql']("SELECT version::text||'|'||dirty::text FROM schema_migrations");assert out['schema']=='3028|false'
 out['source_schema']=ns['sql']("SELECT version::text||'|'||dirty::text FROM schema_migrations",source=True);assert out['source_schema']=='79|false'
 out['jit_table_present']=ns['sql']("SELECT to_regclass('source_id_jit_registrations') IS NOT NULL");assert out['jit_table_present']=='f'
 out['counts']=ns['counts']();assert out['counts']==baseline['counts']
 out['active_data']=ns['scalar']("SELECT json_build_object('active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'nonempty_active_chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embedding_rows',(SELECT COUNT(*) FROM embeddings),'resources',(SELECT COUNT(*) FROM resources WHERE deleted_at IS NULL))")
 assert out['active_data']['documents']==3 and out['active_data']['nonempty_active_chunks']==2717 and out['active_data']['embedding_rows']==2717
 out['reference_checks']=ns['scalar']("SELECT json_build_object('embeddings_missing_chunk',(SELECT COUNT(*) FROM embeddings e LEFT JOIN chunks c ON c.id=e.chunk_id WHERE c.id IS NULL),'embedding_document_mismatch',(SELECT COUNT(*) FROM embeddings e JOIN chunks c ON c.id=e.chunk_id WHERE e.knowledge_id IS DISTINCT FROM c.knowledge_id),'embedding_kb_mismatch',(SELECT COUNT(*) FROM embeddings e JOIN chunks c ON c.id=e.chunk_id WHERE e.knowledge_base_id IS DISTINCT FROM c.knowledge_base_id),'active_chunks_missing_document',(SELECT COUNT(*) FROM chunks c LEFT JOIN knowledges k ON k.id=c.knowledge_id WHERE c.deleted_at IS NULL AND c.content<>'' AND k.id IS NULL))");assert not any(out['reference_checks'].values())
 out['credential_and_identity_counts']=ns['scalar']("SELECT json_build_object('active_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'active_api_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'identities',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_local_sessions),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'ownership_audits',(SELECT COUNT(*) FROM source_id_ownership_audits),'enabled_connectors',(SELECT COUNT(*) FROM data_sources WHERE status<>'disabled'))");assert not any(out['credential_and_identity_counts'].values())
 out['indexes_valid']=ns['sql']('SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready')=='0';assert out['indexes_valid']
 out['ownership_constraints']=ns['scalar']("SELECT json_agg(json_build_object('type',contype,'definition',pg_get_constraintdef(oid))) FROM pg_constraint WHERE conrelid='source_id_account_ownership'::regclass")
 out['normalized_email_index_present']=ns['sql']("SELECT COUNT(*) FROM pg_indexes WHERE indexname='idx_sourceid_users_normalized_email'")=='1';assert out['normalized_email_index_present']
 out['flags']={};env=environment(app)
 allowed={'OIDC_AUTH_ENABLE':('false',),'SOURCEID_BASIC_ENCODING':('oauth_form',),'SOURCEID_PROTOCOL_ONLY':('true',),'SOURCEID_CONTRACT_CONFIRMED':('false',),'SOURCEID_EMAIL_BINDING_ENABLE':('false',),'SOURCEID_AUTO_BINDING_ENABLE':('false',),'SOURCEID_SSO_POLICY_CONFIRMED':('false',),'SOURCEID_SESSION_MODE':('external',),'SOURCEID_ADMISSION_MODE':('oidc_sso_approved',),'SOURCEID_LOCAL_REFRESH_ENABLE':('false',),'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':('false',),'SOURCEID_ACCOUNT_SCOPE':('NOT_SET','pilot'),'SOURCEID_ENTERPRISE_POLICY_CONFIRMED':('NOT_SET','false'),'SOURCEID_JIT_ENABLE':('NOT_SET','false'),'AUTO_MIGRATE':('false',)}
 for k,values in allowed.items():
  value=env.get(k,'NOT_SET');assert value in values;out['flags'][k]=value
 assert (p/'private/app.env').read_bytes()==(b/'private/before-upgrade/app.env').read_bytes();out['env_bytes_unchanged']=True
 out['app']=metadata(app);out['frontend']=metadata('knowhub-oa-sso-pilot-frontend-1')
 build=json.loads((b/'build-status.json').read_text());assert out['app']['image_id']==build['image_id']
 out['app_revision']=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',out['app']['image_id']]).decode().strip();assert out['app_revision']=='09e6d86db731953834916e58ac5f3f5985ad0aa1'
 out['frontend_revision']=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',out['frontend']['image_id']]).decode().strip();assert out['frontend_revision']=='832b6652adf541892545a7eac8b68733f846ab95'
 before=json.loads((b/'private/before-upgrade/other-container-metadata.json').read_text())
 out['unchanged_containers']={prefix:{name:metadata(prefix+'-'+name+'-1')['id']==v['id'] for name,v in services.items()} for prefix,services in before.items()};assert all(all(d.values()) for d in out['unchanged_containers'].values())
 z=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=b"BEGIN READ ONLY;SELECT version::text||'|'||dirty::text FROM schema_migrations;ROLLBACK;",stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert z.returncode==0
 out['default_schema']=z.stdout.decode().strip();assert out['default_schema']=='3027|false'
 out['app_no_host_port']=not inspect(app,'.HostConfig.PortBindings');out['pg_no_host_port']=not inspect(pg,'.HostConfig.PortBindings');assert out['app_no_host_port'] and out['pg_no_host_port']
 out['backup_private_permissions']=stat.S_IMODE((b/'private/before-upgrade').stat().st_mode)==0o700 and all(stat.S_IMODE((b/'private/before-upgrade'/name).stat().st_mode)==0o600 for name in ('app.env','postgres.env','database3027.dump','files.tar'));assert out['backup_private_permissions']
 leftovers=ns['sql']("SELECT COUNT(*) FROM pg_database WHERE datname IN ('enterprise_business_validation','enterprise_acl_validation','enterprise_mock_pilot','enterprise_mock_all','enterprise_scale_validation','enterprise_jit_validation')",db='postgres');assert leftovers=='0';out['disposable_databases_removed']=True
 out['result']='PASS'
except Exception as e:out.update(failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>'])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(b/'final-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
