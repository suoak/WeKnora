"""Production allowlist aggregate audit. Run via pinned read-only SSH stdin.

No resource body, title, path, principal, credential, policy mutation or export.
Missing classification is unreviewed, never an inference of ordinary content.
"""
import datetime,json,subprocess

QUERY_NUMBER=0
def query(statement):
    global QUERY_NUMBER
    QUERY_NUMBER+=1
    sql="BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; "+statement+"; ROLLBACK;"
    result=subprocess.run(['docker','exec','-i','-u','postgres','WeKnora-postgres','psql','-X','-w','-U','postgres','-d','weknora','-qAt','-v','ON_ERROR_STOP=1','-v','VERBOSITY=sqlstate'],input=sql.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25)
    if result.returncode:raise RuntimeError('query_'+str(QUERY_NUMBER)+'_'+result.stderr.decode().strip()[:100])
    return json.loads(result.stdout.decode())

def main():
    base="FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id AND t.deleted_at IS NULL AND t.status='active' JOIN knowledge_bases kb ON kb.tenant_id=t.id AND kb.deleted_at IS NULL WHERE pc.status='published'"
    counts=query("SELECT json_build_object('published_active_spaces',count(DISTINCT t.id),'live_kbs',count(DISTINCT kb.id),'temporary_kbs',count(DISTINCT kb.id) FILTER(WHERE kb.is_temporary),'live_documents',(SELECT count(*) FROM knowledges k WHERE k.deleted_at IS NULL AND EXISTS(SELECT 1 "+base+" AND kb.id=k.knowledge_base_id AND kb.tenant_id=k.tenant_id))) "+base)
    tables=query("SELECT COALESCE(json_agg(table_name),'[]'::json) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('enterprise_space_policies','kb_publication_policies','knowledge_access_policies','document_access_epochs')")
    result={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'read_only':True,'production_changes':False,'counts':counts,'classification_tables_present':tables,'content_inspected':False,'real_employee_provider_verified':False}
    result['kb_shape']=query("SELECT json_build_object('kb_over_256_documents',count(*) FILTER(WHERE documents>256),'largest_kb_documents',COALESCE(max(documents),0),'non_document_kbs',count(*) FILTER(WHERE type<>'document')) FROM (SELECT kb.id,kb.type,(SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL) AS documents "+base+") shape")
    required={'enterprise_space_policies','kb_publication_policies','knowledge_access_policies','document_access_epochs'}
    if not required.issubset(set(tables)):
        result['classification']={'reviewed_safe_public_kbs':0,'reviewed_safe_public_spaces':0,'unreviewed_or_restricted_kbs':counts['live_kbs'],'reason':'required reviewed/versioned classification unavailable; no content sensitivity inferred'}
    else:
        # Aggregates are candidates only: dynamic resource-version verification,
        # employee provider and file-containment gates cannot be inferred in SQL.
        aggregate=query("SELECT json_build_object('approved_ordinary_kb_policy_rows',count(*) FILTER(WHERE p.state='approved' AND p.policy='inherit' AND p.content_class='ordinary' AND p.source_acl_state='local'),'unreviewed_or_restricted_kbs',count(*) FILTER(WHERE p.state IS NULL OR p.state<>'approved' OR p.policy<>'inherit' OR p.content_class<>'ordinary' OR p.source_acl_state<>'local')) FROM (SELECT kb.id "+base+") eligible LEFT JOIN kb_publication_policies p ON p.knowledge_base_id=eligible.id")
        result['classification']=aggregate
        result['classification']['runtime_safe_public_kbs']='NOT_VERIFIED'
    result['first_confirmation_inventory']=query("SELECT COALESCE(json_agg(row_to_json(review)),'[]'::json) FROM (SELECT substr(md5('knowhub-b7-space:'||t.id::text),1,16) AS space_ref,substr(md5('knowhub-b7-kb:'||kb.id),1,16) AS kb_ref,kb.type,kb.is_temporary,(SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL) AS documents,(SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL AND k.parse_status='completed' AND k.enable_status='enabled') AS ready_documents,(SELECT COALESCE(json_object_agg(channel,n),'{}'::json) FROM (SELECT COALESCE(k.channel,'unknown') AS channel,count(*) AS n FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL GROUP BY channel) src) AS source_channels "+base+" ORDER BY t.id,kb.id) review")
    result['embedding_dimensions']=query("SELECT COALESCE(json_agg(row_to_json(shape)),'[]'::json) FROM (SELECT m.parameters->'embedding_parameters'->>'dimension' AS dimension,count(DISTINCT kb.id) AS kbs "+base.replace(" WHERE pc.status=", " LEFT JOIN models m ON m.id=kb.embedding_model_id AND m.deleted_at IS NULL WHERE pc.status=")+" GROUP BY m.parameters->'embedding_parameters'->>'dimension') shape")
    result['vector_index_dimensions']=query("SELECT COALESCE(json_agg(DISTINCT substring(indexdef from 'halfvec\\(([0-9]+)\\)')),'[]'::json) FROM pg_indexes WHERE schemaname='public' AND tablename='embeddings' AND indexdef ILIKE '%hnsw%'")
    result['classification_semantics']={'unapproved_is_not_confirmed_sensitive':True,'body_reviewed':False,'safe_to_publish':'NOT_APPROVED','employee_eligibility':'NOT_VERIFIED'}
    result['migration']=query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations")
    print(json.dumps(result,indent=2))

if __name__=='__main__':
    try:main()
    except Exception as error:
        print(json.dumps({'status':'BLOCKED','error_class':type(error).__name__,'diagnosis':str(error),'read_only':True,'production_changes':False}));raise SystemExit(1)
