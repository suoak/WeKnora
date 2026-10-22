"""Actual 80/8080 P3 APP business/security acceptance, synthetic data only."""
from pathlib import Path
import hashlib,importlib.util,json,os,sys
base=Path(sys.argv[1]).resolve();cfg=json.loads((base/'formal-runtime-current.json').read_text());p=Path(cfg['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==cfg['sha256']
spec=importlib.util.spec_from_file_location('formal_runtime',p);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);r=module.Runtime(base)
out=base/'results/business';out.mkdir(exist_ok=False);checks={};keys=[]
try:
 A=r.login('A');B=r.login('B');admin=r.login('reviewer');owner=r.login('owner')
 assert r.request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':1,'method':'tools/list'})[0]==401
 key=r.create_key(A,'Formal dynamic employee');keys.append((A,key));fixed=r.create_key(A,'Formal historical fixed control',None);keys.append((A,fixed));bk=r.create_key(B,'Formal second employee');keys.append((B,bk))
 token=key['token'];expected={'fm-public-kb','fm-public-two','fm-private-home','fm-private-other'}
 status,tools=r.request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':99,'method':'tools/list'},token=token)
 names={v['name']for v in tools['result']['tools']};assert status==200 and names=={'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document','wiki_search','wiki_read_page','wiki_index'}
 assert {v['id']for v in r.value(r.call(token,'list_knowledge_bases'))}==expected
 for mode in ['keyword','semantic','hybrid']:
  data=r.value(r.call(token,'search_knowledge',{'query':'PUBLIC','mode':mode,'limit':30}));assert data['status']=='complete'and {x['knowledge_base_id']for x in data['results']}==expected
  assert 'FORMAL_URL_SECRET'not in json.dumps(data)
 r.value(r.call(token,'grep_chunks',{'query':'PUBLIC','limit':30}));r.value(r.call(token,'list_documents',{'knowledge_base_id':'fm-public-kb'}))
 body=json.dumps(r.value(r.call(token,'read_document',{'knowledge_id':'fm-public-kb-doc'})));assert 'ordinary Office'in body and 'FORMAL_URL_SECRET'not in body
 for tool,args in [('read_document',{'knowledge_id':'fm-sensitive-doc'}),('read_document',{'knowledge_id':'fm-unpublished-doc'}),('read_document',{'knowledge_id':'nonexistent'}),('add_document',{}),('delete_document',{}),('ask',{})]:assert r.denied(r.call(token,tool,args))
 checks['eight_readonly_tools_public_private_union_and_denials']='PASS'
 # No employee membership in either Portal-public space.
 assert r.sql("SELECT count(*)FROM tenant_members WHERE user_id='fm-A'AND tenant_id IN(820002,820099)")=='0'
 checks['public_without_membership_or_duplicate_KB_approval']='PASS'
 wiki=r.value(r.call(token,'wiki_search',{'query':'verified'}));assert {x['knowledge_base_id']for x in wiki['pages']}=={'fm-public-two','fm-private-home'}and'FORMAL_UNTRUSTED_WIKI_CANARY'not in json.dumps(wiki)
 ambiguity=r.value(r.call(token,'wiki_read_page',{'slug':'concept/verified'}));assert ambiguity['status']=='ambiguous'and all(not v.get('content')for v in ambiguity['pages'])
 r.value(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-public-two'}))
 before=r.sql("SELECT count(*)FROM wiki_pages WHERE knowledge_base_id='fm-public-two'AND page_type='index'")
 r.value(r.call(token,'wiki_index',{'knowledge_base_id':'fm-public-two'}));assert before=='0'and r.sql("SELECT count(*)FROM wiki_pages WHERE knowledge_base_id='fm-public-two'AND page_type='index'")=='0'
 assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/unknown','knowledge_base_id':'fm-public-two'}))
 checks['Wiki_all_sources_slug_ambiguity_and_no_index_writes']='PASS'
 r.future()
 for t in [token,fixed['token'],bk['token']]:assert r.denied(r.call(t,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 r.sql("INSERT INTO tenant_members(user_id,tenant_id,role,status)VALUES('fm-A',820207,'viewer','active')")
 assert 'fm-future-private'in {v['id']for v in r.value(r.call(token,'list_knowledge_bases'))}
 for mode in ['keyword','semantic','hybrid']:
  hits=r.value(r.call(token,'search_knowledge',{'query':'FUTURE_PRIVATE_CANARY','mode':mode,'limit':30}));assert 'fm-future-private-doc'in {v['knowledge_id']for v in hits['results']}
 for t in [fixed['token'],bk['token']]:assert r.denied(r.call(t,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 r.sql("UPDATE tenant_members SET status='inactive'WHERE user_id='fm-A'AND tenant_id=820207")
 assert r.denied(r.call(token,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 checks['dynamic_future_private_join_and_revoke_fixed_and_other_user_unchanged']='PASS'
 # First public appearance uses existing Portal management, never M2 approval.
 r.sql("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by)VALUES(820207,'draft','Formal future Portal','fm-reviewer','fm-reviewer')")
 assert r.request('POST','/api/v1/system/admin/portal/spaces/820207/publish',token=A['token'])[0]==403
 assert r.request('POST','/api/v1/system/admin/portal/spaces/820207/publish',token=admin['token'])[0]==200
 assert 'fm-future-private'in {v['id']for v in r.value(r.call(token,'list_knowledge_bases'))}
 r.value(r.call(token,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 assert r.request('POST','/api/v1/system/admin/portal/spaces/820207/unpublish',token=admin['token'])[0]==200
 assert r.denied(r.call(token,'read_document',{'knowledge_id':'fm-future-private-doc'}))
 assert {'fm-private-home','fm-private-other'}<={v['id']for v in r.value(r.call(token,'list_knowledge_bases'))}
 assert r.sql("SELECT count(*)FROM kb_publication_policies WHERE knowledge_base_id='fm-future-private'")=='0'
 checks['Portal_new_publication_dynamic_discovery_withdrawal_preserves_private']='PASS'
 # Stored ciphertext and owner-only reveal are checked through actual APIs.
 encrypted=r.sql("SELECT api_key LIKE 'enc:v1:%'FROM tenant_api_keys WHERE id="+str(key['id']));assert encrypted=='t'
 status,reveal=r.request('POST','/api/v1/mcp-api-keys/'+str(key['id'])+'/reveal',{},A['token']);assert status==200 and reveal['data']['token']==token
 assert r.request('POST','/api/v1/mcp-api-keys/'+str(key['id'])+'/reveal',{},B['token'])[0]in(403,404)
 status,rotated=r.request('POST','/api/v1/mcp-api-keys/'+str(key['id'])+'/rotate',{},A['token']);assert status==200
 assert r.call(token,'list_knowledge_bases')[0]==401;token=rotated['data']['token'];r.value(r.call(token,'list_knowledge_bases'))
 assert r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=A['token'])[0]==200
 assert r.call(token,'list_knowledge_bases')[0]==401
 checks['infinite_expiry_encrypted_owner_reveal_rotate_revoke']='PASS'
 material={'candidate_sha256':r.sha,'url':'http://172.30.232.42','login':A,'owner_jwt':owner['token'],'admin_jwt':admin['token'],'private_directory':str(r.private),'synthetic_account':True}
 (r.private/'client.json').write_text(json.dumps(material));os.chmod(r.private/'client.json',0o600)
 result={'exit_code':0,'candidate_sha256':r.sha,'checks':checks,'actual_shared_APP_and_80_entry':True,'synthetic_only':True,'real_SID':'NOT_REPEATED','formal_model':'BLOCKED_CREDENTIAL_APPROVAL','cloud_Feishu':'BLOCKED_TEST_APPLICATION_APPROVAL'}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 # Revoke only keys created by this exact acceptance script; no old keys.
 for login,key in keys:r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=login['token'])
 (out/'http-actions.json').write_text(json.dumps(r.actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
