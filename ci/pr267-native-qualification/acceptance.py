"""Authenticated qualification against the new, owned full native APP only."""
import base64,datetime,hashlib,json,os,pathlib,secrets,socket,subprocess,time,urllib.request,urllib.error,uuid
ROOT=pathlib.Path(os.environ['QUALIFICATION_ROOT'])
PROJECT='knowhub-pr267-'+os.environ['GITHUB_RUN_ID'];SHA='e866d539adf3acaf52a7a1b5e81289f32df30613'
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ['SOURCE_SHA']==SHA
proof=json.loads((ROOT/'deployment-proof.json').read_text());assert proof['source_sha']==SHA and proof['project']==PROJECT
C=['docker','compose','-p',PROJECT,'-f',str(ROOT/'compose.json')];RESULTS=[];RUNID=datetime.datetime.now(datetime.timezone.utc).strftime('%H%M%S')+'-'+secrets.token_hex(3)
app_id=subprocess.check_output(C+['ps','-q','app'],text=True,stdin=subprocess.DEVNULL).strip()
app_inspect=json.loads(subprocess.check_output(['docker','inspect',app_id],text=True))[0]
assert app_inspect['Config']['Labels']['com.docker.compose.project']==PROJECT
BASE='http://127.0.0.1:39881'
def private(name,value):
 fd=os.open(ROOT/name,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 with os.fdopen(fd,'w') as f:json.dump(value,f,indent=2)
def record(case,ok,**detail):
 r={'case':case,'result':'PASS' if ok else 'FAIL','source_sha':SHA,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'http':detail.get('http')};RESULTS.append(r);print(json.dumps(r),flush=True)
 private('results-'+RUNID+'-'+str(len(RESULTS))+'.json',RESULTS)
def http(method,path,body=None,token=None,headers=None,raw=None):
 assert path.startswith(('/api/v1/','/health','/internal/mcp/isolated/personal'))
 h={'Content-Type':'application/json','Accept':'application/json'}
 if token:h['Authorization']='Bearer '+token
 h.update(headers or {});payload=raw if raw is not None else json.dumps(body).encode() if body is not None else None
 req=urllib.request.Request(BASE+path,data=payload,headers=h,method=method)
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 try:
  with opener.open(req,timeout=90) as r:status=r.status;data=r.read(2*1024*1024);rh=dict(r.headers)
 except urllib.error.HTTPError as e:status=e.code;data=e.read(65536);rh=dict(e.headers)
 try:value=json.loads(data)
 except ValueError:value={'transport_text':data.decode('utf-8','replace')[:4096]}
 return {'status':status,'body':value,'headers':rh}
def sql(q):
 p=subprocess.run(C+['exec','-T','postgres','psql','-X','-U','postgres','-d','consumer_native_test','-At','-v','ON_ERROR_STOP=1'],input='BEGIN READ ONLY;\n'+q+'\nROLLBACK;',text=True,capture_output=True,timeout=30)
 assert p.returncode==0,'Qualification SQL failed (raw query error kept private)'
 return '\n'.join(l for l in p.stdout.splitlines() if l not in ('BEGIN','ROLLBACK'))
def register(role):
 path=ROOT/(role+'-account.json')
 if path.exists():account=json.loads(path.read_text())
 else:
  account={'username':'native-'+role,'email':'native-'+role+'@example.invalid','password':secrets.token_urlsafe(16)+'aA!9'}
  private(role+'-account.json',account)
 if len(account['password'])>32:
  assert sql("SELECT count(*) FROM users WHERE email='native-"+role+"@example.invalid';")=='0'
  private(role+'-rejected-policy-account.json',account)
  account['password']=secrets.token_urlsafe(16)+'aA!9';path.write_text(json.dumps(account));os.chmod(path,0o600)
 r=http('POST','/api/v1/auth/register',account);assert r['status']==201,{'phase':'register','status':r['status'],'error':r['body'].get('error')}
 r=http('POST','/api/v1/auth/login',{'email':account['email'],'password':account['password']});assert r['status']==200 and r['body'].get('token')
 return r['body']
def upload(a,kb,name,content):
 boundary='NativeQualification'+uuid.uuid4().hex
 raw=('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+name+'"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+content+('\r\n--'+boundary+'--\r\n').encode()
 return http('POST','/api/v1/knowledge-bases/'+kb+'/knowledge/file',token=a['token'],headers={'Content-Type':'multipart/form-data; boundary='+boundary},raw=raw)
def mcp(token,method,params,path='/api/v1/mcp/personal'):
 h={'Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2024-11-05'}
 init=http('POST',path,{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'native-qualification','version':'1'}}},token,h)
 sid=next((v for k,v in init['headers'].items() if k.lower()=='mcp-session-id'),None)
 if sid:h['Mcp-Session-Id']=sid
 http('POST',path,{'jsonrpc':'2.0','method':'notifications/initialized'},token,h)
 return http('POST',path,{'jsonrpc':'2.0','id':2,'method':method,'params':params},token,h)
def main():
 health=http('GET','/health');record('full_app_health',health['status']==200)
 a=register('owner');other=register('other');record('dedicated_normal_accounts',not a['user'].get('is_system_admin') and not other['user'].get('is_system_admin'))
 info=http('GET','/api/v1/system/info',token=a['token']);actual=info['body'].get('data',{}).get('commit_id');record('running_executable_revision',info['status']==200 and actual==SHA,commit_id=actual)
 model=http('POST','/api/v1/models',{'name':'office-test-embedding','type':'Embedding','source':'remote','parameters':{'provider':'generic','base_url':'http://model-fixture:9000/v1','api_key':'isolated-nonsecret-fixture','embedding_parameters':{'dimension':1024}}},a['token']);assert model['status']==201
 chat=http('POST','/api/v1/models',{'name':'office-test-chat','type':'KnowledgeQA','source':'remote','parameters':{'provider':'generic','base_url':'http://model-fixture:9000/v1','api_key':'isolated-nonsecret-fixture'}},a['token']);assert chat['status']==201
 def kbcreate(wiki=False):
  r=http('POST','/api/v1/knowledge-bases',{'name':'Native '+('Wiki' if wiki else 'document')+' qualification','type':'document','embedding_model_id':model['body']['data']['id'],'summary_model_id':chat['body']['data']['id'],'chunking_config':{'chunk_size':512,'chunk_overlap':50},'indexing_strategy':{'vector_enabled':True,'keyword_enabled':True,'wiki_enabled':wiki,'graph_enabled':False}},a['token'])
  assert r['status']==201,{'phase':'create_kb','status':r['status']};return r['body']['data']['id']
 kb=kbcreate();private('acceptance-ids.json',{'kb':kb})
 r=upload(a,kb,'native-canary.txt',b'Nativecanary qualification: bounded consumer diagnostics retain safety. Wiki ownership preserves original queue policy.');record('plain_upload',r['status'] in (200,201),http=r['status'])
 r=upload(a,kb,'native-normal.docx',(ROOT/'normal.docx').read_bytes());record('docx_upload',r['status'] in (200,201),http=r['status'])
 deadline=time.monotonic()+240
 while time.monotonic()<deadline:
  rows=json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',k.id,'title',k.title,'parse_status',k.parse_status,'chunk_count',(SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL),'pending_subtasks_count',k.pending_subtasks_count)),'[]') FROM knowledges k WHERE k.knowledge_base_id='"+kb+"';"))
  if len(rows)==2 and all(r['parse_status'] in ('completed','failed') for r in rows):break
  time.sleep(3)
 record('embedded_workers_parse_and_finalize',len(rows)==2 and all(r['parse_status']=='completed' and r['chunk_count']>0 and r['pending_subtasks_count']==0 for r in rows),documents=rows)
 result=http('POST','/api/v1/knowledge-bases/'+kb+'/hybrid-search',{'query_text':'Nativecanary qualification','vector_threshold':0,'match_count':20},a['token']);record('real_index_retrieval',result['status']==200 and bool(result['body'].get('data')),http=result['status'])
 denied=upload(other,kb,'unauthorized.txt',b'No permission');record('unrelated_user_upload_denied',denied['status'] in (403,404),http=denied['status'])
 key=http('POST','/api/v1/mcp-api-keys',{'name':'Native isolated qualification','client_type':'generic','capabilities':['retrieve','add_content'],'never_expires':True,'personal_scope_mode':'fixed','tenant_scopes':[{'tenant_id':a['user']['tenant_id'],'kb_scope_mode':'selected','knowledge_bases':[{'knowledge_base_id':kb,'source_type':'owned'}]}]},a['token'])
 assert key['status'] in (200,201),{'phase':'key_create','http':key['status']}
 k=key['body']['data'];private('mcp-key.json',k);token=k.get('token') or k.get('credential',{}).get('token');assert token
 r=mcp(token,'tools/call',{'name':'search_knowledge','arguments':{'query':'Nativecanary qualification','knowledge_base_id':kb}})
 record('go_personal_mcp_search',r['status']==200 and not r['body'].get('error') and not r['body'].get('result',{}).get('isError',False) and 'nativecanary' in json.dumps(r['body']).lower(),http=r['status'],response=r['body'])
 eicar=bytes.fromhex('58354f2150254041505b345c505a58353428505e2937434329377d2445494341522d5354414e444152442d414e544956495255532d544553542d46494c452124482b482a')
 for filename,data in [('native-mcp.txt',b'Nativecanary MCP upload'),('eicar.txt',eicar)]:
  r=mcp(token,'tools/call',{'name':'upload_document','arguments':{'knowledge_base_id':kb,'filename':filename,'content_base64':base64.b64encode(data).decode(),'idempotency_key':'native-'+uuid.uuid4().hex}})
  rejected=bool(r['body'].get('result',{}).get('isError',False));record('real_clamav_reject_eicar' if filename=='eicar.txt' else 'go_personal_mcp_clean_upload',r['status']==200 and rejected==(filename=='eicar.txt'),http=r['status'],response=r['body'])
 r=mcp(token,'tools/call',{'name':'upload_document','arguments':{'knowledge_base_id':kb,'filename':'readonly.txt','content_base64':base64.b64encode(b'no write').decode(),'idempotency_key':'readonly-'+uuid.uuid4().hex}},'/internal/mcp/isolated/personal')
 record('strict_readonly_mcp_write_denied',r['status'] in (400,403,404) or bool(r['body'].get('error')) or bool(r['body'].get('result',{}).get('isError')),http=r['status'])
 wkb=kbcreate(True)
 page=http('POST','/api/v1/knowledgebase/'+wkb+'/wiki/pages',{'slug':'concept/nativecanary','title':'Nativecanary','page_type':'concept','status':'published','content':'# Nativecanary\n\nIsolated native APP qualification.','summary':'Qualification'},a['token'])
 record('wiki_normal_owner_create',page['status']==201,http=page['status'])
 read=http('GET','/api/v1/knowledgebase/'+wkb+'/wiki/pages/concept/nativecanary',token=a['token'])
 record('wiki_read',read['status']==200 and 'Nativecanary' in json.dumps(read['body']),http=read['status'])
 for operation,body in [('prepare',{'hold_for_review':True,'runtime_sha':SHA,'incident_id':'unauthorized','trip':'unauthorized'}),('execute',{'plan':{'hold_for_review':True},'approval':{'operation':'reset'}})]:
  denied=http('POST','/api/v1/datasource/00000000-0000-0000-0000-000000000001/safety-recovery/'+operation,body,a['token'])
  record('observation_'+operation+'_rejects_nonadmin',denied['status']==403,http=denied['status'])
 assert len(RESULTS)==16 and all(x['result']=='PASS' for x in RESULTS),'A required native APP case failed'
 private('acceptance-final.json',{'source_sha':SHA,'results':RESULTS,'production_changes':False,'llm_fixture_limit':'Deterministic test model: pipeline qualification, not semantic quality or complete AI Wiki generation.'})
 print(json.dumps({'passed':sum(r['result']=='PASS' for r in RESULTS),'failed':sum(r['result']=='FAIL' for r in RESULTS)}),flush=True)
if __name__=='__main__':
 try:main()
 except Exception as error:
  print(json.dumps({'phase':'native_app_acceptance','error_class':type(error).__name__,'results':RESULTS}),flush=True)
  raise SystemExit(1)

