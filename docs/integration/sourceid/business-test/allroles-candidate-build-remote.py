"""Build committed candidate on pinned offline baseline; never replace live APP."""
import hashlib, os, shutil, tarfile, pathlib, json, subprocess
os.umask(0o077)
base=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d')
sha='035f35e1d388a2f02615d35cf826bbdcc22e56b0'
assert hashlib.sha256((base/'source.tar.gz').read_bytes()).hexdigest()==(base/'archive.sha256').read_text().strip()
source=base/'source'; source.mkdir(mode=0o700)
with tarfile.open(base/'source.tar.gz') as a:
    for m in a.getmembers():
        (source/m.name).resolve().relative_to(source.resolve())
        if m.issym() or m.islnk(): (source/m.name).parent.joinpath(m.linkname).resolve().relative_to(source.resolve())
    a.extractall(source)
old=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
for p in ('third_party/anydoc-go/lib','third_party/duckdb'):
    if (old/'source'/p).exists(): shutil.copytree(old/'source'/p,source/p,dirs_exist_ok=True)
(base/'artifacts').mkdir(mode=0o700)
script='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
test "$(go version | awk '{print $3}')" = go1.26.7
go test -p 2 -tags anydoc -race -count=1 ./internal/application/repository -run 'SourceID.*(AutoEmailBinding|EmailRJGH)'
export VERSION=0.8.0-sourceid-allroles-035f35e1d COMMIT_ID=035f35e1d BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) GO_VERSION=1.26.7
make build-prod GO_BUILD_TAGS=anydoc
cp WeKnora /work/artifacts/WeKnora
'''
(base/'build.sh').write_text(script)
(base/'Dockerfile').write_text('FROM sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1\nCOPY artifacts/WeKnora /app/WeKnora\nLABEL org.opencontainers.image.revision="'+sha+'" knowhub.environment="isolated-legacy-rehearsal"\n')
worker=r'''
import pathlib,subprocess,json,datetime,hashlib
b=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d')
def state(**kw): (b/'status.json').write_text(json.dumps(dict(utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw)))
state(result='RUNNING',stage='build')
with (b/'artifacts/build.log').open('wb') as log:
 q=subprocess.run(['docker','run','--rm','--name','oa-allroles-builder-035f35e1d','--cpus','2','--memory','8g','--network','none','--log-driver','none','-v',str(b)+':/work','-v','/home/aqyfzx/ai/knowhub-feishu-personal-20261014/tools:/work/tools:ro','-v','/home/aqyfzx/ai/knowhub-feishu-personal-20261014/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/build.sh'],stdout=log,stderr=subprocess.STDOUT,timeout=1800)
 if q.returncode: state(result='FAIL',stage='build',exit_code=q.returncode);raise SystemExit(2)
 q=subprocess.run(['docker','build','--pull=false','--network','none','-t','knowhub-allroles/app:035f35e1d','-f',str(b/'Dockerfile'),str(b)],stdout=log,stderr=subprocess.STDOUT,timeout=300)
 if q.returncode: state(result='FAIL',stage='image',exit_code=q.returncode);raise SystemExit(2)
iid=subprocess.check_output(['docker','image','inspect','--format','{{.Id}}','knowhub-allroles/app:035f35e1d']).decode().strip()
state(result='PASS',stage='complete',image_id=iid,revision='035f35e1d388a2f02615d35cf826bbdcc22e56b0',binary_sha256=hashlib.sha256((b/'artifacts/WeKnora').read_bytes()).hexdigest(),live_services_changed=False)
'''
(base/'worker.py').write_text(worker)
with (base/'worker.log').open('wb') as log:
    q=subprocess.Popen(['python3',str(base/'worker.py')],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'result':'STARTED','pid':q.pid,'source_commit':sha,'live_services_changed':False}))
