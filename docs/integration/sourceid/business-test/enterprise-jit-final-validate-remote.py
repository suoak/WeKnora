"""New JIT candidate, isolated synthetic test DB. No APP deployment."""
import os,hashlib,tarfile,shutil
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');sha='f40b1602f69ce5d88e1e87dc3cefae36e9bde356'
assert hashlib.sha256((b/'source.tar.gz').read_bytes()).hexdigest()==(b/'archive.sha256').read_text().strip()
s=b/'source';s.mkdir(mode=0o700)
with tarfile.open(b/'source.tar.gz') as a:
 for m in a.getmembers():
  (s/m.name).resolve().relative_to(s.resolve())
  if m.issym() or m.islnk():(s/m.name).parent.joinpath(m.linkname).resolve().relative_to(s.resolve())
 a.extractall(s)
old=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
for name in ('third_party/anydoc-go/lib','third_party/duckdb'):
 if (old/'source'/name).exists():shutil.copytree(old/'source'/name,s/name,dirs_exist_ok=True)
(b/'private').mkdir(mode=0o700)
pg='knowhub-oa-sso-pilot-postgres-1';db='enterprise_jit_validation'
assert run(['docker','exec',pg,'psql','-X','-U','clean_owner','-d','postgres','-qAt','-c',"SELECT COUNT(*) FROM pg_database WHERE datname='"+db+"'"]).decode().strip()=='0'
run(['docker','exec',pg,'createdb','-U','clean_owner',db])
pw=dict(x.split('=',1) for x in (p/'private/postgres.env').read_text().splitlines())['POSTGRES_PASSWORD']
(b/'private/test.env').write_text('SOURCEID_TEST_POSTGRES_DSN=postgres://clean_owner:'+urllib.parse.quote(pw,safe='')+'@127.0.0.1:5432/'+db+'?sslmode=disable\n');pw=None
shell='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
go test -p 2 -tags anydoc -race -v -count=1 ./internal/config ./internal/application/repository ./internal/application/service ./internal/handler -run 'SourceIDJIT|SourceIDPostgresJIT|SourceIDEnterpriseScope'
go test ./internal/database -run 'MigrationNumbersAreUniqueAndPaired|Immutable|Governance|MigrationManifest' -count=1
go vet -tags anydoc ./internal/application/repository ./internal/application/service ./internal/config
go build -tags anydoc -o /work/private/knowhub-jit-server ./cmd/server
'''
(b/'test.sh').write_text(shell)
worker='''import pathlib,subprocess,json,datetime,traceback,os,re
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');cache='/home/aqyfzx/ai/knowhub-feishu-personal-20261014'
out={'result':'RUNNING','code_sha':'f40b1602f69ce5d88e1e87dc3cefae36e9bde356','real_sid_called':False,'app_deployed':False,'live_database_migrated':False}
(b/'validation-result.json').write_text(json.dumps(out))
try:
 with (b/'private/test.log').open('wb') as log:
  q=subprocess.run(['docker','run','--rm','--name','enterprise-jit-validator','--network','container:knowhub-oa-sso-pilot-postgres-1','--cpus','2','--memory','6g','--log-driver','none','--env-file',str(b/'private/test.env'),'-v',str(b)+':/work','-v',cache+'/tools:/work/tools:ro','-v',cache+'/toolchain:/work/toolchain:ro','-v',cache+'/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/test.sh'],stdout=log,stderr=subprocess.STDOUT,timeout=900)
 log=(b/'private/test.log').read_text(errors='replace')
 out['passed_tests']=re.findall(r'^--- PASS: ([A-Za-z0-9_/.-]+)',log,re.M)
 out['failed_tests']=re.findall(r'^--- FAIL: ([A-Za-z0-9_/.-]+)',log,re.M)
 out['capacity']=[{'registrations':int(n),'concurrent':int(c),'duration_ms':int(d),'p95_ms':int(p)} for n,c,d,p in re.findall(r'synthetic_jit_distinct=(\\d+) concurrent=(\\d+) duration_ms=(\\d+) p95_ms=(\\d+)',log)]
 out['result']='PASS' if q.returncode==0 else 'FAIL'
except Exception as e:out.update(result='FAIL',failure_class=type(e).__name__)
finally:
 z=subprocess.run(['docker','exec','knowhub-oa-sso-pilot-postgres-1','dropdb','-U','clean_owner','enterprise_jit_validation'],stdout=subprocess.PIPE,stderr=subprocess.PIPE);out['disposable_database_removed']=z.returncode==0
 out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(b/'validation-result.json').write_text(json.dumps(out,indent=2))
'''
(b/'validation-worker.py').write_text(worker)
with (b/'private/worker.log').open('wb') as f:z=subprocess.Popen(['python3',str(b/'validation-worker.py')],stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'result':'STARTED','pid':z.pid,'real_authentication':False,'app_changed':False,'live_database_migrated':False}))
