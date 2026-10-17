"""Exact approved CI source; offline candidate image only, never deploys."""
import os,hashlib,tarfile,shutil
os.umask(0o077)
SHA='5f988be51779f1a22bd9d027b7503fe6a8749f62'
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
receipt=json.loads((B/'source-receipt.json').read_text())
ci=json.loads((B/'ci-approved.json').read_text())
assert ci['source_sha']==SHA and ci['result']=='PASS' and ci['jobs']==dict.fromkeys(('linux','postgres','frontend','browser'),'success')
assert receipt['source_revision']==SHA and hashlib.sha256((B/'source.tar.gz').read_bytes()).hexdigest()==receipt['archive_sha256']
source=B/'source';source.mkdir(mode=0o700,exist_ok=True)
with tarfile.open(B/'source.tar.gz') as archive:
 for member in archive.getmembers():
  (source/member.name).resolve().relative_to(source.resolve())
  if member.issym() or member.islnk():(source/member.name).parent.joinpath(member.linkname).resolve().relative_to(source.resolve())
 archive.extractall(source)
original=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f/source')
for name in ('third_party/anydoc-go/lib','third_party/duckdb'):
 if (original/name).exists():shutil.copytree(original/name,source/name,dirs_exist_ok=True)
for name in ('artifacts','private','tools','toolchain','cache'):(B/name).mkdir(mode=0o700,exist_ok=True)
(B/'build.sh').write_text('''set -eu
cd /work/source
export PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
test "$(go version | awk '{print $3}')" = go1.26.7
export VERSION=0.8.0-verified-email-5f988be51 COMMIT_ID=5f988be51 BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) GO_VERSION=1.26.7
make build-prod GO_BUILD_TAGS=anydoc
cp WeKnora /work/artifacts/WeKnora
cp -a migrations /work/artifacts/migrations
''')
(B/'Dockerfile').write_text('FROM knowhub-verified-email/base:f40b1602f\nCOPY artifacts/WeKnora /app/WeKnora\nCOPY artifacts/migrations /app/migrations\nLABEL org.opencontainers.image.revision="'+SHA+'"\n')
(B/'.dockerignore').write_text('source\nsource.tar.gz\nsource-receipt.json\nci-approved.json\nprivate\nmonitor\ntools\ntoolchain\ncache\n')
worker='''import os,subprocess,pathlib,json,datetime,hashlib
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');SHA='5f988be51779f1a22bd9d027b7503fe6a8749f62'
def run(args,timeout=120):
 z=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_candidate_build_failed')
 return z.stdout
def state(v):(B/'build-result.json').write_text(json.dumps(v))
out={'result':'RUNNING','source_sha':SHA,'live_app_changed':False,'live_database_written':False,'real_sid_requests':0};state(out)
try:
 base='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
 run(['docker','image','tag',base,'knowhub-verified-email/base:f40b1602f'])
 cache='/home/aqyfzx/ai/knowhub-feishu-personal-20261014'
 args=['docker','run','--rm','--name','knowhub-verified-email-candidate-builder','--network','none','--cpus','2','--memory','8g','--log-driver','none','-v',str(B)+':/work','-v',cache+'/tools:/work/tools:ro','-v',cache+'/toolchain:/work/toolchain:ro','-v',cache+'/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/build.sh']
 with (B/'private/build.log').open('wb') as log:
  z=subprocess.run(args,stdout=log,stderr=subprocess.STDOUT,timeout=1800);assert z.returncode==0
  z=subprocess.run(['docker','build','--pull=false','--network','none','-f',str(B/'Dockerfile'),'-t','knowhub-verified-email/app:5f988be51',str(B)],stdout=log,stderr=subprocess.STDOUT,timeout=300);assert z.returncode==0
 image=json.loads(run(['docker','image','inspect','knowhub-verified-email/app:5f988be51']))[0]
 assert image['Config']['Labels']['org.opencontainers.image.revision']==SHA
 gates={'SOURCEID_ACCOUNT_LINK_MODE':'trusted_ownership','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'false','SOURCEID_VERIFIED_EMAIL_POLICY_REF':''}
 (B/'app-pin.json').write_text(json.dumps({'services':{'app':{'image':image['Id'],'environment':gates}}}))
 (B/'app-rollback-pin.json').write_text(json.dumps({'services':{'app':{'image':base,'environment':gates}}}))
 out.update(result='PASS',image_id=image['Id'],oci_revision=SHA,tag='knowhub-verified-email/app:5f988be51',schema_target=3030,base_image_id=base,builder_image_id='sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb',go_version='1.26.7',archive_sha256=json.loads((B/'source-receipt.json').read_text())['archive_sha256'],binary_sha256=hashlib.sha256((B/'artifacts/WeKnora').read_bytes()).hexdigest(),pull_performed=False,offline_build=True,candidate_pin_prepared=True,rollback_pin_prepared=True)
except Exception:out.update(result='FAIL',failure='CONTROLLED_CANDIDATE_BUILD_FAILURE')
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();state(out)
'''
(B/'build-worker.py').write_text(worker)
if (B/'build-result.json').exists():assert json.loads((B/'build-result.json').read_text())['result']!='RUNNING'
subprocess.Popen(['python3',str(B/'build-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','source_sha':SHA,'live_app_changed':False}))
