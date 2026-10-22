"""Published -> new KB -> indexed document on actual P3 entry, same Key."""
from pathlib import Path
import hashlib,importlib.util,json,sys
base=Path(sys.argv[1]).resolve();h=json.loads((base/'formal-runtime-current.json').read_text());p=Path(h['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==h['sha256'];s=importlib.util.spec_from_file_location('r',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);r=m.Runtime(base)
out=base/'results/portal-dynamic';out.mkdir(exist_ok=False);checks={};key=None
try:
 A=r.login('A');admin=r.login('reviewer');key=r.create_key(A,'Formal Portal live scope');token=key['token']
 assert r.denied(r.call(token,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 assert r.request('POST','/api/v1/system/admin/portal/spaces/820207/publish',token=admin['token'])[0]==200
 r.value(r.call(token,'read_document',{'knowledge_id':'fm-future-private-doc'}));checks['same_Key_new_Portal_space']='PASS'
 r.sql("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy)SELECT'fm-after-publish','Formal KB after publication',tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy FROM knowledge_bases WHERE id='fm-future-private'")
 assert 'fm-after-publish'in {x['id']for x in r.value(r.call(token,'list_knowledge_bases'))};checks['new_KB_automatic_discovery']='PASS'
 r.sql("INSERT INTO knowledges SELECT(jsonb_populate_record(NULL::knowledges,to_jsonb(k)||jsonb_build_object('id','fm-after-publish-doc','knowledge_base_id','fm-after-publish','title','AFTERPUBLISH_CANARY'))).* FROM knowledges k WHERE id='fm-future-private-doc'")
 r.sql("INSERT INTO chunks SELECT(jsonb_populate_record(NULL::chunks,to_jsonb(c)||jsonb_build_object('id','fm-after-publish-chunk','seq_id',nextval('chunks_seq_id_seq'),'knowledge_id','fm-after-publish-doc','knowledge_base_id','fm-after-publish','content','AFTERPUBLISH_CANARY'))).* FROM chunks c WHERE id='fm-future-private-chunk'")
 r.sql("INSERT INTO embeddings SELECT(jsonb_populate_record(NULL::embeddings,to_jsonb(e)||jsonb_build_object('id',nextval(pg_get_serial_sequence('embeddings','id')),'source_id','fm-after-publish-chunk','chunk_id','fm-after-publish-chunk','knowledge_id','fm-after-publish-doc','knowledge_base_id','fm-after-publish','content','AFTERPUBLISH_CANARY'))).* FROM embeddings e WHERE knowledge_id='fm-future-private-doc'")
 for mode in ['keyword','semantic','hybrid']:
  v=r.value(r.call(token,'search_knowledge',{'query':'AFTERPUBLISH_CANARY','mode':mode,'knowledge_base_ids':['fm-after-publish'],'limit':10}));assert 'fm-after-publish-doc'in {x['knowledge_id']for x in v['results']}
 checks['new_document_indexed_real_PG_three_modes']='PASS'
 assert r.request('POST','/api/v1/system/admin/portal/spaces/820207/unpublish',token=admin['token'])[0]==200
 assert r.denied(r.call(token,'read_document',{'knowledge_id':'fm-after-publish-doc'}));assert {'fm-private-home','fm-private-other'}<={x['id']for x in r.value(r.call(token,'list_knowledge_bases'))}
 assert r.sql("SELECT count(*)FROM kb_publication_policies WHERE knowledge_base_id IN('fm-future-private','fm-after-publish')")=='0'
 checks['withdrawal_preserves_private_no_approval_rows']='PASS'
 result={'exit_code':0,'candidate_sha256':r.sha,'checks':checks,'shared_entry':True,'synthetic_documents_and_1024_vector':True,'real_document_parser_or_formal_model':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if key:r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=A['token'])
 (out/'http-actions.json').write_text(json.dumps(r.actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
