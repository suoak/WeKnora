"""Actual isolated Nginx -> same Go APP: same Key, exact read-only MCP route."""
from pathlib import Path
import datetime,json,urllib.request,urllib.error
ROOT=Path(__file__).resolve().parents[4]
runtime=json.loads(Path('E:/AIData/Codex/knowhub-b7b-clients/connection-runtime.json').read_text())
url=runtime.get('tunnel_url',runtime['url']);origin=url.rsplit('/api/',1)[0]
rows=[]
def request(label,target,method='POST',rpc=None,authenticated=True,extra=None):
    headers={'Accept':'application/json, text/event-stream','Content-Type':'application/json'}
    if authenticated:headers['Authorization']='Bearer '+runtime['token']
    headers.update(extra or {})
    r=urllib.request.Request(target,data=json.dumps(rpc).encode()if rpc is not None else None,headers=headers,method=method)
    try:
        with urllib.request.urlopen(r,timeout=20)as response:status=response.status;raw=response.read()
    except urllib.error.HTTPError as e:status=e.code;raw=e.read()
    body=None
    if raw:
        text=raw.decode()
        if text.startswith('event:')or text.startswith('data:'):
            text=next(line[5:].strip()for line in text.splitlines()if line.startswith('data:'))
        try:body=json.loads(text)
        except ValueError:pass
    rows.append({'label':label,'http_status':status,'bytes':len(raw)})
    return status,body
initial={'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'knowhub-network-gate','version':'1'}}}
try:
    status,_=request('nginx_initialize',url,rpc=initial);assert status==200
    status,_=request('same_key_same_app_initialize',runtime['app_url']+'/api/v1/mcp/personal',rpc=initial);assert status==200
    status,body=request('nginx_tools_list',url,rpc={'jsonrpc':'2.0','id':2,'method':'tools/list'});assert status==200
    tools={t['name']for t in body['result']['tools']};assert tools=={'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'}
    for i,name in enumerate(('create_knowledge','upload_file','delete_knowledge','update_chunk','manage_space')):
        status,body=request('blocked_unregistered_write_'+name,url,rpc={'jsonrpc':'2.0','id':10+i,'method':'tools/call','params':{'name':name,'arguments':{}}});assert status==200 and ('error'in body or body.get('result',{}).get('isError'))
    for route in ('/api/v1/knowledges','/api/v1/auth/me','/api/v1/system/admin/portal/spaces','/mcp/anything','/api/v1/mcp/personal/'):
        status,_=request('blocked_other_route_'+route,origin+route,method='GET');assert status==403
    status,_=request('no_key_identity_headers_cannot_authenticate',url,rpc=initial,authenticated=False,extra={'X-User-ID':'A','X-RJGH':'synthetic','X-From-MCP':'false'});assert status==401
    record={'candidate':runtime['candidate'],'exit_code':0,'checks':rows,'tools':sorted(tools),'same_key_and_app':True,'readonly_server_and_network_gate':'PASS','production_network':'NOT_OPENED_NOT_TESTED'}
except BaseException as e:
    record={'candidate':runtime['candidate'],'exit_code':1,'checks':rows,'error_class':type(e).__name__}
record['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
out=ROOT/'docs/integration/knowhub-mcp-portal/evidence/batch7/continued/network-check.json';out.write_text(json.dumps(record,indent=2),encoding='utf8')
print(json.dumps(record));raise SystemExit(record['exit_code'])
