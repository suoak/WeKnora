"""Build committed candidate on pinned offline baseline; never replace live APP."""
import hashlib, os, shutil, tarfile, pathlib, json, subprocess
os.umask(0o077)
base=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
sha='479416c9086d91de4484dbbd2cc5f912cdcdb697'
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

# Resolve a verified local image rather than a Docker Hub sha256 repository.
subprocess.run(['docker','image','tag','sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286','knowhub-prompt-login/base:035f35e1d'],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
assert subprocess.check_output(['docker','image','inspect','--format','{{.Id}}','knowhub-prompt-login/base:035f35e1d']).decode().strip()=='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'

script='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
test "$(go version | awk '{print $3}')" = go1.26.7
go test -p 2 -tags anydoc -race -count=1 ./internal/config ./internal/application/service ./internal/handler ./internal/application/repository ./internal/middleware ./internal/utils -run 'SourceID|OIDC|AuthToken|SwitchTenant|Login|Logout|GenerateTokens|ValidateToken|RefreshToken'
export VERSION=0.8.0-sourceid-prompt-login-479416c90 COMMIT_ID=479416c90 BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) GO_VERSION=1.26.7
make build-prod GO_BUILD_TAGS=anydoc
cp WeKnora /work/artifacts/WeKnora
'''
(base/'build.sh').write_text(script)
(base/'Dockerfile').write_text('FROM knowhub-prompt-login/base:035f35e1d\nCOPY artifacts/WeKnora /app/WeKnora\nLABEL org.opencontainers.image.revision="'+sha+'" knowhub.environment="prompt-login-candidate-not-deployed"\n')
worker=r'''
import pathlib,subprocess,json,datetime,hashlib
b=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
def state(**kw): (b/'status.json').write_text(json.dumps(dict(utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw)))
state(result='RUNNING',stage='build')
with (b/'artifacts/build.log').open('wb') as log:
 q=subprocess.run(['docker','run','--rm','--name','oa-prompt-login-builder-479416c90','--cpus','2','--memory','8g','--network','none','--log-driver','none','-v',str(b)+':/work','-v','/home/aqyfzx/ai/knowhub-feishu-personal-20261014/tools:/work/tools:ro','-v','/home/aqyfzx/ai/knowhub-feishu-personal-20261014/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/build.sh'],stdout=log,stderr=subprocess.STDOUT,timeout=1800)
 if q.returncode: state(result='FAIL',stage='build',exit_code=q.returncode);raise SystemExit(2)
 q=subprocess.run(['docker','build','--pull=false','--network','none','-t','knowhub-prompt-login/app:479416c90','-f',str(b/'Dockerfile'),str(b)],stdout=log,stderr=subprocess.STDOUT,timeout=300)
 if q.returncode: state(result='FAIL',stage='image',exit_code=q.returncode);raise SystemExit(2)
iid=subprocess.check_output(['docker','image','inspect','--format','{{.Id}}','knowhub-prompt-login/app:479416c90']).decode().strip()
state(result='PASS',stage='complete',image_id=iid,revision='479416c9086d91de4484dbbd2cc5f912cdcdb697',binary_sha256=hashlib.sha256((b/'artifacts/WeKnora').read_bytes()).hexdigest(),live_services_changed=False)
'''
(base/'worker.py').write_text(worker)
with (base/'worker.log').open('wb') as log:
    q=subprocess.Popen(['python3',str(base/'worker.py')],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'result':'STARTED','pid':q.pid,'source_commit':sha,'live_services_changed':False}))
