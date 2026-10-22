"""Actual APP negatives on known synthetic Wiki pages; no cloud or production."""
from pathlib import Path
import json,subprocess,sys,urllib.request,urllib.error

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022')
instance=json.loads((base/'results/deploy/instance.json').read_text())
assert instance['project'].startswith('knowhub-v1-wiki-dynamic-')
out=base/'results/wiki-negatives';out.mkdir(exist_ok=False)
client=json.loads((base/'private/client.json').read_text())
origin=instance['url'];checks={};actions=[];rpc=0;key=None
compose=['docker','compose','--project-name',instance['project'],'--project-directory',str(base),'-f',str(base/'compose.yml')]

def request(method,path,body=None,token=None):
 h={'Content-Type':'application/json','Accept':'application/json, text/event-stream'}
 if token:h['Authorization']='Bearer '+token
 q=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,headers=h,method=method)
 try:
  with urllib.request.urlopen(q,timeout=45)as r:status,raw=r.status,r.read().decode()
 except urllib.error.HTTPError as e:status,raw=e.code,e.read().decode()
 if raw.startswith(('event:','data:')):raw=next(x[5:]for x in raw.splitlines()if x.startswith('data:'))
 return status,json.loads(raw)if raw else None

def sql(q):
 p=subprocess.run(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_v1','-Atc',q],capture_output=True,timeout=30)
 actions.append({'operation':'synthetic SQL old-writer simulation','query':q,'exit_code':p.returncode})
 assert p.returncode==0,p.stderr.decode()
 return p.stdout.decode().strip()

def call(tool,args):
 global rpc
 rpc+=1;status,data=request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':rpc,'method':'tools/call','params':{'name':tool,'arguments':args}},key['token'])
 actions.append({'operation':tool,'arguments':args,'http_status':status,'is_error':bool(data and(data.get('error')or data.get('result',{}).get('isError')))})
 assert status==200,data
 return data

def value(data):
 assert not data.get('error')and not data['result'].get('isError'),data
 return json.loads(data['result']['content'][0]['text'])

try:
 status,options=request('GET','/api/v1/mcp-api-keys/scope-options',token=client['login']['token']);assert status==200
 scopes=[{'tenant_id':x['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for x in options['data']]
 status,result=request('POST','/api/v1/mcp-api-keys',{'name':'Wiki old-writer and archive probe','personal_scope_mode':'dynamic_all','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes},client['login']['token']);assert status==201
 key=result['data']
 args={'slug':'concept/verified','knowledge_base_id':'private-home'}
 before=value(call('wiki_read_page',args));assert 'ordinary content'in before['pages'][0]['content']
 old=sql("SELECT content FROM wiki_pages WHERE id='wiki-private-home'")
 sql("UPDATE wiki_pages SET status='archived' WHERE id='wiki-private-home'")
 denied=call('wiki_read_page',args);assert denied.get('error')or denied['result'].get('isError')
 checks['archived_page_denied']='PASS'
 sql("UPDATE wiki_pages SET status='published',content='RAW_OLD_WRITER_CANARY' WHERE id='wiki-private-home'")
 result=value(call('wiki_search',{'query':'RAW_OLD_WRITER_CANARY'}))
 assert not result['pages']and result['status']=='partial'
 checks['raw_writer_unsealed_body_hidden']='PASS'
 # Restoring exact bytes must not restore a trigger-invalidated proof.
 sql("UPDATE wiki_pages SET content='"+old.replace("'","''")+"' WHERE id='wiki-private-home'")
 denied=call('wiki_read_page',args);assert denied.get('error')or denied['result'].get('isError')
 checks['old_writer_ABA_cannot_reapprove']='PASS'
 for query in ['UNTRUSTED_WIKI_CANARY']:
  result=value(call('wiki_search',{'query':query,'knowledge_base_ids':['public-two']}));assert not result['pages']and result['status']=='partial'
 checks['mixed_foreign_source_hidden']='PASS'
 sql("INSERT INTO wiki_pages(id,tenant_id,knowledge_base_id,slug,title,content,page_type,status)VALUES('no-provenance',1,'public-two','concept/no-proof','NO_REF_CANARY','NO_REF_CANARY','concept','published')")
 result=value(call('wiki_search',{'query':'NO_REF_CANARY','knowledge_base_ids':['public-two']}));assert not result['pages']and result['status']=='partial'
 denied=call('wiki_read_page',{'slug':'concept/no-proof','knowledge_base_id':'public-two'});assert denied.get('error')or denied['result'].get('isError')
 checks['missing_refs_and_proof_denied']='PASS'
 healthy=value(call('wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'public-two'}))
 assert 'ordinary content'in healthy['pages'][0]['content']
 checks['unrelated_healthy_page_still_readable']='PASS'
 result={'exit_code':0,'candidate_sha256':instance['candidate_sha256'],'checks':checks,'synthetic_only':True,'real_APP':True,'real_model_generation':'NOT_TESTED'}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if key:
  status,_=request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=client['login']['token'])
  actions.append({'operation':'revoke own probe key','http_status':status})
 (out/'actions.json').write_text(json.dumps(actions,indent=2))
 (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
