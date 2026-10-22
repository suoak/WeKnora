"""Check the exact business candidate and its real HTTP/browser evidence."""
from pathlib import Path
import datetime, hashlib, json, subprocess

ROOT = Path(__file__).resolve().parents[4]
OUT = ROOT / 'docs/integration/knowhub-mcp-portal/evidence/batch7/business'
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
manifests = [json.loads(p.read_text()) for p in (OUT/'candidates').glob('*.json')]
business = next(m for m in manifests if m['candidate_sha256'].startswith('31514a0c30a37f63'))
clients = next(m for m in manifests if m['candidate_sha256'].startswith('b979d1ab41fca8b8'))
checks = []
for manifest in (business, clients):
    # No changed Go input may inherit an old PASS. Frontend has its own exact
    # input map below because its final template was added after the Go run.
    for n,h in manifest['files'].items():
        if n.startswith(('internal/', 'cmd/', 'migrations/')) or n in ('go.mod','go.sum'):
            assert (ROOT/n).is_file() and sha(ROOT/n)==h, n
    folder=OUT/'linux'/('results-'+manifest['candidate_sha256'][:16]+'-'+manifest['phase'])
    done=json.loads((folder/'done.json').read_text())
    assert done['all_commands_passed'] and not done['blocked_capacity']
    results=json.loads((folder/'results.json').read_text())
    assert all(r['exit_code']==0 for r in results)
    checks.append({'candidate':manifest['candidate_sha256'],'phase':manifest['phase'],
                   'commands':[{k:r[k] for k in ('name','exit_code')} for r in results]})
local=max((OUT/'local').glob('*-frontend-results.json'))
frontend=json.loads(local.read_text())
assert len(frontend)==3 and all(r['exit_code']==0 for r in frontend)
for r in frontend:
    assert all((ROOT/n).is_file() and sha(ROOT/n)==h for n,h in r['inputs'].items())
browser=json.loads((OUT/'browser/result-attempt2.json').read_text(encoding='utf8'))
assert browser['exit_code']==0 and browser['candidate']==clients['candidate_sha256']
assert any(a['action']=='one_dialog_personal_create_all_private_scopes_and_public_union'
           and a['private_tenants']==[1,7] for a in browser['actions'])
assert any(a.get('http_status')==401 for a in browser['actions'])
successful={a['tool'] for a in browser['actions'] if a['action']=='real_MCP_tool'
            and a['http_status']==200 and not a['is_error']}
assert {'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'}<=successful
assert any(a.get('tool')=='create_knowledge' and a['is_error'] for a in browser['actions'])
protocol=OUT/('client-protocol-'+clients['candidate_sha256'][:16]+'.jsonl')
wire=[json.loads(line) for line in protocol.read_text().splitlines()]
assert {x['tool']for x in wire if x['rpc_method']=='tools/call'}>=successful
assert any(x['status']==401 and x['rpc_method']=='tools/call'for x in wire)
assert subprocess.run(['git','diff','--check'],cwd=ROOT,capture_output=True).returncode==0
names=set(subprocess.check_output(['git','ls-files'],cwd=ROOT).decode().splitlines())
names.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=ROOT).decode().splitlines())
files={n:sha(ROOT/n)for n in sorted(names)if (ROOT/n).is_file()
       and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
fingerprint=hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()
result={'checked_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'baseline':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip(),
        'source_candidate_sha256':fingerprint,'files':files,'validated_go_candidates':checks,
        'frontend_result':str(local.relative_to(OUT)),
        'browser_result':'browser/result-attempt2.json','protocol_sha256':sha(protocol),
        'browser_harness_sha256':sha(ROOT/'docs/integration/knowhub-mcp-portal/tools/batch7d_browser_flow.cjs'),
        'workmate_invoked':False,'production_enabled':False,'commit_push_deploy':False,
        'exit_code':0}
(OUT/'final-validation.json').write_text(json.dumps(result,indent=2),encoding='utf8')
print(json.dumps({'exit_code':0,'source_candidate_sha256':fingerprint,'files':len(files)}))
