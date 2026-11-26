"""A real HTTP client inside the owned APP namespace; never calls Go internals."""
import base64,json,os,sys,urllib.request,urllib.error
assert os.getuid()==1000
p=json.load(sys.stdin);assert p['path'].startswith(('/api/v1/','/health','/internal/mcp/isolated/personal'))
data=base64.b64decode(p['payload']) if p.get('payload') is not None else None
req=urllib.request.Request('http://127.0.0.1:8080'+p['path'],data=data,headers=p.get('headers',{}),method=p['method'])
try:
 with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req,timeout=90) as response:status=response.status;body=response.read(2*1024*1024);headers=dict(response.headers)
except urllib.error.HTTPError as error:status=error.code;body=error.read(65536);headers=dict(error.headers)
except urllib.error.URLError as error:
 print(json.dumps({'status':0,'body':{},'headers':{},'transport_error':{'class':type(error.reason).__name__,'errno':getattr(error.reason,'errno',None)}}));raise SystemExit(0)
try:value=json.loads(body)
except ValueError:value={'transport_text':body.decode('utf-8','replace')[:4096]}
print(json.dumps({'status':status,'body':value,'headers':headers}))
