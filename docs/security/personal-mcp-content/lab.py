"""Disposable synthetic APP lab. No production resources or existing DB access."""
import os,pathlib,subprocess,json,secrets,threading,struct,socket,time,shutil
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
B=pathlib.Path('/home/aqyfzx/ai/knowhub-personal-upload-r1-20261112');P='knowhub-personal-upload-r1-20261112'
os.umask(0o077)
assert B.name==P

def run(args,timeout=120):return subprocess.check_output(args,stderr=subprocess.STDOUT,timeout=timeout).decode()
def start(args):return run(['docker','run','-d','--label','knowhub.scope='+P]+args).strip()
if not (B/'private/clients.json').exists():
 for port in [18871,18872,18873,18874,18875,18876]:
  s=socket.socket();s.bind(('127.0.0.1',port));s.close()
 start(['--name',P+'-pg','--cpus','2','--memory','2g','-p','127.0.0.1:18871:5432','-e','POSTGRES_DB=knowhub_personal_upload3046','-e','POSTGRES_HOST_AUTH_METHOD=trust','paradedb/paradedb:v0.22.2-pg17','-c','log_statement=none','-c','log_min_error_statement=panic'])
 start(['--name',P+'-redis','-p','127.0.0.1:18872:6379','redis:7.0-alpine','redis-server','--save','','--appendonly','no'])
 for _ in range(60):
  if subprocess.run(['docker','exec',P+'-pg','pg_isready','-h','127.0.0.1','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('isolated postgres unavailable')
 env=dict(os.environ,KNOWHUB_UPLOAD_ACCEPT_DIR=str(B));r=subprocess.run(['docker','run','--rm','--network','host','--user',str(os.getuid())+':'+str(os.getgid()),'-v',str(B)+':'+str(B),'-e','KNOWHUB_UPLOAD_ACCEPT_DIR='+str(B),'-v','/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/lab/jieba:ro','-e','JIEBA_DICT_DIR=/lab/jieba','-w',str(B/'source'),'knowhub-build/golang:1.26-bookworm-ustc',str(B/'seed')],stdout=(B/'seed.log').open('wb'),stderr=subprocess.STDOUT);assert r.returncode==0
class Model(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_POST(self):
  data=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
  if self.path.endswith('/embeddings'):
   inp=data.get('input',[]);inp=[inp] if isinstance(inp,str) else inp
   out={'data':[{'index':i,'embedding':[1.,0.,0.]} for i,_ in enumerate(inp)],'usage':{'prompt_tokens':1,'total_tokens':1}}
  else:out={'choices':[{'message':{'role':'assistant','content':'Synthetic document summary'},'finish_reason':'stop'}],'usage':{'prompt_tokens':1,'completion_tokens':1,'total_tokens':2}}
  b=json.dumps(out).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
server=ThreadingHTTPServer(('127.0.0.1',18874),Model);threading.Thread(target=server.serve_forever,daemon=True).start()
# The parser runs only inside an isolated test network.
network=P+'-parser'
subprocess.run(['docker','network','create','--internal','--subnet','198.18.249.0/28','--label','knowhub.scope='+P,network],check=True,stdout=subprocess.DEVNULL)
start(['--name',P+'-docreader','--network',network,'--cpus','2','--memory','2g','knowhub-feishu-v2/docreader:8cef28d360f9fc4457ab9fe1b9a8fcf74c48de31'])
parser_ip=run(['docker','inspect','--format','{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',P+'-docreader']).strip()
config=B/'runtime/config';shutil.copytree(B/'source/config',config,dirs_exist_ok=True)
f=config/'config.yaml';text=f.read_text().replace('  enabled: false\n  policy_reference: ""','  enabled: true\n  policy_reference: "synthetic-upload-lab"',1).replace('  port: 8080','  port: 18873',1).replace('  host: "0.0.0.0"','  host: "127.0.0.1"',1);f.write_text(text)
private=B/'private';private.mkdir(exist_ok=True);(private/'app.env').write_text('JWT_SECRET='+secrets.token_hex(32)+'\nSYSTEM_AES_KEY='+secrets.token_hex(16)+'\nSYSTEM_SIGNING_KEY='+secrets.token_hex(32)+'\n')
for name in ['files','logs']:(B/name).mkdir(exist_ok=True)
args=['--name',P+'-app','--network','host','--cpus','2','--memory','4g','--env-file',str(private/'app.env'),'-v',str(B/'WeKnora')+':/lab/WeKnora:ro','-v',str(config)+':/app/config:ro','-v',str(B/'files')+':/lab/files','-v',str(B/'logs')+':/lab/logs','-v','/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/lab/jieba:ro','-w','/app']
environment={'DOCREADER_ADDR':parser_ip+':50051','DOCREADER_TRANSPORT':'grpc','AUTO_MIGRATE':'false','DB_DRIVER':'postgres','DB_HOST':'127.0.0.1','DB_PORT':'18871','DB_USER':'postgres','DB_PASSWORD':'synthetic-upload-only','DB_NAME':'knowhub_personal_upload3046','REDIS_ADDR':'127.0.0.1:18872','REDIS_PASSWORD':'','RETRIEVE_DRIVER':'postgres','STORAGE_TYPE':'local','LOCAL_STORAGE_BASE_DIR':'/lab/files','JIEBA_DICT_DIR':'/lab/jieba','GIN_MODE':'release','LOG_PATH':'/lab/logs/app.log','LOG_LEVEL':'info','SSRF_WHITELIST':'127.0.0.1','WEKNORA_TENANT_ENABLE_RBAC':'true','GOMAXPROCS':'2','LANGFUSE_ENABLED':'false'}
for k,v in environment.items():args+=['-e',k+'='+v]
args+=['knowhub-build/golang:1.26-bookworm-ustc','/lab/WeKnora'];start(args)
(B/'lab-started.json').write_text(json.dumps({'antivirus_scan':False,'schema':3046,'auto_migrate':False,'app':'http://127.0.0.1:18873','production_changed':False}))
while not (B/'stop-lab').exists():time.sleep(1)
