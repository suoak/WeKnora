"""Immutable development snapshots; no APP deployment or production access."""
from pathlib import Path
import hashlib,json,shlex,subprocess,sys,tarfile
from v1_host import connect
from next_test_environment import docker_hosts
root=Path(__file__).resolve().parents[4]
assert root.name=='knowhub-v1-wiki-dynamic-20261022'
out=root/'docs/integration/knowhub-mcp-portal/evidence/v1-wiki-dynamic'
if sys.argv[1]=='collect':
 c=json.loads((out/'development-current.json').read_text())
 with connect()as ssh:
  with ssh.open_sftp()as f:
   dest=out/('development-'+c['source_sha256'][:16]);dest.mkdir(exist_ok=True)
   resultdir=c.get('results_dir','results')
   if resultdir!='results':dest=dest/resultdir;dest.mkdir(exist_ok=True)
   for n in f.listdir(c['remote']+'/'+resultdir):
    if n.endswith(('.json','.log','.txt')):f.get(c['remote']+'/'+resultdir+'/'+n,str(dest/n))
  if(dest/'done.json').exists():print((dest/'done.json').read_text())
  else:print('RUNNING')
 raise SystemExit(0)
if sys.argv[1]=='resume-ready':
 c=json.loads((out/'development-current.json').read_text())
 with connect()as ssh:
  with ssh.open_sftp()as f:
   with f.open(c['remote']+'/runner.py')as h:s=h.read().decode()
  attempt=c.get('environment_attempt',0)+1
  resultdir='results-environment-'+str(attempt)
  s=s.replace("out=base/'results'","out=base/"+repr(resultdir))
  s=s.replace("commands=[('prepare'", "commands=[('prepare'")
  a=s.index("  code=execute('create-disposable-pg'")
  b=s.index('  for _ in range(60):',a)
  s=s[:a]+s[b:]
  s=s.replace("pg,'pg_isready','-U','postgres'", "pg,'pg_isready','-h','127.0.0.1','-U','postgres','-d','feishu_atomic_test'")
  s=s.replace('len(commands)+2+len(follow)','len(commands)+1+len(follow)')
  s=s.replace('len(commands)+2+len(formalDBs)+len(follow)','len(commands)+1+len(formalDBs)+len(follow)')
  s=s.replace("commands=[('prepare'", "common[2:2]="+repr(docker_hosts())+"\ncommands=[('prepare'")
  s=s.replace("pgcommon=list(common);pgcommon[pgcommon.index('none')]='container:'+pg", "pgcommon=list(common)\n  while '--add-host' in pgcommon:\n   hostidx=pgcommon.index('--add-host');del pgcommon[hostidx:hostidx+2]\n  pgcommon[pgcommon.index('none')]='container:'+pg")
  digest=hashlib.sha256(s.encode()).hexdigest();path=c['remote']+'/ready-retry-'+digest+'.py'
  with ssh.open_sftp()as f:
   with f.open(path,'w')as h:h.write(s)
  _,o,e=ssh.exec_command('nohup python3 '+shlex.quote(path)+' > '+shlex.quote(path+'.log')+' 2>&1 </dev/null &');assert o.channel.recv_exit_status()==0;e.close();o.close()
 c['results_dir']=resultdir
 c['environment_attempt']=attempt
 (out/'development-current.json').write_text(json.dumps(c,indent=2),encoding='utf8')
 (out/('ready-retry-'+digest+'.json')).write_text(json.dumps({'source_sha256':c['source_sha256'],'harness_sha256':digest,'reason':'final TCP readiness and container-local public-domain fixtures; network remains none; original failed logs preserved'},indent=2))
 print('same-source ready retry started');raise SystemExit(0)
names=set(subprocess.check_output(['git','ls-files','--modified','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0'))
files={n:hashlib.sha256((root/n).read_bytes()).hexdigest()for n in sorted(names)if(root/n).is_file()and not n.startswith('docs/integration/knowhub-mcp-portal/')}
sha=hashlib.sha256(''.join(n+'\0'+h+'\n'for n,h in files.items()).encode()).hexdigest()
remote='/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/'+sha[:16]
archive=Path('E:/AIData/Codex')/('knowhub-next-dev-'+sha+'.tar.gz')
with tarfile.open(archive,'w:gz')as t:
 for n in files:t.add(root/n,arcname=n,recursive=False)
c={'source_sha256':sha,'files':files,'remote':remote,'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest()}
runner=r'''
from pathlib import Path
import json,subprocess,time
base=Path(BASE);out=base/'results';out.mkdir(exist_ok=False)
sha=json.loads((base/'manifest.json').read_text())['source_sha256']
volume='knowhub-next-development-'+sha[:12]
socketvolume=volume+'-socket'
legacysocket='/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1'
common=['docker','run','--rm','--network','none','--name','knowhub-next-development-'+sha[:12],
 '-v',str(base/'source')+':/source:ro','-v',str(out)+':/results',
 '-v',volume+':/work','-v',socketvolume+':'+legacysocket,'-v','feishu-incremental-1d533edfe339:/baseline-work:ro',
 '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
 '-v','/home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro',
 '-v','knowhub-mcp-b7-20261021-102200-data:/prior',
 '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTMPDIR=/work/tmp',
 '-e','GOTOOLCHAIN=local','-e','CGO_CFLAGS=-O2 -g -I/work/sqlite-headers','-e','CGO_LDFLAGS=-L/native',
 '-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022']
common[2:2]=PUBLIC_HOST_ARGS
commands=[('prepare', ['sh','-c','mkdir -p /work/tmp && cp -R /baseline-work/sqlite-headers /work/sqlite-headers']),
 ('focused', ['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v',
 './internal/application/service','./internal/application/repository','./internal/mcpserver','./internal/middleware','./internal/handler',
 '-run','TestV1Next|TestPortalM7|TestPortalM6PersonalDefaultClosed'])]
rows=[]
pg='knowhub-next-dev-'+sha[:12]+'-pg'
def execute(name,argv):
 start=time.time()
 with(out/(name+'.log')).open('wb')as f:code=subprocess.run(argv,stdout=f,stderr=subprocess.STDOUT,timeout=1800).returncode
 rows.append({'name':name,'argv':argv,'exit_code':code,'seconds':time.time()-start})
 (out/'results.json').write_text(json.dumps(rows,indent=2))
 return code
try:
 for name,args in commands:
  code=execute(name,common+args)
  if code:break
 if all(r['exit_code']==0 for r in rows):
  code=execute('create-disposable-pg',['docker','run','-d','--name',pg,'--label','knowhub.next=development','--network','none','-v',pg+'-data:/var/lib/postgresql/data','-v',socketvolume+':/var/run/postgresql','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_DB=feishu_atomic_test','paradedb/paradedb:v0.22.2-pg17'])
  assert code==0
  for _ in range(60):
   if subprocess.run(['docker','exec',pg,'pg_isready','-h','127.0.0.1','-U','postgres','-d','feishu_atomic_test'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(1)
  else:raise RuntimeError('own PG did not become ready')
  assert execute('pg-version',['docker','exec',pg,'psql','-U','postgres','-d','feishu_atomic_test','-Atc',"SELECT version();SELECT extname,extversion FROM pg_extension WHERE extname IN ('pg_search','vector');"])==0
  formalDBs=['knowhub_m2_test','knowhub_m3_chain','knowhub_m3_fresh','knowhub_m3_empty','knowhub_m3_failure']
  for name in formalDBs:
   assert execute('create-tagged-'+name,['docker','exec',pg,'createdb','-U','postgres',name])==0
  pgcommon=list(common)
  while '--add-host' in pgcommon:
   hostidx=pgcommon.index('--add-host');del pgcommon[hostidx:hostidx+2]
  pgcommon[pgcommon.index('none')]='container:'+pg
  idx=pgcommon.index('-w');pgcommon[idx:idx]=['-e','FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable','-e','KNOWHUB_M2_TEST_POSTGRES_DSN=host='+legacysocket+' user=postgres dbname=knowhub_m2_test sslmode=disable']
  follow=[('new-real-PG',['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/router','-run','^TestV1NextPostgres']),
   ('new-real-PG-race',['go','test','-race','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/router','-run','^TestV1NextPostgres']),
   ('formal-migrations',['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/application/repository','-run','^TestFeishuLegacyFormalMigrationChain$']),
   ('formal-tagged-migration-race',['go','test','-race','-tags','sqlite_fts5,knowhub_m3_postgres','-p','2','-count=1','-v','./internal/application/repository','-run','^TestEnterpriseM2Postgres|^TestDocumentM3Postgres.*MigrationRunner$']),
   ('database',['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/database']),
   ('race-new',['go','test','-race','-tags','sqlite_fts5','-p','2','-count=1','./internal/application/service','./internal/mcpserver','./internal/application/repository','-run','TestV1Next']),
   ('vet',['go','vet','-tags','sqlite_fts5','./internal/application/access','./internal/application/repository','./internal/application/service','./internal/mcpserver','./internal/middleware','./internal/router']),
   ('full-go',['go','test','-tags','sqlite_fts5','-p','2','-count=1','./...']),
   ('build',['go','build','-tags','sqlite_fts5','-o','/work/WeKnora','./cmd/server']),
   ('seed-compile',['go','test','-tags','sqlite_fts5,knowhub_v1_deployed','-c','-o','/work/seed.test','./internal/router'])]
  for name,args in follow:
   # Package-wide unit runs must not share a destructive PG fixture across
   # packages. Real PG assertions above run serially against the owned DB;
   # conditional integration skips in this complete unit run remain visible.
   if execute(name,(common if name=='full-go' else pgcommon)+args):break
  completed=len(rows)==len(commands)+2+len(formalDBs)+len(follow)
 else:completed=False
 (out/'done.json').write_text(json.dumps({'source_sha256':sha,'all_passed':completed and all(r['exit_code']==0 for r in rows)},indent=2))
except Exception as e:(out/'failure.json').write_text(json.dumps({'error':str(e),'type':type(e).__name__}));raise
'''.replace('BASE',repr(remote)).replace('PUBLIC_HOST_ARGS',repr(docker_hosts()))
with connect()as ssh:
 command='mkdir -p '+shlex.quote(remote.rsplit('/',1)[0])+' && mkdir '+shlex.quote(remote)
 _,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0,e.read().decode()
 with ssh.open_sftp()as f:
  f.put(str(archive),remote+'/source.tar.gz')
  for n,data in [('manifest.json',json.dumps(c)),('runner.py',runner)]:
   with f.open(remote+'/'+n,'w')as h:h.write(data)
 command='mkdir '+shlex.quote(remote+'/source')+' && tar -xzf '+shlex.quote(remote+'/source.tar.gz')+' -C '+shlex.quote(remote+'/source')+' && nohup python3 '+shlex.quote(remote+'/runner.py')+' > '+shlex.quote(remote+'/runner.log')+' 2>&1 </dev/null &'
 _,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0,e.read().decode()
(out/'development-current.json').write_text(json.dumps(c,indent=2),encoding='utf8')
(out/('development-'+sha+'.json')).write_text(json.dumps(c,indent=2),encoding='utf8')
print(json.dumps({'source_sha256':sha,'remote':remote,'stage':'focused development checks, not deployment acceptance'}))
