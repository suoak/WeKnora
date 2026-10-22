"""Create a credential-free handoff archive for this exact frozen candidate."""
from pathlib import Path
import hashlib,json,sys,tarfile

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021')
m=json.loads((base/'manifest.json').read_text());assert base.name==m['candidate_sha256'][:16]
assert json.loads((base/'results/build/done.json').read_text())['all_passed']
out=base/'results/package-v2';out.mkdir(exist_ok=False)
archive=base/'handoff-v2.tar.gz'
assert not archive.exists()
entries={
 'manifest.json':base/'manifest.json',
 'source.tar.gz':base/'source.tar.gz',
 'APP':base/'artifacts/WeKnora',
 'frontend':base/'artifacts/dist',
 'migrations':base/'source/migrations',
 'config':base/'source/config',
 'scripts':base/'image-context/scripts',
 'compose.test.yml':base/'deployment-compose.yml',
 'images.json':base/'results/build/images.json',
 'artifacts.json':base/'results/build/artifacts.json',
 'image-context.json':base/'results/build/image-context.json',
}
files={}
with tarfile.open(archive,'w:gz',compresslevel=3)as tar:
 for name,p in entries.items():
  tar.add(p,arcname=name)
  for f in ([p]if p.is_file()else p.rglob('*')):
   if f.is_file():files[name if p.is_file()else name+'/'+str(f.relative_to(p))]=hashlib.sha256(f.read_bytes()).hexdigest()
seen=set()
with tarfile.open(archive,'r|gz')as tar:
 for member in tar:
  if member.isfile():
   assert hashlib.sha256(tar.extractfile(member).read()).hexdigest()==files[member.name],member.name
   seen.add(member.name)
assert seen==set(files)
record={'candidate_sha256':m['candidate_sha256'],'archive':str(archive),'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'files':files,'readback_verified':True,'contains_credentials':False,'images':'Local built image IDs in images.json; runtime base dependencies remain required.'}
(out/'package.json').write_text(json.dumps(record,indent=2))
print(json.dumps({k:v for k,v in record.items()if k!='files'}))
