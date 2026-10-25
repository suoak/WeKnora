"""Bounded live HTTP/private-ACL probe using synthetic data and the test gateway.

Creates two isolated KnowHub users in the new test database. It never fabricates
Feishu OAuth grants, accesses real Feishu documents, or claims real-user acceptance.
"""
import argparse
import base64
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import json,os,pathlib,re,secrets,subprocess,time,urllib.request,urllib.error
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':raise ValueError('unexpected_build_root')
release=root/'isolated-site';proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+BASE_SHA[:12]
if proof['source_sha']!=BASE_SHA or proof['project']!=project:raise ValueError('project_identity_mismatch')
if SHA!=BASE_SHA:
 upgrade=json.loads((release/('upgrade-'+SHA[:12])/'state.json').read_text())
 if upgrade.get('source_sha')!=SHA or upgrade.get('phase')!='RUNNING':raise ValueError('running_upgrade_required')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
def api(method,path,body=None,token=None):
 if not path.startswith('/api/v1/'):raise ValueError('unexpected_probe_target')
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 req=urllib.request.Request('http://127.0.0.1:28480'+path,method=method,headers=headers,
  data=json.dumps(body).encode() if body is not None else None)
 try:
  with opener.open(req,timeout=45) as response:
   raw=response.read(2*1024*1024+1)
   if len(raw)>2*1024*1024:raise ValueError('probe_response_limit')
   return response.status,json.loads(raw)
 except urllib.error.HTTPError as error:return error.code,{}
def unbox(value):return value.get('data',value) if isinstance(value,dict) else value
statefile=release/'private-http-probe-state.json'
if statefile.exists():state=json.loads(statefile.read_text())
else:state={'source_sha':BASE_SHA,'account_b':{'username':'feishu-v2-viewer','email':'feishu-v2-viewer@example.invalid',
 'password':secrets.token_urlsafe(18)+'aA!9'}}
if state['source_sha']!=BASE_SHA:raise ValueError('probe_state_identity_mismatch')
def save():
 temporary=release/('probe-state-'+secrets.token_hex(4)+'.json')
 fd=os.open(str(temporary),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as out:json.dump(state,out,indent=2)
 temporary.replace(statefile)
save()
account_a=json.loads((release/'bootstrap-user.json').read_text())
status,login_a=api('POST','/api/v1/auth/login',{'email':account_a['email'],'password':account_a['password']})
if status!=200:raise ValueError('probe_owner_login_failed')
token_a=login_a['token'];tenant=int(login_a['active_tenant']['id'])
account_b=state['account_b']
status,login_b=api('POST','/api/v1/auth/login',{'email':account_b['email'],'password':account_b['password']})
if status!=200:
 status,_=api('POST','/api/v1/auth/register',account_b)
 if status!=201:raise ValueError('probe_second_user_registration_failed')
 status,login_b=api('POST','/api/v1/auth/login',{'email':account_b['email'],'password':account_b['password']})
 if status!=200:raise ValueError('probe_second_user_login_failed')
token_b=login_b['token']
if not state.get('member_b'):
 status,_=api('POST','/api/v1/tenants/'+str(tenant)+'/members',{'email':account_b['email'],'role':'admin'},token_a)
 if status not in (200,201,409):raise ValueError('probe_same_tenant_membership_failed')
 state['member_b']=True;save()
status,switched=api('POST','/api/v1/auth/switch-tenant',{'tenant_id':tenant},token_b)
if status!=200 or not switched.get('token'):raise ValueError('probe_second_user_tenant_switch_failed')
token_b=switched['token']
if not state.get('private_kb'):
 status,value=api('POST','/api/v1/me/feishu/private-knowledge-base',{'name':'V2 synthetic private isolation'},token_a)
 if status!=201:raise ValueError('probe_private_kb_creation_failed')
 kb=unbox(value)
 if kb.get('embedding_model_id')!='builtin-feishu-v2-embedding':raise ValueError('probe_default_embedding_not_bound')
 state['private_kb']=kb['id'];save()
if not state.get('public_kb'):
 status,value=api('POST','/api/v1/knowledge-bases',{'name':'V2 synthetic visible control','type':'document'},token_a)
 if status!=201:raise ValueError('probe_visible_control_creation_failed')
 state['public_kb']=unbox(value)['id'];save()
kb_id=state['private_kb'];public_id=state['public_kb']
if not state.get('knowledge_id'):
 status,value=api('POST','/api/v1/knowledge-bases/'+kb_id+'/knowledge/manual',{
  'title':'V2 synthetic privacy canary','content':'# Synthetic test\n\nv2privacycanary isolated integration verification.',
  'status':'publish','process_config':{'summary_enabled':False}},token_a)
 if status!=200:raise ValueError('probe_manual_ingest_failed')
 state['knowledge_id']=unbox(value)['id'];save()
kid=state['knowledge_id']
checks=[]
for label,path in [('private_kb','/api/v1/knowledge-bases/'+kb_id),('private_document','/api/v1/knowledge/'+kid)]:
 status,_=api('GET',path,token=token_b)
 checks.append({'check':'same_tenant_admin_b_denied_'+label,'status':status})
 if status not in (403,404):raise ValueError('private_http_acl_expanded')
status,_=api('GET','/api/v1/knowledge-bases/'+public_id,token=token_b)
if status!=200:raise ValueError('visible_positive_control_not_readable')
checks.append({'check':'same_tenant_admin_b_can_read_public_control','status':status})
status,catalog=api('GET','/api/v1/knowledge-bases?page=1&page_size=100',token=token_b)
serialized=json.dumps(catalog)
if status!=200 or kb_id in serialized or public_id not in serialized:raise ValueError('private_kb_catalog_filter_failed')
checks.append({'check':'private_kb_hidden_from_same_tenant_catalog','status':200})
search={'query_text':'v2privacycanary','match_count':10,'vector_threshold':0,'keyword_threshold':0,
 'disable_keywords_match':False,'disable_vector_match':False}
status,_=api('POST','/api/v1/knowledge-bases/'+kb_id+'/hybrid-search',search,token_b)
if status not in (403,404):raise ValueError('private_search_scope_expanded')
checks.append({'check':'same_tenant_admin_b_denied_private_search','status':status})
status,value=api('GET','/api/v1/knowledge/'+kid,token=token_a)
if status!=200:raise ValueError('owner_document_not_readable')
knowledge=unbox(value)
if knowledge.get('parse_status')=='pending' and RETRY_SYNTHETIC and state.get('reparsed_for_sha')!=SHA:
 status,_=api('POST','/api/v1/knowledge/'+kid+'/reparse',{'process_config':{'summary_enabled':False}},token_a)
 if status!=200:raise ValueError('synthetic_owner_reparse_failed')
 state['reparsed_for_sha']=SHA;save()
if knowledge.get('parse_status')!='completed':
 print(json.dumps({'status':'PENDING','source_sha':SHA,'parse_status':knowledge.get('parse_status'),
  'checks':checks,'scope':'synthetic_private_http_only'}));raise SystemExit(0)
if not all(re.fullmatch(r'[a-f0-9-]{36}',value) for value in [kid,kb_id]):raise ValueError('unexpected_probe_ids')
statement=("SELECT count(*)||':'||count(*) FILTER(WHERE dimension=1024 AND embedding IS NOT NULL)||':'||"+
 "count(*) FILTER(WHERE content ||| 'v2privacycanary') FROM embeddings WHERE knowledge_id='"+kid+"' AND knowledge_base_id='"+kb_id+"';")
p=subprocess.run(['docker','compose','--project-name',project,'--file',str(release/'compose.json'),
 'exec','-T','postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At'],
 input=statement,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=30)
if p.returncode:raise ValueError('probe_index_verification_failed')
counts=[int(value) for value in p.stdout.strip().split(':')]
if len(counts)!=3 or min(counts)<1:raise ValueError('probe_real_vectors_or_bm25_missing')
status,hits=api('POST','/api/v1/knowledge-bases/'+kb_id+'/hybrid-search',search,token_a)
if status!=200 or kid not in json.dumps(hits):raise ValueError('owner_hybrid_search_did_not_find_synthetic_document')
checks.append({'check':'owner_hybrid_search_finds_synthetic_document','status':200})

def personal_key(label,jwt):
 existing=state.get('mcp_'+label)
 if existing and existing.get('expires',0)>time.time()+120:return existing['token']
 expires=int(time.time())+3600
 status,value=api('POST','/api/v1/mcp-api-keys',{'name':'V2 synthetic privacy probe '+label,
  'personal_scope_mode':'fixed','client_type':'generic','capabilities':['retrieve'],
  'expires_at_unix':expires,'tenant_scopes':[{'tenant_id':tenant,'kb_scope_mode':'all','knowledge_bases':[]}]},jwt)
 if status!=201:raise ValueError('synthetic_mcp_key_creation_failed')
 row=unbox(value)
 if not row.get('token'):raise ValueError('synthetic_mcp_token_missing')
 state['mcp_'+label]={'id':row['id'],'token':row['token'],'expires':expires};save()
 return row['token']

def rpc(token,name,arguments):
 request=urllib.request.Request('http://127.0.0.1:28480/api/v1/mcp/personal',method='POST',
  headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','Accept':'application/json, text/event-stream'},
  data=json.dumps({'jsonrpc':'2.0','id':1,'method':'tools/call','params':{'name':name,'arguments':arguments}}).encode())
 with opener.open(request,timeout=45) as response:
  raw=response.read(2*1024*1024+1)
  if len(raw)>2*1024*1024:raise ValueError('mcp_probe_response_limit')
  text=raw.decode()
  if text.lstrip().startswith('{'):result=json.loads(text)
  else:
   rows=[json.loads(line[5:].strip()) for line in text.splitlines() if line.startswith('data:')]
   if len(rows)!=1:raise ValueError('mcp_probe_stream_shape')
   result=rows[0]
 if result.get('error') or result.get('result',{}).get('isError'):raise ValueError('mcp_probe_tool_failed')
 return result
key_a=personal_key('a',token_a);key_b=personal_key('b',token_b)
catalog_a=json.dumps(rpc(key_a,'list_knowledge_bases',{}))
catalog_b=json.dumps(rpc(key_b,'list_knowledge_bases',{}))
if kb_id not in catalog_a or public_id not in catalog_b or kb_id in catalog_b:raise ValueError('personal_mcp_private_catalog_boundary_failed')
found=json.dumps(rpc(key_a,'search_knowledge',{'query':'v2privacycanary','mode':'hybrid','knowledge_base_ids':[kb_id],'limit':10}))
if kid not in found:raise ValueError('personal_mcp_owner_search_missing')
checks.append({'check':'personal_mcp_owner_hybrid_search_and_other_admin_catalog_isolation','status':'PASS'})
result={'status':'PASS','source_sha':SHA,'project':project,'scope':'synthetic_manual_ingest_gateway_and_private_http_acl',
 'checks':checks,'indexed_rows':counts[0],'vector_rows_1024':counts[1],'bm25_canary_rows':counts[2],
 'real_feishu_oauth':'NOT_EXECUTED','real_feishu_document_import':'NOT_EXECUTED',
 'real_cross_employee_feishu_acl':'NOT_TESTED','synthetic_go_personal_mcp':'PASS','business_mcp_search':'NOT_EXECUTED'}
(release/'private-http-probe-result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--manifest',type=Path,required=True)
    parser.add_argument('--candidate-manifest',type=Path)
    parser.add_argument('--retry-stalled-synthetic',action='store_true')
    args=parser.parse_args();raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(16384))
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'isolated_private_http_probe_failed'}))\n raise SystemExit(2)\n"
        expected=sha
        if args.candidate_manifest:
            payload=args.candidate_manifest.read_bytes()
            candidate=json.loads(payload.decode('utf-16') if payload.startswith((b'\xff\xfe',b'\xfe\xff')) else payload.decode('utf-8-sig'))
            expected=candidate['source_sha']
            if not re.fullmatch('[a-f0-9]{40}',expected):raise ValueError('invalid_candidate_sha')
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(expected)+'\nBASE_SHA='+repr(sha)+'\nRETRY_SYNTHETIC='+repr(args.retry_stalled_synthetic)+'\n'
        code=base64.b64encode((prefix+guarded).encode()).decode()
        _,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=240)
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 0 if result.get('status') in ('PASS','PENDING') else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_private_http_probe_unavailable'}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
