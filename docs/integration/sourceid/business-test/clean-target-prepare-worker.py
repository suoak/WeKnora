"""Fresh source pair -> isolated clean candidate; no Mock identities injected."""
from support import *
import secrets,tarfile,traceback
def main():
    os.umask(0o077);private=BASE/'private';private.mkdir(mode=0o700)
    out={'result':'FAIL','target':'172.30.232.42','live_database_switched':False,'source_database_changed':False,'mock_identity_injected':False,'real_sid_login':False}
    stage='source_baseline'
    try:
        assert not inspect('839b9ed3ef0ba4c531a8c7b1331adcbd5881154c3f2a097171fde66300193d06','.State.Running')
        assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations",source=True)=='79|false'
        cols=columns();before=signatures(cols,source=True);n_before=counts(source=True);f_before=files('weknora_data-files')
        out['source_counts']=n_before
        out['source_model_credential_inventory']=scalar("SELECT json_build_object('rows',COUNT(*),'api_key_present',COUNT(*) FILTER (WHERE COALESCE(parameters::jsonb->>'api_key','')<>''),'encrypted_api_key',COUNT(*) FILTER (WHERE parameters::jsonb->>'api_key' LIKE 'enc:v1:%'),'app_secret_present',COUNT(*) FILTER (WHERE COALESCE(parameters::jsonb->>'app_secret','')<>'')) FROM models",source=True)
        stage='fresh_snapshot_pair';state(stage=stage,result='RUNNING')
        with (private/'source79-files.tar').open('wb') as f:execute(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','tar','--mount','type=volume,source=weknora_data-files,target=/scan,readonly',IMAGE,'-C','/scan','-cf','-','.'],output=f)
        with (private/'source79-clean.dump').open('wb') as f:execute(['docker','exec',SOURCE,'sh','-c','exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'],output=f)
        assert before==signatures(cols,source=True) and n_before==counts(source=True) and f_before==files('weknora_data-files')
        out['fresh_snapshot']={'generation':'20261016-clean-v1','database_bytes':(private/'source79-clean.dump').stat().st_size,'files':f_before['files'],'file_bytes':f_before['bytes'],'source_selected_data_stable':True,'source_files_stable':True,'database_transaction_consistent':True,'cross_system_write_freeze':False}
        stage='create_isolated_target';state(stage=stage,result='RUNNING')
        for vol in (PGVOL,FILEVOL):
            assert not execute(['docker','volume','ls','--filter','name=^'+vol+'$','--format','{{.Name}}']).strip()
            execute(['docker','volume','create','--label','knowhub.purpose=clean-legacy-target',vol])
        database_password=secrets.token_urlsafe(40)
        (private/'postgres.env').write_text('POSTGRES_USER=clean_owner\nPOSTGRES_DB='+DB+'\nPOSTGRES_PASSWORD='+database_password+'\n')
        execute(['docker','run','-d','--name',PG,'--network','none','--log-driver','none','--cpus','2','--memory','3g','--label','knowhub.purpose=clean-legacy-target','--env-file',str(private/'postgres.env'),'--mount','type=volume,source='+PGVOL+',target=/var/lib/postgresql/data',PGIMAGE])
        for _ in range(45):
            p=subprocess.run(['docker','exec',PG,'pg_isready','-h','127.0.0.1','-U','clean_owner'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            if p.returncode==0:break
            time.sleep(1)
        else:raise RuntimeError('candidate_pg_timeout')
        assert inspect(PG,'.HostConfig.NetworkMode')=='none' and not inspect(PG,'.HostConfig.PortBindings')
        sql('CREATE DATABASE '+REFERENCE,db='postgres')
        restore('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016/private/database79.dump',REFERENCE)
        reference_signatures=signatures(cols,REFERENCE)
        out['old_clean_snapshot_comparison']={t:v==reference_signatures[t] for t,v in before.items()}
        out['old_snapshot_comparison_scope']='selected account/content/ACL/vector/resource/model-identity columns; excludes credentials and runtime tables'
        # Compare prior clean file archive in a disposable directory, not the
        # prior rehearsal's modified database/files directory.
        prior=private/'previous-clean-files';prior.mkdir(mode=0o700)
        with tarfile.open('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016/private/files.tar') as archive:
            for member in archive.getmembers():
                (prior/member.name).resolve().relative_to(prior.resolve());assert not member.issym() and not member.islnk()
            archive.extractall(prior)
        code="import os,hashlib,json;h=hashlib.sha256();n=b=0\nfor root,dirs,names in os.walk('/scan'):\n dirs.sort()\n for name in sorted(names):\n  p=os.path.join(root,name);data=open(p,'rb').read();h.update(os.path.relpath(p,'/scan').encode());h.update(hashlib.sha256(data).digest());n+=1;b+=len(data)\nprint(json.dumps({'files':n,'bytes':b,'digest':h.hexdigest()}))"
        oldfiles=json.loads(execute(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3','--mount','type=bind,source='+str(prior)+',target=/scan,readonly',IMAGE,'-c',code]))
        out['old_clean_files_match_current_source']=oldfiles==f_before
        sql('DROP DATABASE '+REFERENCE,db='postgres')
        stage='restore_fresh_pair';state(stage=stage,result='RUNNING')
        restore(private/'source79-clean.dump',DB)
        assert signatures(cols)==before and counts()==n_before
        with (private/'source79-files.tar').open('rb') as f:
            p=subprocess.run(['docker','run','--rm','-i','--network','none','--log-driver','none','--entrypoint','tar','--mount','type=volume,source='+FILEVOL+',target=/restore',IMAGE,'-C','/restore','-xf','-'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30);assert p.returncode==0
        assert files(FILEVOL)==f_before
        stage='full_migration';state(stage=stage,result='RUNNING')
        execute(['docker','run','--rm','--network','container:'+PG,'--log-driver','none','--env-file',str(private/'postgres.env'),'--entrypoint','python3',IMAGE,'-c',"import os,subprocess,urllib.parse;u='postgres://clean_owner:'+urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe='')+'@127.0.0.1:5432/"+DB+"?sslmode=disable';p=subprocess.run(['migrate','-path','/app/migrations/versioned','-database',u,'up'],stdout=subprocess.PIPE,stderr=subprocess.PIPE);raise SystemExit(p.returncode)"],timeout=180)
        assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3027|false'
        assert counts()==n_before
        migrated=signatures(cols);out['original_selected_data_preserved']={t:before[t]==migrated[t] for t in before};assert all(out['original_selected_data_preserved'].values())
        out['schema']={'version':3027,'dirty':False,'full_up_chain':45}
        out['extensions']=scalar("SELECT json_agg(json_build_object('name',extname,'version',extversion)) FROM pg_extension WHERE extname IN ('pg_search','vector')")
        out['invalid_indexes']=int(sql('SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready'));assert out['invalid_indexes']==0
        out['embedding_references']=scalar("SELECT json_build_object('rows',COUNT(*),'missing_chunks',COUNT(*) FILTER (WHERE c.id IS NULL),'missing_documents',COUNT(*) FILTER (WHERE k.id IS NULL),'missing_kbs',COUNT(*) FILTER (WHERE b.id IS NULL)) FROM embeddings e LEFT JOIN chunks c ON c.id=e.chunk_id LEFT JOIN knowledges k ON k.id=e.knowledge_id LEFT JOIN knowledge_bases b ON b.id=e.knowledge_base_id")
        stage='invalidate_candidate_credentials';state(stage=stage,result='RUNNING')
        out['credential_counts_before']=scalar("SELECT json_build_object('auth_tokens',(SELECT COUNT(*) FROM auth_tokens),'unrevoked_auth_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'api_keys',(SELECT COUNT(*) FROM tenant_api_keys),'unrevoked_api_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'mcp_services',(SELECT COUNT(*) FROM mcp_services),'oauth_token_rows',(SELECT COUNT(*) FROM mcp_oauth_tokens),'data_sources',(SELECT COUNT(*) FROM data_sources))")
        sql("BEGIN; UPDATE auth_tokens SET is_revoked=TRUE; UPDATE users SET password_hash='!candidate-original-password-disabled'; UPDATE tenant_api_keys SET revoked_at=NOW(); UPDATE source_id_sessions SET revoked=TRUE; UPDATE mcp_endpoints SET enabled=FALSE,token_hash='',token_hint=''; UPDATE mcp_services SET enabled=FALSE,headers='{}',auth_config=NULL,env_vars='{}',stdio_config=NULL; UPDATE mcp_oauth_tokens SET access_token=NULL,refresh_token=NULL,expires_at=NOW(); UPDATE mcp_oauth_clients SET client_secret=NULL; UPDATE data_sources SET status='disabled',sync_schedule='',config='{}'; UPDATE models SET parameters=jsonb_build_object('embedding_parameters',parameters::jsonb->'embedding_parameters','interface_type',parameters::jsonb->'interface_type','provider',parameters::jsonb->'provider','base_url','http://127.0.0.1:9'); UPDATE tenants SET credentials=NULL,parser_engine_config=NULL; UPDATE storage_backends SET config='{}',status='disabled' WHERE provider<>'local'; DELETE FROM system_settings; COMMIT;")
        assert before==signatures(cols)
        out['credential_counts_after']=scalar("SELECT json_build_object('unrevoked_auth_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'unrevoked_api_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'enabled_mcp_services',(SELECT COUNT(*) FROM mcp_services WHERE enabled),'oauth_access_present',(SELECT COUNT(*) FROM mcp_oauth_tokens WHERE COALESCE(access_token,'')<>''),'enabled_mcp_endpoints',(SELECT COUNT(*) FROM mcp_endpoints WHERE enabled),'sourceid_identities',(SELECT COUNT(*) FROM external_identities),'sourceid_reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sourceid_sessions',(SELECT COUNT(*) FROM source_id_sessions))")
        assert not any(out['credential_counts_after'].values())
        env={'DB_DRIVER':'postgres','DB_HOST':'127.0.0.1','DB_PORT':'5432','DB_USER':'clean_owner','DB_PASSWORD':database_password,'DB_NAME':DB,'REDIS_ADDR':'127.0.0.1:6379','RETRIEVE_DRIVER':'postgres','AUTO_MIGRATE':'false','AUTO_RECOVER_DIRTY':'false','JWT_SECRET':secrets.token_urlsafe(48),'SYSTEM_AES_KEY':secrets.token_hex(16),'OIDC_AUTH_ENABLE':'false','SOURCEID_ENABLE':'true','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_EMAIL_CONTRACT_CONFIRMED':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_LOGOUT_ENABLE':'false','SOURCEID_DIAGNOSTICS_ENABLE':'false','SOURCEID_SESSION_MODE':'external','LANGFUSE_ENABLED':'false','LLM_DEBUG_LOG':'false','NEO4J_ENABLE':'false','GIN_MODE':'release','LOG_LEVEL':'error','LOG_PATH':'/tmp/clean-target-logs','STORAGE_TYPE':'local','LOCAL_STORAGE_BASE_DIR':'/data/files','DISABLE_REGISTRATION':'true','WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL':'','WEKNORA_TENANT_ENABLE_RBAC':'true','WEKNORA_TENANT_ENABLE_CROSS_TENANT_ACCESS':'true','WEKNORA_SANDBOX_DOCKER_ENABLED':'false','HTTP_PROXY':'','HTTPS_PROXY':'','ALL_PROXY':'','NO_PROXY':'*'}
        (private/'candidate.env').write_text('\n'.join(k+'='+v for k,v in env.items())+'\n')
        with (private/'sanitized-3027-clean.dump').open('wb') as f:execute(['docker','exec',PG,'pg_dump','-U','clean_owner','-d',DB,'-Fc','--no-owner','--no-acl'],output=f)
        (private/'selected-data-baseline.json').write_text(json.dumps({'columns':cols,'signatures':before,'source_counts':n_before}))
        assert before==signatures(cols,source=True) and n_before==counts(source=True) and f_before==files('weknora_data-files')
        out['identifiers']={'database':DB,'postgres_container':PG,'postgres_volume':PGVOL,'file_volume':FILEVOL,'root':str(BASE)}
        out['result']='PASS'
    except Exception as e:
        out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
    out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(BASE/'preparation-result.json').write_text(json.dumps(out,indent=2)+'\n');state(stage=stage,**out);print(json.dumps(out))
if __name__=='__main__':main()
