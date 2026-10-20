"""Exercise real business vector recall with existing vector, no model/PII export."""
import importlib.util
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
fixture=m.BASE/'private/mock'
baseline=json.loads((fixture/'acceptance-baseline.json').read_text())
vector=json.loads(m.sql("SELECT embedding::text FROM embeddings WHERE knowledge_base_id='"+baseline['knowledge_base_id']+"' AND embedding IS NOT NULL LIMIT 1"))
assert vector and len(vector)<10000
prefix=(fixture/'legacy-clone-http-acceptance.py').read_text().split("stage='start';out=")[0]
body=r'''
out={'result':'FAIL','model_requests':0,'query_source':'existing stored vector; loopback only','document_text_exported':False,'embedding_exported':False}
try:
    baseline=json.loads((ROOT/'acceptance-baseline.json').read_text())
    admin=login('admin');token=admin['token']
    if baseline['knowledge_tenant_id']!=admin.get('tenant',{}).get('id'):
        st,b,_=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':baseline['knowledge_tenant_id'],'refresh_token':admin.get('refresh_token','')},token=token);assert st==200 and b['success'];token=b['token']
    status,b,_=req('/api/v1/knowledge-bases/'+baseline['knowledge_base_id']+'/hybrid-search','POST',{'query_embedding':VECTOR,'disable_keywords_match':True,'disable_vector_match':False,'match_count':3,'vector_threshold':0,'skip_context_enrichment':True},token=token)
    out['http_status']=status
    hits=b.get('data') or []
    out['hit_count']=len(hits) if isinstance(hits,list) else 0
    out['success_field']=b.get('success') is True
    assert status==200 and b.get('success') is True and len(hits)>0
    assert all(h.get('knowledge_id') in baseline['document_ids'] for h in hits)
    out['hits_reference_original_documents']=True
    assert req('/api/v1/auth/logout','POST',token=token)[0]==200
    out['logout_invalidates_access']=req('/api/v1/auth/me',token=token)[0]==401
    out['result']='PASS'
except Exception as e:
    out['failure_class']=type(e).__name__;out['failure_lines']=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>']
print(json.dumps(out))
'''
code=prefix+'\nVECTOR='+repr(vector)+'\n'+body
p=subprocess.run(['docker','exec','-i','oa-legacy79-app-20261016','python3','-'],input=code.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=70)
assert p.returncode==0
out=json.loads(p.stdout)
(m.BASE/'retrieval-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
