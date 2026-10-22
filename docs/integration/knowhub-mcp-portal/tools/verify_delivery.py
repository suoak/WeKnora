"""Check this audit delivery against captured evidence, without network access."""
import ast
import json
from pathlib import Path
import re

BASE = Path(__file__).resolve().parents[1]
expected = [
    '00_BASELINE_AUDIT.md', '01_PORTAL_RESOURCE_INVENTORY.md',
    '02_LEGACY_MCP_SECURITY_AUDIT.md', '03_UPSTREAM_FORK_GAP_MATRIX.md',
    '04_ENTERPRISE_ACCESS_POLICY.md', '05_SENSITIVE_DOCUMENT_ACL_DESIGN.md',
    '06_GO_MCP_USER_PRINCIPAL_DESIGN.md', '07_UNIFIED_SEARCH_DESIGN.md',
    '08_PORTAL_AND_CLIENT_UX.md', '09_TEST_MATRIX.md',
    '10_PYTHON_TO_GO_MIGRATION.md', '11_DEPLOYMENT_AND_ROLLBACK.md',
    '12_IMPLEMENTATION_REPORT.md',
]
for name in expected:
    text = (BASE / name).read_text(encoding='utf-8')
    assert text.strip(), name
    for ref in re.findall(r'\]\(([^)]+)\)', text):
        if '://' not in ref:
            assert (BASE / ref.split('#')[0]).exists(), (name, ref)
    assert all(line.rstrip() == line for line in text.splitlines()), name
for path in (BASE / 'tools').glob('*.py'):
    ast.parse(path.read_text(encoding='utf-8'), filename=str(path))
prod = json.loads((BASE / 'evidence/production-readonly-20261020.json').read_text(encoding='utf-8'))
assert prod['read_only'] and not prod['production_changes'] and prod['ssh_host_key_verified']
assert prod['legacy_auth']['configured_mode'] == 'weknora_api_key'
assert not prod['legacy_auth']['process_api_key_present']
assert sum(x['count'] for x in prod['published_kbs']) == 48
assert sum(x['count'] for x in prod['published_documents']) == 2718
assert sum(x['documents_with_embeddings'] for x in prod['published_index_coverage']) == 2716
baseline = json.loads((BASE / 'evidence/source-baselines.json').read_text(encoding='utf-8'))
assert prod['deployed_python']['sha256'] == baseline['audit']['python_module_sha256'] == baseline['production_mcp']['python_module_sha256']
routes = json.loads((BASE / 'evidence/legacy-tool-routes.json').read_text(encoding='utf-8'))
assert len(routes) == 31 and all(x['routes'] for x in routes)
assert set(prod['deployed_python']['tools']) == {x['tool'] for x in routes}
for path in (BASE / 'evidence').glob('*.json'):
    json.loads(path.read_text(encoding='utf-8'))
print('PASS: 13 reports, local links, Python syntax, production inventory, deployed source hash, 31 route mappings and JSON evidence')
