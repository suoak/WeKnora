import pathlib
B=pathlib.Path('/home/aqyfzx/ai/knowhub-personal-upload-r1-20261112')
source=(B/'accept.py').read_text().split("check('schema3046-before'")[0].replace("'accept-results.json'","'supplement-results.json'")
exec(source)
# All changes below target only the uniquely named synthetic lab database.
for mode in ['keyword','semantic','hybrid']:
 r=call('search_knowledge',{'query':'KNOWHUB','mode':mode,'limit':20,'knowledge_base_ids':['upload-a-kb']});check('search-'+mode,not denied(r) and bool(decoded(r).get('results')),decoded(r) if not denied(r) else r)
raw=sql("SELECT json_agg(json_build_object('id',id,'type',file_type,'status',parse_status)) FROM knowledges")
rows=json.loads(raw);check('all-documents-completed',all(r['status']=='completed' for r in rows),rows)
check('chunk-vector-generated',int(sql('select count(*) from chunks'))>=len(rows) and int(sql('select count(*) from embeddings'))>=len(rows))
request={'knowledge_base_id':'upload-editor-kb','title':'revoked share','content':'Synthetic deny assertion','idempotency_key':'share-revocation-0001'}
sql("UPDATE kb_shares SET permission='viewer' WHERE id='upload-editor-share'")
try:check('editor-revoked',denied(call('add_document',request)))
finally:sql("UPDATE kb_shares SET permission='editor' WHERE id='upload-editor-share'")
sql("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=101")
try:check('key-revoked',denied(call('add_document',dict(request,knowledge_base_id='upload-a-kb'))))
finally:sql('UPDATE tenant_api_keys SET revoked_at=NULL WHERE id=101')
sql("UPDATE users SET is_active=false WHERE id='upload-A'")
try:check('account-disabled',denied(call('add_document',dict(request,knowledge_base_id='upload-a-kb'))))
finally:sql("UPDATE users SET is_active=true WHERE id='upload-A'")
check('upload-audit-identifiers',int(sql("select count(*) from audit_logs where action='mcp.file_added' and actor_user_id='upload-A' and scope_type='knowledge_base' and details->>'key_id'='101'"))>0)
check('schema-unchanged3046',sql('select version from schema_migrations')=='3046')
(B/'supplement-done.json').write_text(json.dumps({'pass':True,'antivirus_scanning':'deferred by user','employee_external_recheck':'deferred by user'}))
