"""Continue full migration after clean restore; never touches source DB."""
import importlib.util
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
before=m.counts();original=m.ownership(True)
assert original==m.ownership()
assert m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='79|false'
p=subprocess.run(['docker','run','--rm','--network','container:'+m.PG,'--log-driver','none','--entrypoint','migrate',m.IMAGE,'-path','/app/migrations/versioned','-database','postgres://clone@127.0.0.1:5432/clone?sslmode=disable','up'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=100)
out={'result':'FAIL' if p.returncode else 'PASS','migration':'79_to_3027_full_chain','clone_only':True}
if p.returncode:
    raw=(p.stdout+p.stderr).decode('utf-8','replace').lower()
    out['markers']=[w for w in ('extension','already exists','does not exist','constraint','syntax error','dirty','duplicate','timeout','null value') if w in raw]
    out['schema']=m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")
else:
    out['schema']=m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")
    assert out['schema']=='3027|false'
    out['original_role_acl_preserved']={k:original[k]==m.ownership()[k] for k in original}
    assert all(out['original_role_acl_preserved'].values())
    out['original_counts_preserved']=m.counts()==before;assert out['original_counts_preserved']
    out['extensions']=m.scalar("SELECT json_agg(json_build_object('name',extname,'version',extversion)) FROM pg_extension WHERE extname IN ('pg_search','vector')")
    out['invalid_indexes']=int(m.sql('SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready'))
    out['embedding_references']=m.scalar("SELECT json_build_object('rows',COUNT(*),'missing_chunks',COUNT(*) FILTER (WHERE c.id IS NULL),'missing_documents',COUNT(*) FILTER (WHERE k.id IS NULL),'missing_kbs',COUNT(*) FILTER (WHERE b.id IS NULL)) FROM embeddings e LEFT JOIN chunks c ON c.id=e.chunk_id LEFT JOIN knowledges k ON k.id=e.knowledge_id LEFT JOIN knowledge_bases b ON b.id=e.knowledge_base_id")
    out['credential_table_columns']=m.scalar("SELECT json_agg(json_build_object('table',table_name,'column',column_name)) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('tenants','models','mcp_api_keys','data_sources','datasources','auth_tokens') AND (column_name ~ '(key|token|secret|password|status|enabled|revoked|parameters)' OR column_name='is_active')")
(m.BASE/'migration-continue-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
