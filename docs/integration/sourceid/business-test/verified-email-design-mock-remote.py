"""Unchanged f40 source + synthetic design probes. No network or live DB."""
import os,shutil,hashlib,tarfile
os.umask(0o077)
base=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-design-20261017')
source=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f/source')
assert (source/'go.mod').is_file()
archive=source.parent/'source.tar.gz'
assert hashlib.sha256(archive.read_bytes()).hexdigest()==(source.parent/'archive.sha256').read_text().strip()
with tarfile.open(archive) as tree:
 for name in ('internal/application/repository/sourceid_binding.go','internal/application/repository/sourceid_enterprise.go','internal/application/repository/sourceid_jit.go','internal/application/service/sourceid.go','internal/application/service/sourceid_local_session.go','internal/config/sourceid_enterprise.go'):
  assert tree.extractfile(name).read()==(source/name).read_bytes()
target=base/'source-v3';base.mkdir(mode=0o700,exist_ok=True)
fresh=not target.exists()
# Independent source copy; no existing file is edited.
assert shutil.disk_usage(base).free > 2*1024**3
if fresh:shutil.copytree(source,target,symlinks=True,ignore=shutil.ignore_patterns('.git','node_modules'))
test=base/'sourceid_verified_email_design_test.go'
shutil.copyfile(test,target/'internal/application/repository/sourceid_verified_email_design_test.go')
cache=pathlib.Path('/home/aqyfzx/ai/knowhub-feishu-personal-20261014')
for name in ('tools','toolchain','cache'):(base/name).mkdir(exist_ok=True)
script='''set -eu
cd /work/source-v3
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
echo STAGE_TOOLCHAIN
go version
test "$(go version | awk '{print $3}')" = go1.26.7
echo STAGE_FORMAT
test -z "$(gofmt -l internal/application/repository/sourceid_verified_email_design_test.go)"
echo STAGE_TESTS
go test -p 2 -tags anydoc -race -json -count=1 ./internal/application/repository -run 'SourceIDVerifiedEmailDesign|SourceIDAutoEmailBinding|SourceIDEmailRJGH'
'''
(base/'test.sh').write_text(script)
args=['docker','run','--rm','--name','knowhub-verified-email-design-test','--network','none','--cpus','2','--memory','6g','--log-driver','none','-v',str(base)+':/work:ro','-v',str(cache/'tools')+':/work/tools:ro','-v',str(cache/'toolchain')+':/work/toolchain:ro','-v',str(cache/'cache')+':/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/test.sh']
z=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=300)
events=[]
for line in z.stdout.splitlines():
 try:events.append(json.loads(line))
 except ValueError:pass
safe_stages=[line.decode() for line in z.stdout.splitlines() if line.startswith(b'STAGE_') or line.startswith(b'go version ')]
out={'safe_stages':safe_stages,'process_exit':z.returncode,'runner_failure_class':[label for marker,label in [('read-only file system','READ_ONLY_MOUNT'),('permission denied','MOUNT_PERMISSION'),('already in use','NAME_IN_USE'),('No such image','MISSING_BUILDER_IMAGE'),('no space left','DISK_CAPACITY'),('mount','MOUNT_ERROR'),('Cannot connect','DOCKER_UNAVAILABLE')] if z.returncode==125 and marker in z.stderr.decode('utf-8','replace')],'result':'PASS' if z.returncode==0 else 'FAIL','source':'f40b1602f69ce5d88e1e87dc3cefae36e9bde356','baseline_archive_and_core_verified':True,'synthetic_only':True,'network':'none','live_database_access':False,'live_app_changed':False,'real_sid_requests':0,'production_adapter_implemented':False,'tests_passed':[x['Test'] for x in events if x.get('Action')=='pass' and 'Test' in x],'tests_failed':[x['Test'] for x in events if x.get('Action')=='fail' and 'Test' in x],'probe_sha256':hashlib.sha256(test.read_bytes()).hexdigest(),'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(base/'result.json').write_text(json.dumps(out))
print(json.dumps(out))
