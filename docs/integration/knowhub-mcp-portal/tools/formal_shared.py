"""Operate only explicitly approved build-host P3; preserve frozen candidates."""
from pathlib import Path
import datetime,hashlib,json,shlex,sys
from v1_host import connect
root=Path(__file__).resolve().parents[4];docs=root/'docs/integration/knowhub-mcp-portal';out=docs/'evidence/formal-shared-upgrade';out.mkdir(exist_ok=True)
mode=sys.argv[1]
if mode=='init':
 stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dt%H%M%Sz').lower()
 c={'candidate_sha256':'c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75','remote':'/home/aqyfzx/ai/knowhub-formal-c2-20261022/'+stamp,'production_authorized':False}
 with connect()as ssh:
  _,o,e=ssh.exec_command('mkdir -p '+shlex.quote(c['remote'].rsplit('/',1)[0])+' && mkdir '+shlex.quote(c['remote']));assert o.channel.recv_exit_status()==0,e.read().decode()
 (out/'current.json').write_text(json.dumps(c,indent=2));print(json.dumps(c));raise SystemExit(0)
c=json.loads((out/'current.json').read_text());base=c['remote'];assert base.startswith('/home/aqyfzx/ai/knowhub-formal-c2-20261022/')
with connect()as ssh:
 if mode.startswith('start-'):
  stage=mode[6:];script=Path(__file__).parent/('formal_shared_'+stage.replace('-','_')+'_linux.py');payload=script.read_bytes();sha=hashlib.sha256(payload).hexdigest();remote=base+'/'+stage+'-'+sha+'.py'
  with ssh.open_sftp()as f:
   with f.open(remote,'wb')as h:h.write(payload)
   if stage in ('final-smoke','legacy-python','auth-edges','business','wiki-negative','portal-dynamic','feishu','recovery','compatibility'):
    data=(Path(__file__).parent/'formal_runtime.py').read_bytes();hsha=hashlib.sha256(data).hexdigest();hp=base+'/formal-runtime-'+hsha+'.py'
    with f.open(hp,'wb')as h:h.write(data)
    with f.open(base+'/formal-runtime-current.json','w')as h:h.write(json.dumps({'path':hp,'sha256':hsha}))
   if stage=='build-ops':
    data=(Path(__file__).parent/'formal_operator.go.txt').read_bytes()
    with f.open(base+'/formal_operator.go.txt','wb')as h:h.write(data)
   if stage=='build-seed':
    data=(Path(__file__).parent/'formal_seed.go.txt').read_bytes()
    with f.open(base+'/formal_seed.go.txt','wb')as h:h.write(data)
   if stage=='migration-red':
    data=(root/'internal/application/repository/feishu_legacy_migration_test.go').read_bytes()
    with f.open(base+'/formal_migration_test.go','wb')as h:h.write(data)
  (out/(stage+'-harness-'+sha+'.json')).write_text(json.dumps({'script_sha256':sha,'candidate_sha256':c['candidate_sha256'],'remote':remote},indent=2))
  _,o,e=ssh.exec_command('nohup '+shlex.join(['python3',remote,base]+sys.argv[2:3])+' > '+shlex.quote(base+'/'+stage+'-launcher-'+sha+'.log')+' 2>&1 </dev/null &');assert o.channel.recv_exit_status()==0,e.read().decode();print(stage+' started '+sha)
 elif mode=='collect':
  with ssh.open_sftp()as f:
   for name in f.listdir(base):
    if '-launcher-'in name and name.endswith('.log'):f.get(base+'/'+name,str(out/name))
   for stage in f.listdir(base+'/results'):
    dest=out/(stage+'-'+Path(base).name);dest.mkdir(exist_ok=True)
    for n in f.listdir(base+'/results/'+stage):
     if n.endswith(('.json','.log','.txt')):f.get(base+'/results/'+stage+'/'+n,str(dest/n))
    result=dest/'result.json';print(stage,json.loads(result.read_text())if result.exists()else'RUNNING')
 else:raise ValueError('Unsupported operation')
