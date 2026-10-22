"""Production allowlist aggregate audit. Run via pinned read-only SSH stdin.

No resource body, title, path, principal, credential, policy mutation or export.
Missing classification is unreviewed, never an inference of ordinary content.
"""
import datetime,json,subprocess

def query(statement):
    sql="BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; "+statement+"; ROLLBACK;"
    result=subprocess.run(['docker','exec','-i','-u','postgres','WeKnora-postgres','psql','-X','-w','-U','postgres','-d','weknora','-qAt','-v','ON_ERROR_STOP=1'],input=sql.encode(),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=25)
    if result.returncode:raise RuntimeError('aggregate_unavailable')
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
    result['migration']=query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations")
    print(json.dumps(result,indent=2))

if __name__=='__main__':
    try:main()
    except Exception as error:
        print(json.dumps({'status':'BLOCKED','error_class':type(error).__name__,'read_only':True,'production_changes':False}));raise SystemExit(1)
