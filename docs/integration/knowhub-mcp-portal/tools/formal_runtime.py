"""Shared-test HTTP helpers. Credentials never enter public results."""
from pathlib import Path
import json,subprocess,time,urllib.request,urllib.error

class Runtime:
 def __init__(self,base):
  self.base=Path(base).resolve();assert self.base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
  self.state=json.loads((self.base/'private/state.json').read_text());self.private=Path(self.state['private_path']);self.candidate=Path(self.state['candidate'])
  self.sha=json.loads((self.candidate/'manifest.json').read_text())['candidate_sha256'];self.origin='http://127.0.0.1';self.actions=[];self.rpc=0
  self.env=dict(x.split('=',1)for x in Path(self.state['fixture_env']).read_text().splitlines()if'='in x)
 def request(self,method,path,body=None,token=None,tenant=None):
  h={'Content-Type':'application/json','Accept':'application/json, text/event-stream'}
  if token:h['Authorization']='Bearer '+token
  if tenant:h['X-Tenant-ID']=str(tenant)
  q=urllib.request.Request(self.origin+path,data=json.dumps(body).encode()if body is not None else None,headers=h,method=method)
  started=time.monotonic()
  try:
   with urllib.request.urlopen(q,timeout=45)as s:status,raw=s.status,s.read().decode();self.headers={k.lower():v for k,v in s.headers.items()}
  except urllib.error.HTTPError as e:status,raw=e.code,e.read().decode();self.headers={k.lower():v for k,v in e.headers.items()}
  if raw.startswith(('event:','data:')):raw=next(s[5:]for s in raw.splitlines()if s.startswith('data:'))
  try:data=json.loads(raw)if raw else None
  except ValueError:data={'http_error':raw[:200]};assert status>=400,'unexpected non-JSON success'
  self.actions.append({'method':method,'path':path,'status':status,'elapsed_ms':1000*(time.monotonic()-started)})
  return status,data
 def login(self,who):
  status,d=self.request('POST','/api/v1/auth/login',{'email':'fm-'+who+'@example.invalid','password':self.env['FORMAL_TEST_PASSWORD']});assert status==200 and d.get('token'),'synthetic local account login failed';return d
 def create_key(self,login,name,mode='dynamic_all'):
  jwt=login['token'];status,options=self.request('GET','/api/v1/mcp-api-keys/scope-options',token=jwt);assert status==200
  scopes=[{'tenant_id':v['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for v in options['data']]
  body={'name':name,'client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes}
  if mode is not None:body['personal_scope_mode']=mode
  status,d=self.request('POST','/api/v1/mcp-api-keys',body,jwt);assert status==201 and not d['data'].get('expires_at')
  assert d['data']['personal_scope_mode']==('fixed'if mode is None else mode)
  return d['data']
 def call(self,token,name,args=None):
  self.rpc+=1;status,d=self.request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':self.rpc,'method':'tools/call','params':{'name':name,'arguments':args or {}}},token)
  self.actions[-1].update({'tool':name,'arguments':args or{},'is_error':self.denied((status,d))});return status,d
 @staticmethod
 def denied(response):
  status,d=response;return status>=400 or bool(d and(d.get('error')or d.get('result',{}).get('isError')))
 @staticmethod
 def value(response):
  status,d=response;assert status==200 and not Runtime.denied(response),'MCP operation denied'
  return json.loads(d['result']['content'][0]['text'])
 def sql(self,q):
  if q.lstrip().upper().startswith(('INSERT','UPDATE','DELETE')):assert 'fm-'in q or'820'in q,'unscoped fixture mutation forbidden'
  p=subprocess.run(['docker','exec','-i','-u','postgres','WeKnora-postgres','psql','-X','-U','postgres','-d','WeKnora','-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),capture_output=True,timeout=40)
  assert p.returncode==0,'scoped test SQL failed';return p.stdout.decode().strip()
 def future(self):
  image=json.loads((self.candidate/'results/build/done.json').read_text())['app']
  p=subprocess.run(['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',self.state['fixture_env'],'-e','FORMAL_SEED_MODE=future','-v',str(Path(self.state['backup_path'])/'operators')+':/ops:ro','--entrypoint','/ops/formal-seed',image],capture_output=True,timeout=120)
  assert p.returncode==0,'future fixture creation failed'
