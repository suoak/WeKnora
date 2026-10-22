"""Evidence consistency, not a substitute for functional acceptance."""
from pathlib import Path
import ast, datetime, hashlib, json, re, subprocess

ROOT = Path(__file__).resolve().parents[4]
OUT = ROOT / 'docs/integration/knowhub-mcp-portal/evidence/batch7/published'
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
rows = []
for done in sorted((OUT/'linux').glob('*/done.json')):
    phase = done.parent.name.rsplit('-', 1)[-1]
    completion = json.loads(done.read_text())
    digest = completion['candidate_sha256']
    manifest = json.loads((OUT/'candidates'/(digest+'.json')).read_text())
    checks = json.loads((done.parent/'results.json').read_text())
    # Reports/tools can evolve after a frozen run; runtime source may not.
    mismatches = [n for n,h in manifest['files'].items()
                  if (n.startswith(('internal/', 'cmd/', 'migrations/')) or n in ('go.mod','go.sum'))
                  and (not (ROOT/n).is_file() or sha(ROOT/n) != h)]
    fixture_adjustments = []
    fixture_path = 'internal/router/portal_mcp_client_fixture_test.go'
    if fixture_path in mismatches:
        # Only manual-client credential/deadline budgets changed. These tests
        # are excluded from ordinary core/service/race builds by their tag.
        previous = (ROOT/fixture_path).read_bytes().replace(
            b'Add(40 * time.Minute)', b'Add(20 * time.Minute)').replace(
            b'Add(30 * time.Minute)', b'Add(15 * time.Minute)')
        assert hashlib.sha256(previous).hexdigest() == manifest['files'][fixture_path]
        mismatches.remove(fixture_path)
        fixture_adjustments.append('manual tagged fixture only: bounded 15->30 minutes; synthetic Key 20->40 minutes')
    assert not mismatches, mismatches
    assert not completion['blocked_capacity']
    assert all((done.parent/(c['name']+'.exit.txt')).read_text().strip()==str(c['exit_code'])
               for c in checks if c['name']!='orchestration')
    rows.append({'candidate_sha256':digest, 'phase':phase,
                 'commands':[{k:c[k] for k in ('name','exit_code')} for c in checks],
                 'all_commands_passed':completion['all_commands_passed'],
                 'runtime_source_matches_current':True,
                 'manual_fixture_adjustments':fixture_adjustments})
frontend = sorted((OUT/'local').glob('*-frontend-results.json'))[-1]
local = json.loads(frontend.read_text())
assert len(local)==3
for check in local:
    assert check['exit_code']==0, check['name']
    assert all((ROOT/n).is_file() and sha(ROOT/n)==h for n,h in check['inputs'].items())
for tool in (ROOT/'docs/integration/knowhub-mcp-portal/tools').glob('batch7c_*.py'):
    ast.parse(tool.read_text(encoding='utf8'))
report_root=ROOT/'docs/integration/knowhub-mcp-portal'
for report in report_root.glob('*.md'):
    if not report.name[:2].isdigit() or int(report.name[:2])<46: continue
    for target in re.findall(r'\]\(([^)]+)\)',report.read_text(encoding='utf8')):
        if '://' not in target and not target.startswith('#'):
            assert (report.parent/target.split('#')[0]).exists(), (report.name,target)
diff=subprocess.run(['git','diff','--check'],cwd=ROOT,capture_output=True,text=True)
assert diff.returncode==0, diff.stdout+diff.stderr
artifacts={str(p.relative_to(report_root)).replace('\\','/'):sha(p)
           for p in OUT.rglob('*') if p.is_file() and p.name!='final-validation.json'}
result={'checked_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'baseline':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip(),
        'linux_runs':rows,'frontend_results':str(frontend.relative_to(OUT)),
        'evidence_artifacts':artifacts,'consistency_exit_code':0,
        'meaning':'Evidence integrity only; any FAIL/HOLD remains FAIL/HOLD.'}
(OUT/'final-validation.json').write_text(json.dumps(result,indent=2),encoding='utf8')
print(json.dumps({'consistency_exit_code':0,'linux_runs':len(rows),'artifacts':len(artifacts)}))
