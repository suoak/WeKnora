"""Real Portal publish/unpublish and dynamic KB discovery on synthetic data."""
from pathlib import Path
import json,subprocess,sys,urllib.request,urllib.error

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022')
instance=json.loads((base/'results/deploy/instance.json').read_text())
assert instance['project'].startswith('knowhub-v1-wiki-dynamic-')
out=base/'results/portal-dynamic';out.mkdir(exist_ok=False)
client=json.loads((base/'private/client.json').read_text());origin=instance['url']
compose=['docker','compose','--project-name',instance['project'],'--project-directory',str(base),'-f',str(base/'compose.yml')]
actions=[];checks={};key=None;rpc=0

def request(method,path,body=None,token=None):
 h={'Content-Type':'application/json','Accept':'application/json, text/event-stream'}
 if token:h['Authorization']='Bearer '+token
 req=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,headers=h,method=method)
 try:
  with urllib.request.urlopen(req,timeout=45)as r:status,raw=r.status,r.read().decode()
 except urllib.error.HTTPError as e:status,raw=e.code,e.read().decode()
 if raw.startswith(('event:','data:')):raw=next(x[5:]for x in raw.splitlines()if x.startswith('data:'))
 return status,json.loads(raw)if raw else None

def sql(q):
 p=subprocess.run(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_v1','-Atc',q],capture_output=True,timeout=30)
 actions.append({'operation':'synthetic fixture SQL','query':q,'exit_code':p.returncode})
 assert p.returncode==0,p.stderr.decode()
 return p.stdout.decode().strip()

def call(tool,args):
 global rpc
 rpc+=1;status,data=request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':rpc,'method':'tools/call','params':{'name':tool,'arguments':args}},key['token'])
 assert status==200,data
 actions.append({'operation':tool,'arguments':args,'http_status':status,'is_error':bool(data.get('error')or data.get('result',{}).get('isError'))})
 return data

def value(data):
 assert not data.get('error')and not data['result'].get('isError'),data
 return json.loads(data['result']['content'][0]['text'])

def denied(data):return bool(data.get('error')or data.get('result',{}).get('isError'))

try:
 # The earlier HTTP suite created this future private space, then revoked A's
 # membership. No member role is added here to simulate public access.
 assert sql("SELECT status FROM tenant_members WHERE user_id='A'AND tenant_id=1207")=='inactive'
 sql("INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by,updated_by)VALUES(1207,'draft','Future synthetic Portal','reviewer','reviewer')")
 status,options=request('GET','/api/v1/mcp-api-keys/scope-options',token=client['login']['token']);assert status==200
 scopes=[{'tenant_id':v['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for v in options['data']]
 status,r=request('POST','/api/v1/mcp-api-keys',{'name':'Portal dynamic publication probe','personal_scope_mode':'dynamic_all','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes},client['login']['token']);assert status==201
 key=r['data']
 assert denied(call('read_document',{'knowledge_id':'future-private-doc'}))
 checks['unpublished_new_space_not_public']='PASS'
 status,_=request('POST','/api/v1/system/admin/portal/spaces/1207/publish',token=client['login']['token']);assert status==403
 status,_=request('POST','/api/v1/system/admin/portal/spaces/1207/publish',token=client['admin_jwt']);assert status==200
 assert 'future-private'in {v['id']for v in value(call('list_knowledge_bases',{}))}
 assert 'FUTURE_PRIVATE_CANARY'in json.dumps(value(call('read_document',{'knowledge_id':'future-private-doc'})))
 checks['same_key_discovers_new_published_space_without_membership']='PASS'
 sql("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy)SELECT 'portal-new-kb','New KB after Portal publication',tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy FROM knowledge_bases WHERE id='future-private'")
 assert 'portal-new-kb'in {v['id']for v in value(call('list_knowledge_bases',{}))}
 checks['published_space_new_empty_KB_automatically_discovered']='PASS'
 sql("INSERT INTO knowledges SELECT(jsonb_populate_record(NULL::knowledges,to_jsonb(k)||jsonb_build_object('id','portal-new-doc','knowledge_base_id','portal-new-kb','title','PORTALNEWCANARY'))).* FROM knowledges k WHERE id='future-private-doc'")
 # seq_id is independently unique: cloning an indexed fixture must allocate a
 # new value as well as a new canonical chunk ID. This DB is test-owned.
 sql("INSERT INTO chunks SELECT(jsonb_populate_record(NULL::chunks,to_jsonb(c)||jsonb_build_object('id','portal-new-chunk','seq_id',nextval('chunks_seq_id_seq'),'knowledge_id','portal-new-doc','knowledge_base_id','portal-new-kb','content','PORTALNEWCANARY'))).* FROM chunks c WHERE id='future-private-chunk'")
 # Clone the known synthetic 1024 vector and all exact index metadata, with
 # fresh canonical IDs/content. BM25 is maintained by real PostgreSQL indexes.
 sql("INSERT INTO embeddings SELECT(jsonb_populate_record(NULL::embeddings,to_jsonb(e)||jsonb_build_object('id',nextval(pg_get_serial_sequence('embeddings','id')),'source_id','portal-new-chunk','chunk_id','portal-new-chunk','knowledge_id','portal-new-doc','knowledge_base_id','portal-new-kb','content','PORTALNEWCANARY'))).* FROM embeddings e WHERE knowledge_id='future-private-doc'")
 for mode in ['keyword','semantic','hybrid']:
  result=value(call('search_knowledge',{'query':'PORTALNEWCANARY','mode':mode,'knowledge_base_ids':['portal-new-kb'],'limit':10}))
  assert 'portal-new-doc'in {v['knowledge_id']for v in result['results']}
 checks['new_indexed_document_keyword_vector_Hybrid']='PASS'
 status,_=request('POST','/api/v1/system/admin/portal/spaces/1207/unpublish',token=client['admin_jwt']);assert status==200
 assert denied(call('read_document',{'knowledge_id':'portal-new-doc'}))
 remaining={v['id']for v in value(call('list_knowledge_bases',{}))}
 assert {'private-home','private-other'}<=remaining and 'portal-new-kb'not in remaining and 'future-private'not in remaining
 checks['Portal_withdrawal_revokes_public_not_independent_private']='PASS'
 assert sql("SELECT status FROM tenant_members WHERE user_id='A'AND tenant_id=1207")=='inactive'
 assert sql("SELECT count(*) FROM kb_publication_policies WHERE knowledge_base_id IN('portal-new-kb','future-private')")=='0'
 checks['no_membership_or_duplicate_approval_created']='PASS'
 result={'exit_code':0,'candidate_sha256':instance['candidate_sha256'],'checks':checks,'synthetic_only':True,'indexing_scope':'real PG indexes with controlled 1024 fixture vector, not real document parser/cloud model'}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if key:
  status,_=request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=client['login']['token']);actions.append({'operation':'revoke own probe key','http_status':status})
 (out/'actions.json').write_text(json.dumps(actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
