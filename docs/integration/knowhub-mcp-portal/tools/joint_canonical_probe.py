"""Real PostgreSQL regression for canonical writer representations; isolated DBs."""
from pathlib import Path
import json,shlex,sys,hashlib
from v1_host import connect
root=Path(__file__).resolve().parents[4]
c=json.loads((root/'docs/integration/knowhub-mcp-portal/evidence/feishu-incremental/joint-release-source-candidate.json').read_text())
base=c['remote']
code=r"""
from pathlib import Path
import json,subprocess,time
base=Path(BASE)
for _ in range(600):
 rows=json.loads((base/'results/results.json').read_text())['results']
 ready=[r['argv'] for r in rows if r['name']=='new-service-contract']
 if ready:break
 time.sleep(1)
else:raise RuntimeError('development setup did not complete')
argv=ready[0]
argv=argv[:argv.index('go')]
pg='feishu-atomic-'+json.loads((base/'manifest.json').read_text())['source_sha256'][:12]+'-joint-release-source-pg'

for _ in range(600):
 check=subprocess.run(['docker','inspect',pg],capture_output=True)
 if check.returncode==0:
  info=json.loads(check.stdout)[0]
  if info['State']['Running'] and subprocess.run(['docker','exec',pg,'pg_isready','-h','127.0.0.1','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
 time.sleep(1)
else:raise RuntimeError('isolated PostgreSQL did not start')
assert info['Config']['Labels']['knowhub.feishu']=='development' and info['State']['Running']
argv[argv.index('none')]='container:'+pg
idx=argv.index('-w');argv[idx:idx]=['-e','FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable']
out=base/'canonical-probe2';out.mkdir(exist_ok=False)
commands=[('actual-pg', ['go','test','-tags','sqlite_fts5','-count=1','-v','./internal/router','-run','^TestPortalCanonicalIndexWritersPostgres$']),('actual-pg-race',['go','test','-race','-tags','sqlite_fts5','-count=1','-v','./internal/router','-run','^TestPortalCanonicalIndexWritersPostgres$']),('postgres-vet',['go','vet','-tags','sqlite_fts5','./internal/application/repository/retriever/postgres'])]
results=[]
for name,command in commands:
 with (out/(name+'.log')).open('wb') as f:
  start=time.time();rc=subprocess.run(argv+command,stdout=f,stderr=subprocess.STDOUT,timeout=1200).returncode
 results.append(dict(name=name,argv=argv+command,exit_code=rc,seconds=time.time()-start))
 (out/'results.json').write_text(json.dumps(results,indent=2))
 if rc:break
(out/'done.json').write_text(json.dumps(dict(source_sha256=json.loads((base/'manifest.json').read_text())['source_sha256'],all_passed=len(results)==len(commands) and all(r['exit_code']==0 for r in results)),indent=2))
""".replace('BASE',repr(base))
with connect() as ssh:
 if len(sys.argv)>1 and sys.argv[1]=='collect':
  dest=root/'docs/integration/knowhub-mcp-portal/evidence/feishu-incremental/linux-joint-release-source/canonical-probe2';dest.mkdir(exist_ok=True)
  with ssh.open_sftp() as ftp:
   for name in ftp.listdir(base+'/canonical-probe2'):ftp.get(base+'/canonical-probe2/'+name,str(dest/name))
  print([(r['name'],r['exit_code']) for r in json.loads((dest/'results.json').read_text())]if(dest/'results.json').exists()else'Running')
 else:
  with ssh.open_sftp() as ftp:
   with ftp.open(base+'/canonical-probe-'+hashlib.sha256(code.encode()).hexdigest()+'.py','w') as f:f.write(code)
  i,o,e=ssh.exec_command('nohup python3 '+shlex.quote(base+'/canonical-probe-'+hashlib.sha256(code.encode()).hexdigest()+'.py')+' >'+shlex.quote(base+'/canonical-probe-launch-'+hashlib.sha256(code.encode()).hexdigest()+'.log')+' 2>&1 </dev/null &',timeout=10)
  assert o.channel.recv_exit_status()==0,e.read().decode();i.close();o.close();e.close();print('Started exact source regression')
