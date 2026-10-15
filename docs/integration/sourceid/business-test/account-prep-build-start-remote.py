"""Isolated CLI build/PG tests. Never attaches to the business DB or network."""
import pathlib, subprocess, json, tarfile, shutil, os

root = pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
source = root / 'source'
assert not source.exists()
baseline = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/source')
source.mkdir(mode=0o700)
for name in ('go.mod', 'go.sum'):
    shutil.copy2(baseline / name, source / name)
for name in ('cmd', 'internal', 'third_party', 'config', 'migrations', 'docreader'):
    shutil.copytree(baseline / name, source / name, symlinks=True)
with tarfile.open(root / 'overlay.tar.gz') as archive:
    for item in archive.getmembers():
        (source / item.name).resolve().relative_to(source.resolve())
        assert item.isfile() and not item.issym()
    archive.extractall(source)
(root / 'artifacts').mkdir(mode=0o700)
worker = r"""
import pathlib, subprocess, json, hashlib, datetime
b=pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
old=pathlib.Path('/home/aqyfzx/ai/knowhub-feishu-personal-20261014')
net='oa-accountprep-synthetic-20261016'
pg='oa-accountprep-pg-20261016'
def run(args):
    q=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=40)
    if q.returncode: raise RuntimeError('command_failed')
    return q.stdout
def state(**kw): (b/'status.json').write_text(json.dumps(dict(utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw)))
state(result='RUNNING')
created_net=created_pg=False
try:
    run(['docker','network','create','--internal','--label','knowhub.test=accountprep',net]);created_net=True
    run(['docker','run','-d','--name',pg,'--network',net,'--network-alias','fixture','--label','knowhub.test=accountprep','-e','POSTGRES_USER=fixture','-e','POSTGRES_PASSWORD=synthetic-only','-e','POSTGRES_DB=fixture','--tmpfs','/var/lib/postgresql/data:rw','sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4']);created_pg=True
    script='''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
go version
go test -p 2 -tags anydoc -race -count=1 -v ./internal/accountprep ./cmd/sourceid-test-account
go vet -tags anydoc ./internal/accountprep ./cmd/sourceid-test-account
go build -tags anydoc -o /work/artifacts/sourceid-test-account ./cmd/sourceid-test-account
'''
    (b/'test.sh').write_text(script)
    args=['docker','run','--rm','--name','oa-accountprep-builder-20261016','--cpus','2','--memory','8g','--pids-limit','1024','--network',net,'-e','SOURCEID_TEST_POSTGRES_DSN=postgres://fixture:synthetic-only@fixture:5432/fixture?sslmode=disable','-v',str(b)+':/work','-v',str(old/'tools')+':/work/tools:ro','-v',str(old/'cache')+':/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/test.sh']
    with (b/'artifacts/tests.log').open('wb') as f: q=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=1200)
    if q.returncode: state(result='FAIL',stage='tests_or_build',exit_code=q.returncode)
    else:
        binary=b/'artifacts/sourceid-test-account'
        state(result='PASS',binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),compiler='go1.26.7',test_database='disposable PostgreSQL private Docker network',business_database_accessed=False)
finally:
    if created_pg:run(['docker','rm','-f',pg])
    if created_net:run(['docker','network','rm',net])
"""
(root / 'worker.py').write_text(worker)
with (root / 'worker.log').open('wb') as log:
    process = subprocess.Popen(['python3', str(root / 'worker.py')], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
print(json.dumps({'result': 'STARTED', 'pid': process.pid, 'directory': str(root), 'business_database_accessed': False, 'running_services_modified': False}))
