"""Build exact accepted source using existing offline packaging toolchain."""
import os,hashlib,tarfile,shutil
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');sha='09e6d86db731953834916e58ac5f3f5985ad0aa1'
assert hashlib.sha256((b/'source.tar.gz').read_bytes()).hexdigest()==(b/'archive.sha256').read_text().strip()
s=b/'source';s.mkdir(mode=0o700)
with tarfile.open(b/'source.tar.gz') as a:
 for m in a.getmembers():
  (s/m.name).resolve().relative_to(s.resolve())
  if m.issym() or m.islnk():(s/m.name).parent.joinpath(m.linkname).resolve().relative_to(s.resolve())
 a.extractall(s)
old=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
for p in ('third_party/anydoc-go/lib','third_party/duckdb'):
 if (old/'source'/p).exists():shutil.copytree(old/'source'/p,s/p,dirs_exist_ok=True)
(b/'artifacts').mkdir(mode=0o700)
script='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
test "$(go version | awk '{print $3}')" = go1.26.7
go test -p 2 -tags anydoc -race -count=1 ./cmd/sourceid-ownership ./internal/config ./internal/application/repository ./internal/application/service ./internal/handler -run 'SourceIDEnterprise|SourceIDOwnership|SourceIDAccountPolicy|SourceIDSSO|SourceIDPilot'
export VERSION=0.8.0-enterprise-09e6d86db COMMIT_ID=09e6d86db BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) GO_VERSION=1.26.7
make build-prod GO_BUILD_TAGS=anydoc
cp WeKnora /work/artifacts/WeKnora
go build -tags anydoc -o /work/artifacts/sourceid-ownership ./cmd/sourceid-ownership
cp -a migrations /work/artifacts/migrations
'''
(b/'build-app.sh').write_text(script)
base='sha256:c6e631c631e4488b32f7964e700dae8dfd9105ca4a75f2c0443272f44484c1c5'
run(['docker','image','inspect',base])
run(['docker','image','tag',base,'knowhub-enterprise/base:832b6652a'])
(b/'Dockerfile.app').write_text('FROM knowhub-enterprise/base:832b6652a\nCOPY artifacts/WeKnora /app/WeKnora\nCOPY artifacts/sourceid-ownership /app/sourceid-ownership\nCOPY artifacts/migrations /app/migrations\nLABEL org.opencontainers.image.revision="'+sha+'"\n')
(b/'.dockerignore').write_text('private\nsource\nsource.tar.gz\nartifacts/*.log\n')
worker='''import pathlib,subprocess,json,datetime,traceback,os
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db')
def state(**x):(b/'build-status.json').write_text(json.dumps(dict(utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**x)))
builder='sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb'
cache='/home/aqyfzx/ai/knowhub-feishu-personal-20261014'
try:
 state(result='RUNNING',stage='app_tests_and_build')
 args=['docker','run','--rm','--name','enterprise-candidate-builder','--network','none','--cpus','2','--memory','8g','--log-driver','none','-v',str(b)+':/work','-v',cache+'/tools:/work/tools:ro','-v',cache+'/toolchain:/work/toolchain:ro','-v',cache+'/cache:/work/cache','--entrypoint','bash',builder,'/work/build-app.sh']
 with (b/'artifacts/build-app.log').open('wb') as log:
  q=subprocess.run(args,stdout=log,stderr=subprocess.STDOUT,timeout=2700);assert q.returncode==0
  state(result='RUNNING',stage='immutable_image')
  q=subprocess.run(['docker','build','--pull=false','--network','none','-f',str(b/'Dockerfile.app'),'-t','knowhub-enterprise/app:09e6d86db',str(b)],stdout=log,stderr=subprocess.STDOUT,timeout=300);assert q.returncode==0
 image=json.loads(subprocess.check_output(['docker','image','inspect','knowhub-enterprise/app:09e6d86db']))[0]
 revision=image['Config']['Labels']['org.opencontainers.image.revision'];assert revision=='09e6d86db731953834916e58ac5f3f5985ad0aa1'
 state(result='PASS',stage='complete',image_id=image['Id'],revision=revision,builder=builder,compiler='go1.26.7',network='none',frontend_rebuilt=False,live_service_changed=False)
except Exception as e:state(result='FAIL',failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
'''
(b/'build-worker.py').write_text(worker)
with (b/'worker.log').open('wb') as log:p=subprocess.Popen(['python3',str(b/'build-worker.py')],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'result':'STARTED','pid':p.pid,'sha':sha,'compiler':'go1.26.7','network':'none','live_changed':False}))
