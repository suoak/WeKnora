"""Fresh non-root capability/resource preflight; only creates an owned empty task directory."""
import datetime,importlib.util,json,shlex
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];OUT=ROOT/'docs/qa/office-test-deployment';OUT.mkdir(exist_ok=True)
REMOTE=r'''
import datetime,json,os,pathlib,socket,subprocess,shutil
assert socket.gethostname()=='aqyfzx10' and os.environ['SSH_CONNECTION'].split()[2]=='172.30.232.42' and os.geteuid()!=0
out={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'hostname':socket.gethostname(),'user_uid':os.geteuid(),'checks':[],'existing_services_changed':False}
def run(args,timeout=30):
 try:
  p=subprocess.run(args,capture_output=True,text=True,timeout=timeout);r={'argv':args,'exit_code':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
 except subprocess.TimeoutExpired:r={'argv':args,'status':'TIMEOUT'}
 out['checks'].append(r);return r.get('stdout','') if r.get('exit_code')==0 else ''
assert run(['docker','info','--format','{{.Name}}']).strip()=='aqyfzx10'
run(['docker','compose','version']);run(['docker','info','--format','{{.DockerRootDir}}']);run(['df','-B1','/','/mnt/sata0']);run(['df','-i','/','/mnt/sata0']);run(['free','-b']);run(['nproc']);run(['uptime']);run(['ss','-ltn']);run(['docker','compose','ls','--format','json'])
base=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds');assert not base.is_symlink() and base.resolve()==base
out['task_parent_writable']=os.access(base,os.W_OK|os.X_OK)
target=base/'office-guard-4ffd7442d-20261125'
if target.exists():assert not target.is_symlink() and target.stat().st_uid==os.geteuid()
else:target.mkdir(mode=0o700)
out['task_directory']={'path':str(target),'uid':target.stat().st_uid,'mode':oct(target.stat().st_mode&0o777)}
assert not run(['docker','ps','-aq','--filter','label=com.docker.compose.project=knowhub-office-guard-test']).strip(),'project name already used; review ownership before resume'
out['free_ports']={}
for port in (39780,39781):
 s=socket.socket()
 try:s.bind(('127.0.0.1',port));out['free_ports'][str(port)]=True
 except OSError:out['free_ports'][str(port)]=False
 finally:s.close()
out['config_readable']=os.access(base/'d6e99d894d3a-457bd103/isolated-site/config.yaml',os.R_OK)
out['image_inventory']=[]
for tag in ['paradedb/paradedb:v0.22.2-pg17','redis:7.0-alpine','sha256:7ddcecd6854e3002d93a8643c689eeaa653173d2e44389bb44216c968398209d']:
 raw=run(['docker','image','inspect',tag,'--format','{{.Id}}']);out['image_inventory'].append({'requested':tag,'id':raw.strip()})
print(json.dumps(out))
'''
spec=importlib.util.spec_from_file_location('known_test_host',ROOT/'docs/integration/knowhub-mcp-portal/tools/v1_host.py');helper=importlib.util.module_from_spec(spec);spec.loader.exec_module(helper)
with helper.connect() as c:
 _,o,e=c.exec_command('python3 -c '+shlex.quote(REMOTE),timeout=180);body=o.read(2*1024*1024);err=e.read(65536);code=o.channel.recv_exit_status()
 if code:print(json.dumps({'status':'BLOCKED','exit':code,'diagnostic':err.decode().splitlines()[-3:]}));raise SystemExit(1)
v=json.loads(body);stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ');p=OUT/('preflight-'+stamp+'.json');p.write_bytes(body+b'\n')
print(json.dumps({'evidence':p.name,'task_directory':v['task_directory'],'ports':v['free_ports'],'checks':[{k:w for k,w in r.items() if k!='stderr'} for r in v['checks'] if r['argv'][0] in ('df','free','nproc','uptime')]}))
