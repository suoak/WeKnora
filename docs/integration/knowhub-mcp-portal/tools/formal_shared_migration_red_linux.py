"""Reproduce the preserved c2 multi-page migration failure in BOTH dialects."""
from pathlib import Path
import hashlib,json,shutil,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());backup=Path(state['backup_path']);ops=backup/'operator-source';art=backup/'operators';tmp=backup/'operator-temp';out=base/'results/migration-red';out.mkdir(exist_ok=False)
source=Path(state['candidate'])/'source'
shutil.copytree(source/'migrations',ops/'migrations')
testpath=ops/'internal/application/repository/feishu_legacy_migration_test.go';testpath.write_bytes((base/'formal_migration_test.go').read_bytes())
pg='knowhub-next-dev-b702755c0333-pg';x=json.loads(subprocess.check_output(['docker','inspect',pg]))[0];assert x['State']['Running']and x['Config']['Labels'].get('knowhub.next')=='development'
args=['docker','run','--rm','--network','container:'+pg,'-v',str(ops)+':/source:ro','-v',str(tmp)+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v','knowhub-mcp-b7-20261021-102200-data:/prior','-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTOOLCHAIN=local','-e','GOTMPDIR=/work','-e','CGO_CFLAGS=-O2 -g -I/work/sqlite-headers','-e','FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable','-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','go','test','-tags','sqlite_fts5','-count=1','-v','./internal/application/repository','-run','^TestFeishuLegacyFormalMigrationChain$']
with(out/'regression-old-c2.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=600).returncode
raw=(out/'regression-old-c2.log').read_text();confirmed=code!=0 and all('--- FAIL: TestFeishuLegacyFormalMigrationChain/'+v in raw for v in ['postgres','sqlite'])and'wiki_mcp_kb_epochs'in raw
r={'exit_code':code,'expected_failure_confirmed':confirmed,'candidate_under_test':'preserved c2 SQL, extended regression fixture','test_sha256':hashlib.sha256(testpath.read_bytes()).hexdigest(),'argv':args,'production_accessed':False,'shared_P3_accessed':False}
(out/'result.json').write_text(json.dumps(r,indent=2));print(json.dumps(r));sys.exit(0 if confirmed else 1)
