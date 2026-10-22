"""Actual shared-entry identity/capability boundaries; own synthetic Keys only."""
from pathlib import Path
import hashlib,importlib.util,json,sys,urllib.request,urllib.error
base=Path(sys.argv[1]).resolve();h=json.loads((base/'formal-runtime-current.json').read_text());p=Path(h['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==h['sha256'];s=importlib.util.spec_from_file_location('r',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);r=m.Runtime(base)
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert attempt in('initial','a2');out=base/'results'/('auth-edges'if attempt=='initial'else'auth-edges-'+attempt);out.mkdir(exist_ok=False);checks={};cleanup=[];disabled=False
def auth_me(token):
 q=urllib.request.Request(r.origin+'/api/v1/auth/me',headers={'X-API-Key':token,'X-Tenant-ID':'820198'})
 try:
  with urllib.request.urlopen(q,timeout=20)as s:return s.status
 except urllib.error.HTTPError as e:return e.code
try:
 A=r.login('A');B=r.login('B');owner=r.login('owner');admin=r.login('reviewer')
 # Genuine tenant and platform machine credentials are issued through their
 # existing management APIs, rather than testing arbitrary invalid strings.
 for route,login,body in [('/api/v1/tenants/820099/api-keys',owner,{'name':'Formal machine tenant control','full_access':True}),('/api/v1/system/admin/api-keys',admin,{'name':'Formal platform control','capabilities':['system_tenants_read']})]:
  status,d=r.request('POST',route,body,login['token'],820099 if'/tenants/'in route else None);assert status==201
  key=d['data'];cleanup.append((route+'/'+str(key['id']),login['token']))
  assert r.call(key['token'],'list_knowledge_bases')[0]in(401,403)
  q=urllib.request.Request(r.origin+'/api/v1/mcp/personal',method='POST',data=json.dumps({'jsonrpc':'2.0','id':9,'method':'tools/list'}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+key['token'],'X-User-ID':'fm-A','X-Owner-User-ID':'fm-A','X-Tenant-ID':'820001','X-From-MCP':'false'})
  try:
   with urllib.request.urlopen(q,timeout=20)as response:spoof_status=response.status
  except urllib.error.HTTPError as e:spoof_status=e.code
  assert spoof_status in(401,403),'machine identity spoof accepted'
 checks['real_tenant_platform_keys_and_forged_identity_headers_denied']='PASS'
 status,opts=r.request('GET','/api/v1/mcp-api-keys/scope-options',token=A['token']);assert status==200
 scopes=[{'tenant_id':v['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for v in opts['data']]
 status,d=r.request('POST','/api/v1/mcp-api-keys',{'name':'Formal retrieve-only legacy control','client_type':'generic','capabilities':['retrieve'],'tenant_scopes':scopes},A['token']);assert status==201
 old=d['data'];cleanup.append(('/api/v1/mcp-api-keys/'+str(old['id']),A['token']))
 assert old['personal_scope_mode']=='fixed'and old['capabilities']==['retrieve']
 ids={v['id']for v in r.value(r.call(old['token'],'list_knowledge_bases'))};assert ids=={'fm-private-home','fm-private-other'}
 assert r.denied(r.call(old['token'],'read_document',{'knowledge_id':'fm-public-two-doc'}))
 checks['legacy_retrieve_only_has_private_scope_without_public_expansion']='PASS'
 key=r.create_key(B,'Formal active account control');cleanup.append(('/api/v1/mcp-api-keys/'+str(key['id']),B['token']))
 r.value(r.call(key['token'],'list_knowledge_bases'));assert auth_me(key['token'])==200
 r.sql("UPDATE users SET is_active=false WHERE id='fm-B'");disabled=True
 assert r.call(key['token'],'list_knowledge_bases')[0]==401
 r.sql("UPDATE users SET is_active=true WHERE id='fm-B'");disabled=False
 r.value(r.call(key['token'],'list_knowledge_bases'))
 assert r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=B['token'])[0]==200
 assert r.call(key['token'],'list_knowledge_bases')[0]==401
 assert auth_me(key['token'])==401
 checks['account_disable_and_Key_revoke_revalidated_auth_me_denied']='PASS'
 result={'exit_code':0,'candidate_sha256':r.sha,'checks':checks,'created_and_modified_only_synthetic_subjects':True,'production_accessed':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if disabled:r.sql("UPDATE users SET is_active=true WHERE id='fm-B'")
 for path,jwt in cleanup:r.request('DELETE',path,token=jwt,tenant=820099 if'/tenants/'in path else None)
 (out/'http-actions.json').write_text(json.dumps(r.actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
