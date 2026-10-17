"""Offline synthetic tests only; never attaches to business databases/networks."""
import os,tarfile,shutil
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017')
source=b/'source'
if not source.exists():
 assert shutil.disk_usage(b).free>2*1024**3
 shutil.copytree('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f/source',source,symlinks=True,ignore=shutil.ignore_patterns('.git','node_modules'))
with tarfile.open(b/'overlay.tar.gz') as archive:
 for m in archive.getmembers():
  (source/m.name).resolve().relative_to(source.resolve())
  assert m.isfile() and (m.name.startswith('internal/') or m.name.startswith('migrations/'))
 archive.extractall(source)
for name in ('tools','toolchain','cache'):(b/name).mkdir(exist_ok=True)
script='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
test "$(go version | awk '{print $3}')" = go1.26.7
export SOURCEID_TEST_POSTGRES_DSN='postgres://verified_mock:synthetic-only@127.0.0.1:5432/verified_mock?sslmode=disable'
go test -timeout 10m -p 2 -tags anydoc -race -json -count=1 -skip 'OwnershipScale|JITDistinctRegistrationCapacity' ./internal/config ./internal/application/repository ./internal/application/service ./internal/handler ./internal/middleware ./internal/utils -run 'SourceID|Enrollment|OIDC|AuthToken|SwitchTenant|Login|Logout|GenerateTokens|ValidateToken|RefreshToken'
go test -race -json -count=1 ./internal/database -run 'MigrationNumbersAreUniqueAndPaired|Immutable|Governance|MigrationManifest'
'''
(b/'test.sh').write_text(script)
worker='''import os,pathlib,subprocess,json,datetime,re
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017')
PG='knowhub-email-policy-mock-pg';BUILDER='knowhub-email-policy-test-builder'
def cmd(args):
 q=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=40)
 if q.returncode:raise RuntimeError('controlled_test_command_failed')
 return q.stdout
def state(value):(b/'test-result.json').write_text(json.dumps(value))
out={'result':'RUNNING','source_sha':'728406a6dce9625bbcaeba9d40de69f72710136c','synthetic_only':True,'live_database_access':False,'live_app_changed':False,'real_sid_requests':0}
state(out);passed=[];failed=[];skipped=[];locations=[];owned=[]
try:
 for name in (PG,BUILDER):
  assert subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
 image=json.loads(cmd(['docker','inspect','knowhub-company-test-postgres-1']))[0]['Image']
 cmd(['docker','run','-d','--name',PG,'--network','none','--memory','768m','--log-driver','none','--tmpfs','/var/lib/postgresql/data:rw','-e','PGDATA=/var/lib/postgresql/data/mock','-e','POSTGRES_USER=verified_mock','-e','POSTGRES_PASSWORD=synthetic-only','-e','POSTGRES_DB=verified_mock',image])
 owned.append(PG)
 import time
 for _ in range(30):
  z=subprocess.run(['docker','exec',PG,'pg_isready','-h','127.0.0.1','-U','verified_mock','-d','verified_mock'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  if z.returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('synthetic_postgres_not_ready')
 cache='/home/aqyfzx/ai/knowhub-feishu-personal-20261014'
 args=['docker','run','--rm','--name',BUILDER,'--network','container:'+PG,'--cpus','2','--memory','8g','--log-driver','none','-v',str(b)+':/work:ro','-v',cache+'/tools:/work/tools:ro','-v',cache+'/toolchain:/work/toolchain:ro','-v',cache+'/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/test.sh']
 owned.append(BUILDER)
 z=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 for raw in z.stdout:
  line=raw.decode('utf-8','replace')
  try:v=json.loads(line)
  except ValueError:
   if re.match(r'^(?:internal/|# github.com/|go:)',line):locations.append(line[:500].strip())
   continue
  action=v.get('Action');test=v.get('Test')
  if action in ('pass','fail','skip') and test:
   {'pass':passed,'fail':failed,'skip':skipped}[action].append(test)
   if '/' not in test:out.update(last_completed_test=test,completed_events=len(passed)+len(failed)+len(skipped));state(out)
  if action=='output':
   matches=re.findall(r'[A-Za-z0-9_]+_test.go:[0-9]+',v.get('Output',''))
   if matches:locations.extend(matches)
 z.stdout.close();code=z.wait();out.update(result='PASS' if code==0 else 'FAIL',process_exit=code,passed=passed,failed=failed,skipped=skipped,diagnostic_locations=locations[-100:])
except Exception:out.update(result='FAIL',failure='CONTROLLED_SYNTHETIC_TEST_FAILURE')
finally:
 for name in reversed(owned):subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 out['isolated_containers_removed']=True;out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();state(out)
'''
(b/'worker.py').write_text(worker)
existing=b/'test-result.json'
if existing.exists():assert json.loads(existing.read_text())['result']!='RUNNING'
subprocess.Popen(['python3',str(b/'worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','synthetic_only':True,'live_app_changed':False,'live_database_access':False}))
