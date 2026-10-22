"""Compile scoped test data utility; APP binary remains frozen."""
from pathlib import Path
import hashlib,json,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());backup=Path(state['backup_path']);ops=backup/'operator-source';art=backup/'operators';tmp=backup/'operator-temp';out=base/'results/build-seed';out.mkdir(exist_ok=False)
dest=ops/'cmd/formalseed';dest.mkdir(exist_ok=False);(dest/'main.go').write_bytes((base/'formal_seed.go.txt').read_bytes())
args=['docker','run','--rm','--network','none','-v',str(ops)+':/source:ro','-v',str(art)+':/ops','-v',str(tmp)+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v','knowhub-mcp-b7-20261021-102200-data:/prior','-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTOOLCHAIN=local','-e','GOTMPDIR=/work','-e','CGO_CFLAGS=-O2 -g -I/work/sqlite-headers','-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','go','build','-tags','sqlite_fts5','-o','/ops/formal-seed','./cmd/formalseed']
with(out/'build.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=600).returncode
r={'exit_code':code,'argv':args,'test_utility_only':True,'APP_modified':False,'source_sha256':hashlib.sha256((base/'formal_seed.go.txt').read_bytes()).hexdigest()}
if code==0:r['binary_sha256']=hashlib.sha256((art/'formal-seed').read_bytes()).hexdigest()
(out/'result.json').write_text(json.dumps(r,indent=2));print(json.dumps(r));sys.exit(code)
