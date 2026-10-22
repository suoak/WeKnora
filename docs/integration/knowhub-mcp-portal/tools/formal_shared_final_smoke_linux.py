"""Recheck actual revised-image APP boundaries without replaying fixture resets."""
from pathlib import Path
import hashlib,importlib.util,json,sys,time,urllib.request,urllib.error,urllib.parse,hmac
base=Path(sys.argv[1]).resolve();h=json.loads((base/'formal-runtime-current.json').read_text());p=Path(h['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==h['sha256'];s=importlib.util.spec_from_file_location('r',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);r=m.Runtime(base)
out=base/'results/final-smoke';out.mkdir(exist_ok=False);checks={};key=None
try:
 A=r.login('A');key=r.create_key(A,'Final revised candidate smoke');token=key['token']
 assert r.sha==json.loads((base/'results/promote/result.json').read_text())['candidate_sha256']
 status,tools=r.request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':500,'method':'tools/list'},token=token)
 assert status==200 and {x['name']for x in tools['result']['tools']}=={'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document','wiki_search','wiki_read_page','wiki_index'}
 ids={x['id']for x in r.value(r.call(token,'list_knowledge_bases'))};assert {'fm-public-kb','fm-public-two','fm-private-home','fm-private-other'}<=ids
 assert not {'fm-sensitive','fm-unpublished','fm-future-private','fm-after-publish','fm-feishu-drive-a2'}&ids
 for mode in('keyword','semantic','hybrid'):
  v=r.value(r.call(token,'search_knowledge',{'query':'PUBLIC','mode':mode,'limit':30}));assert v['status']=='partial';assert {'fm-public-kb','fm-public-two','fm-private-home','fm-private-other'}<={x['knowledge_base_id']for x in v['results']}
  assert 'FORMAL_URL_SECRET'not in json.dumps(v)
 r.value(r.call(token,'grep_chunks',{'query':'PUBLIC','limit':30}));r.value(r.call(token,'list_documents',{'knowledge_base_id':'fm-public-kb'}))
 assert 'ordinary Office'in json.dumps(r.value(r.call(token,'read_document',{'knowledge_id':'fm-public-kb-doc'})))
 r.value(r.call(token,'wiki_search',{'query':'verified'}));r.value(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-public-two'}))
 before=r.sql("SELECT count(*)FROM wiki_pages WHERE knowledge_base_id='fm-public-two'AND page_type='index'")
 r.value(r.call(token,'wiki_index',{'knowledge_base_id':'fm-public-two'}));assert r.sql("SELECT count(*)FROM wiki_pages WHERE knowledge_base_id='fm-public-two'AND page_type='index'")==before=='0'
 for tool,args in [('read_document',{'knowledge_id':'fm-sensitive-doc'}),('read_document',{'knowledge_id':'fm-unpublished-doc'}),('read_document',{'knowledge_id':'fm-legacy-drive-a2'}),('wiki_read_page',{'slug':'concept/unknown','knowledge_base_id':'fm-public-two'}),('add_document',{}),('delete_document',{}),('ask',{})]:assert r.denied(r.call(token,tool,args))
 source=json.loads((base/'feishu-source-current.json').read_text());ref=Path(source['revoked_file_reference']).read_text();assert int(urllib.parse.parse_qs(urllib.parse.urlsplit(ref).query)['expires'][0])>time.time()
 try:
  with urllib.request.urlopen(r.origin+ref,timeout=15)as response:code=response.status
 except urllib.error.HTTPError as e:code=e.code
 assert code in(403,404)
 owner=r.login('owner');assert r.request('GET','/api/v1/knowledge/fm-legacy-drive-a2',token=owner['token'],tenant=820099)[0]in(403,404)
 checks['same_source_APP_frontend_revised_candidate_actual_eight_readonly_tools']='PASS';checks['public_private_union_and_restricted_denials']='PASS';checks['partial_source_isolation_and_Wiki_no_write']='PASS';checks['persistent_REST_and_unexpired_file_revoke']='PASS'
 assert r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=A['token'])[0]==200;assert r.call(token,'list_knowledge_bases')[0]==401
 checks['Key_revoke_next_request']='PASS'
 material=json.loads((r.private/'client.json').read_text());material.update({'candidate_sha256':r.sha,'login':A});(r.private/'client.json').write_text(json.dumps(material))
 result={'exit_code':0,'candidate_sha256':r.sha,'checks':checks,'same_backend_Go_source_rebuilt_with_new_version_label':True,'synthetic_only':True,'production_accessed':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if key:r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=A['token'])
 (out/'http-actions.json').write_text(json.dumps(r.actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
