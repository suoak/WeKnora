"""Copy only this task's logs/manifests; never read credential or production data."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT=Path(__file__).resolve().parents[4]
OUT=ROOT/'docs/integration/knowhub-mcp-portal/evidence/batch2'
config=json.loads(Path('E:/AIData/Codex/knowhub-m2-current.json').read_text())
task=Path(config['local'])
assert task.name.startswith('knowhub-mcp-m2-') and task.parent==Path('E:/AIData/Codex')
linux=OUT/'linux';linux.mkdir(parents=True,exist_ok=True)
for path in (task/'results').iterdir():
    if path.suffix in {'.json','.log'}:shutil.copyfile(path,linux/path.name)
for path in task.glob('manifest*.json'):shutil.copyfile(path,OUT/path.name)
if (task/'first-postgres-results.json').exists():shutil.copyfile(task/'first-postgres-results.json',linux/'first-postgres-results.json')

# Exit codes are the actual observed tool executions; raw PowerShell output is
# copied byte-for-byte (legacy redirection may be UTF-16).
windows=[
 ('knowhub-m2-tests-windows.log',1,'go test ./internal/application/access ./internal/application/repository -run TestEnterprise|TestDocumentFilter|TestDerivedSources -count=1','initial JSON TEXT scanner failure'),
 ('knowhub-m2-debug.log',1,'go test ./internal/application/repository -run ^TestEnterpriseM2Approved -v -count=1','strict projection diagnosis'),
 ('knowhub-m2-tests-windows-retry.log',0,'go test ./internal/application/access ./internal/application/repository -run TestEnterprise|TestDocumentFilter|TestDerivedSources -count=1','fixed TEXT/JSONB projection'),
 ('knowhub-m2-repository-final.log',0,'go test ./internal/application/repository -run TestEnterprise -count=1','final runtime repository tests; isolated PostgreSQL separately on Linux'),
 ('knowhub-m2-middleware-windows.log',0,'go test ./internal/middleware -count=1','TMPDIR/TMP/TEMP regression'),
 ('knowhub-m2-python-windows.log',1,'python -m unittest discover -s mcp-server -p test_mcp_transports.py -v','first standard-library probe waited for streaming body'),
 ('knowhub-m2-python-http-diagnostic.log',0,'python -m unittest test_mcp_transports.HttpStatelessSmokeTest -v','headers-only original contract retained'),
 ('knowhub-m2-python-windows-final.log',0,'python -m unittest discover -s mcp-server -p test_mcp_transports.py -v','14 passed'),
 ('knowhub-m2-database-windows.log',1,'go test ./internal/database -count=1','existing Windows file URL path issue'),
 ('knowhub-m2-database-windows-final.log',0,'go test ./internal/database -count=1','real migrator fresh/legacy/up/down/up and governance passed'),
]
win=OUT/'windows';win.mkdir(exist_ok=True)
records=[]
for name,code,command,note in windows:
    source=Path('E:/AIData/Codex')/name
    assert source.exists(),name
    shutil.copyfile(source,win/name)
    records.append({'log':'windows/'+name,'command':command,'exit_code':code,'status':'PASS' if code==0 else 'FAIL','note':note,'exit_code_provenance':'observed tool execution','sha256':hashlib.sha256(source.read_bytes()).hexdigest()})
(OUT/'windows-results.json').write_text(json.dumps(records,indent=2)+'\n',encoding='utf-8')

paths=set(subprocess.check_output(['git','diff','--name-only'],cwd=ROOT).decode().splitlines())
paths.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard','internal','migrations'],cwd=ROOT).decode().splitlines())
paths=sorted(p for p in paths if p.startswith(('internal/','migrations/','mcp-server/')))
manifest=json.loads((task/'manifest.json').read_text())
changes=[]
for path in paths:
    digest=hashlib.sha256((ROOT/path).read_bytes()).hexdigest()
    assert manifest['final_changed_files'].get(path)==digest,('untested source drift',path)
    changes.append({'path':path,'sha256':digest,'matches_tested_candidate':True})
(OUT/'change-manifest.json').write_text(json.dumps({'base_sha':manifest['base_sha'],'final_overlay_sha256':manifest['final_overlay_sha256'],'files':changes,'production_changes':False},indent=2)+'\n',encoding='utf-8')
# Record the untouched first-batch evidence separately; never regenerate it.
old_hashes={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in OUT.parent.iterdir() if p.is_file()}
old_record=OUT/'m0-evidence-sha256.json'
if old_record.exists():
    assert json.loads(old_record.read_text())==old_hashes,'first-batch evidence changed'
else:
    old_record.write_text(json.dumps(old_hashes,indent=2)+'\n',encoding='utf-8')
# Freeze new raw output hashes without transforming original log bytes.
hashes={p.relative_to(OUT).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in OUT.rglob('*')if p.is_file() and p.name!='artifact-sha256.json'}
(OUT/'artifact-sha256.json').write_text(json.dumps(hashes,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'core_changed_files':len(changes),'windows_executions':len(records),'raw_artifacts':len(hashes),'candidate_matches':True}))
