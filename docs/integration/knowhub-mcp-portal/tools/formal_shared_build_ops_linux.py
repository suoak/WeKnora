"""Compile a test-only operator against frozen migrator/AES source."""
from pathlib import Path
import hashlib,json,shutil,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
statepath=base/'private/state.json'
state=json.loads(statepath.read_text())if statepath.exists()else{'backup_path':'/mnt/sata0/knowhub-formal-c2-'+base.name+'-a3','candidate':'/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb'}
backup=Path(state['backup_path']);assert backup.parent==Path('/mnt/sata0')and backup.is_dir()
out=base/'results/build-ops';out.mkdir(exist_ok=False)
candidate=Path(state['candidate']);manifest=json.loads((candidate/'manifest.json').read_text())
ops=backup/'operator-source';ops.mkdir(exist_ok=False)
for name in ['go.mod','go.sum']:shutil.copy2(candidate/'source'/name,ops/name)
for name in ['internal','third_party']:shutil.copytree(candidate/'source'/name,ops/name)
dest=ops/'cmd/formaloperator';dest.mkdir(parents=True);shutil.copy2(base/'formal_operator.go.txt',dest/'main.go')
art=backup/'operators';art.mkdir();tmp=backup/'operator-temp';tmp.mkdir()
header=tmp/'sqlite-headers';header.mkdir();shutil.copy2('/home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3-binding.h',header/'sqlite3.h');shutil.copy2('/home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3ext.h',header/'sqlite3ext.h')
args=['docker','run','--rm','--network','none','-v',str(ops)+':/source:ro','-v',str(art)+':/ops','-v',str(tmp)+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v','knowhub-mcp-b7-20261021-102200-data:/prior','-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTOOLCHAIN=local','-e','GOTMPDIR=/work','-e','CGO_CFLAGS=-O2 -g -I/work/sqlite-headers','-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','go','build','-tags','sqlite_fts5','-o','/ops/formal-operator','./cmd/formaloperator']
with(out/'build.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=600).returncode
r={'exit_code':code,'argv':args,'candidate_sha256':manifest['candidate_sha256'],'production_accessed':False,'business_APP_changed':False,'operator_source_sha256':hashlib.sha256((base/'formal_operator.go.txt').read_bytes()).hexdigest()}
if code==0:r['operator_sha256']=hashlib.sha256((art/'formal-operator').read_bytes()).hexdigest()
(out/'result.json').write_text(json.dumps(r,indent=2));print(json.dumps(r));sys.exit(code)
