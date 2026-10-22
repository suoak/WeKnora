"""Additional complete race regression bound to the same immutable dev source."""
from pathlib import Path
import hashlib,json,shlex,sys
from v1_host import connect
from next_test_environment import docker_hosts

root=Path(__file__).resolve().parents[4]
out=root/'docs/integration/knowhub-mcp-portal/evidence/v1-wiki-dynamic'
c=json.loads((out/'development-current.json').read_text())
remote=c['remote'];dest=out/('extra-'+c['source_sha256'][:16])
if sys.argv[1]=='collect':
 dest.mkdir(exist_ok=True)
 with connect()as ssh:
  with ssh.open_sftp()as f:
   resultdir=c.get('extra_results_dir','extra-quality')
   if resultdir!='extra-quality':dest=dest/resultdir;dest.mkdir(exist_ok=True)
   for n in f.listdir(remote+'/'+resultdir):
    if n.endswith(('.json','.log')):f.get(remote+'/'+resultdir+'/'+n,str(dest/n))
 print((dest/'done.json').read_text()if(dest/'done.json').exists()else'RUNNING')
 raise SystemExit(0)
assert all(hashlib.sha256((root/n).read_bytes()).hexdigest()==h for n,h in c['files'].items())
resultdir='extra-quality-extended' if sys.argv[1]=='retry-extended' else 'extra-quality'
script=r'''
from pathlib import Path
import datetime,json,subprocess
b=Path(BASE);out=b/RESULT_DIR;out.mkdir(exist_ok=False)
m=json.loads((b/'manifest.json').read_text());prior=json.loads((b/'results/results.json').read_text())
base=next(x['argv']for x in prior if x['name']=='focused');base=base[:base.index('go')]
args=base+['go','test','-race','-tags','sqlite_fts5','-p','2','-count=1',
 './internal/application/access','./internal/application/repository','./internal/application/service',
 './internal/middleware','./internal/mcpserver','./internal/router']
# Separate container name permits this independent quality check alongside
# the original unit runner, without sharing a process or test database.
args[args.index('--name')+1]+='-full-race'
while '--add-host' in args:
 hostidx=args.index('--add-host');del args[hostidx:hostidx+2]
args[2:2]=HOST_ARGS
args[args.index('-race')+1:args.index('-race')+1]=['-timeout','25m']
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
with(out/'full-race.log').open('wb')as f:
 try:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=1800).returncode
 except subprocess.TimeoutExpired:code=124
(out/'done.json').write_text(json.dumps(dict(source_sha256=m['source_sha256'],all_passed=code==0,exit_code=code,argv=args,started_utc=started,finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat()),indent=2))
'''.replace('BASE',repr(remote)).replace('RESULT_DIR',repr(resultdir)).replace('HOST_ARGS',repr(docker_hosts()))
with connect()as ssh:
 with ssh.open_sftp()as f:
  with f.open(remote+'/extra-quality.py','w')as h:h.write(script)
 _,o,e=ssh.exec_command('nohup python3 '+shlex.quote(remote+'/extra-quality.py')+' > '+shlex.quote(remote+'/extra-quality-launcher.log')+' 2>&1 </dev/null &')
 assert o.channel.recv_exit_status()==0,e.read().decode();o.close();e.close()
c['extra_results_dir']=resultdir
(out/'development-current.json').write_text(json.dumps(c,indent=2),encoding='utf8')
print('complete race started '+c['source_sha256'])
