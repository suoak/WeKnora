"""Replay exact preserved Python server bytes with fresh synthetic credentials.

The running original container/config/credentials are never replaced or used.
"""
from pathlib import Path
import hashlib,importlib.util,json,os,secrets,subprocess,sys
base=Path(sys.argv[1]).resolve();h=json.loads((base/'formal-runtime-current.json').read_text());p=Path(h['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==h['sha256'];s=importlib.util.spec_from_file_location('r',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);r=m.Runtime(base)
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert attempt in('initial','a2','a3','a4','a5');out=base/'results'/('legacy-python'if attempt=='initial'else'legacy-python-'+attempt);out.mkdir(exist_ok=False);key=None;rows=[]
probe=r'''
import asyncio,json,os,socket,subprocess,sys,tempfile,time
from contextlib import AsyncExitStack
from mcp import ClientSession
from mcp.client.sse import sse_client
from mcp.client.streamable_http import streamable_http_client
from mcp.shared._httpx_utils import create_mcp_http_client
kid=sys.argv[1];allowed=sys.argv[2]=='allow';checks=[]
async def test(url,transport):
 headers={'Authorization':'Bearer '+os.environ['MCP_SERVER_AUTH_TOKEN'],'X-Tenant-ID':'820099','X-WeKnora-API-Key':os.environ['WEKNORA_API_KEY']}
 async with AsyncExitStack()as stack:
  if transport=='sse':read,write=await stack.enter_async_context(sse_client(url+'/sse',headers=headers))
  else:
   client=await stack.enter_async_context(create_mcp_http_client(headers=headers));streams=await stack.enter_async_context(streamable_http_client(url+'/mcp',http_client=client));read,write=streams[:2]
  session=await stack.enter_async_context(ClientSession(read,write));await session.initialize()
  tools=await session.list_tools();assert any(t.name=='get_knowledge'for t in tools.tools)
  for tool in('get_knowledge','list_chunks'):
   start=time.monotonic();res=await asyncio.wait_for(session.call_tool(tool,{'knowledge_id':kid}),15);text=res.model_dump_json()
   if allowed:
    if res.is_error or kid not in text:print(json.dumps({'tool':tool,'actual_response':json.loads(text)}),flush=True)
    assert not res.is_error and kid in text
    if tool=='list_chunks':assert 'updatedcanary'in text
   else:
    assert res.is_error or any(x in text for x in('401','403','404'))
    assert not any(x in text for x in('updatedcanary','initialcanary','Synthetic Wiki','Synthetic Drive'))
   checks.append({'transport':transport,'tool':tool,'allowed':allowed,'seconds':time.monotonic()-start})
for transport in('sse','http'):
 with socket.socket()as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
 with tempfile.TemporaryFile()as log:
  process=subprocess.Popen([sys.executable,'/legacy.py','--transport',transport,'--host','127.0.0.1','--port',str(port)],stdout=log,stderr=log)
  try:
   for _ in range(100):
    if process.poll()is not None:raise RuntimeError('preserved server terminated')
    try:
     with socket.create_connection(('127.0.0.1',port),timeout=.2):break
    except OSError:time.sleep(.1)
   else:raise RuntimeError('preserved server readiness timeout')
   asyncio.run(asyncio.wait_for(test('http://127.0.0.1:'+str(port),transport),90))
  finally:
   process.terminate()
   try:process.wait(timeout=10)
   except subprocess.TimeoutExpired:process.kill();process.wait()
print(json.dumps({'exit_code':0,'exact_preserved_server':True,'calls':checks}))
'''
try:
 owner=r.login('owner');status,d=r.request('POST','/api/v1/tenants/820099/api-keys',{'name':'Formal original Python tenant compatibility','full_access':True},owner['token'],820099);assert status==201;key=d['data']
 original=json.loads(subprocess.check_output(['docker','inspect','WeKnora-mcp']))[0];assert original['State']['Running']
 server=r.private/'original-python-server.py'
 if not server.exists():
  code=subprocess.run(['docker','cp','WeKnora-mcp:/app/weknora_mcp_server.py',str(server)],stdout=subprocess.PIPE,stderr=subprocess.PIPE).returncode;assert code==0,'exact server copy failed'
 assert hashlib.sha256(server.read_bytes()).hexdigest()==subprocess.check_output(['docker','exec','WeKnora-mcp','sha256sum','/app/weknora_mcp_server.py']).decode().split()[0]
 test=r.private/('original-python-probe-'+attempt+'.py');test.write_text(probe);server_token=secrets.token_urlsafe(36)
 envfile=r.private/('original-python-probe-'+attempt+'.env');envfile.write_text('MCP_AUTH_MODE=shared\nMCP_SERVER_AUTH_TOKEN='+server_token+'\nWEKNORA_API_KEY='+key['token']+'\nWEKNORA_TENANT_ID=820099\nWEKNORA_BASE_URL=http://p3app:8080/api/v1\n');os.chmod(envfile,0o600)
 sources=json.loads((base/'feishu-source-current.json').read_text())['sources']
 for phase,kid,allowed in [('active',sources['wiki']['knowledge_id'],True),('source-revoked',sources['drive']['knowledge_id'],False),('key-revoked',sources['wiki']['knowledge_id'],False)]:
  if phase=='key-revoked':assert r.request('DELETE','/api/v1/tenants/820099/api-keys/'+str(key['id']),token=owner['token'],tenant=820099)[0]==200
  argv=['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',str(envfile),'-v',str(server)+':/legacy.py:ro','-v',str(test)+':/probe.py:ro','--entrypoint','python',original['Image'],'/probe.py',kid,'allow'if allowed else'deny']
  p=subprocess.run(argv,capture_output=True,timeout=240);raw=p.stdout+p.stderr
  for secret in(key['token'],server_token,owner['token']):raw=raw.replace(secret.encode(),b'[REDACTED]')
  (out/(phase+'.log')).write_bytes(raw);rows.append({'phase':phase,'argv':argv,'exit_code':p.returncode});(out/'commands.json').write_text(json.dumps(rows,indent=2));assert p.returncode==0,phase
 same=json.loads(subprocess.check_output(['docker','inspect','WeKnora-mcp']))[0];assert same['Id']==original['Id']and same['Image']==original['Image']and same['State']['Running']
 result={'exit_code':0,'candidate_sha256':r.sha,'original_container_preserved':True,'original_image_id':original['Image'],'exact_original_server_sha256':hashlib.sha256(server.read_bytes()).hexdigest(),'original_two_credential_SSE_and_HTTP':'PASS','active_source_and_revoked_source_and_revoked_Key':'PASS','original_credentials_used':False,'legacy_credential_type':'new synthetic legacy tenant key; full_access must still respect persisted source denial; original server does not forward user_mcp tenant headers','synthetic_only':True}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e)}
finally:
 if key:r.request('DELETE','/api/v1/tenants/820099/api-keys/'+str(key['id']),token=owner['token'],tenant=820099)
 (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
