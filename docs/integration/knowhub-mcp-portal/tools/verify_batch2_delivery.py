"""Validate report links, immutable evidence and the exact tested core source."""
import ast
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT=Path(__file__).resolve().parents[4]
BASE=Path(__file__).resolve().parents[1]
OUT=BASE/'evidence/batch2'
reports=sorted(BASE.glob('[0-9][0-9]_*.md'))
assert len(reports)==18,len(reports)
for path in reports:
    text=path.read_text(encoding='utf-8')
    for ref in re.findall(r'\]\(([^)]+)\)',text):
        if '://' not in ref:
            assert (path.parent/ref.split('#')[0]).exists(),(path.name,ref)
for path in (BASE/'tools').glob('*.py'):ast.parse(path.read_text(encoding='utf-8'),filename=str(path))
changes=json.loads((OUT/'change-manifest.json').read_text())
assert not changes['production_changes']
for change in changes['files']:
    assert hashlib.sha256((ROOT/change['path']).read_bytes()).hexdigest()==change['sha256'],change['path']
for name,digest in json.loads((OUT/'artifact-sha256.json').read_text()).items():
    assert hashlib.sha256((OUT/name).read_bytes()).hexdigest()==digest,name
for name,digest in json.loads((OUT/'m0-evidence-sha256.json').read_text()).items():
    assert hashlib.sha256((BASE/'evidence'/name).read_bytes()).hexdigest()==digest,name
last=json.loads((OUT/'linux/last-results.json').read_text())
assert all(x['exit_code']==0 for x in last)
pg=json.loads((OUT/'linux/postgres-results.json').read_text())
assert pg and all(x['status']=='PASS' for x in pg)
assert json.loads((OUT/'linux/environment-final.json').read_text())['task_containers_remaining']==[]
full=(OUT/'linux/go-service-agent-full-fixtures.log').read_text(errors='replace')
failed=re.findall(r'^--- FAIL: (.+?) \(',full,re.MULTILINE)
assert failed==['TestSkillPythonVerifier'],failed
assert re.search(r'^ok\s+github.com/Tencent/WeKnora/internal/agent/tools\s',full,re.MULTILINE)
python=(OUT/'linux/python-full-offline.log').read_text()
assert 'Ran 53 tests' in python and python.rstrip().endswith('OK')
go=[str(ROOT/x['path']) for x in changes['files'] if x['path'].endswith('.go')]
assert subprocess.check_output(['gofmt','-l',*go],cwd=ROOT).strip()==b''
subprocess.run(['git','diff','--check'],cwd=ROOT,check=True)
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip()==changes['base_sha']
assert subprocess.check_output(['git','diff','--cached','--name-only'],cwd=ROOT).strip()==b''
print(json.dumps({'status':'PASS','reports':len(reports),'tested_core_files':len(changes['files']),
    'm0_evidence_preserved':True,'postgresql_migrations_cas_jsonb_read':'PASS',
    'linux_python_tests':53,'remaining_full_service_failure':failed,'staged_files':0}))
