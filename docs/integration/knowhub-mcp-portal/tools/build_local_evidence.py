"""Collect source metadata only. Does not import the MCP application."""
import ast
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[4]
OUT = ROOT / 'docs/integration/knowhub-mcp-portal/evidence'
OUT.mkdir(exist_ok=True)


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


source = ROOT / 'mcp-server/weknora_mcp_server.py'
tree = ast.parse(source.read_text(encoding='utf-8'))
client = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == 'WeKnoraClient')
methods = {n.name: n for n in client.body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}
tools = [n for n in tree.body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and any(
    isinstance(d, ast.Call) and isinstance(d.func, ast.Attribute) and d.func.attr == 'tool' for d in n.decorator_list)]
mapping = []
for tool in tools:
    method = methods.get(tool.name)
    endpoints = []
    if method:
        for node in ast.walk(method):
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr == '_request' and len(node.args) >= 2:
                endpoints.append({'method': ast.literal_eval(node.args[0]), 'path_expression': ast.unparse(node.args[1]), 'line': node.lineno})
        if tool.name == 'create_knowledge_from_file':
            endpoints.append({'method': 'POST', 'path_expression': '/knowledge-bases/{kb_id}/knowledge/file', 'line': method.lineno})
        if tool.name in ('chat', 'agent_chat'):
            endpoints.append({'method': 'POST', 'path_expression': '/' + ('knowledge-chat' if tool.name == 'chat' else 'agent-chat') + '/{session_id}', 'line': method.lineno})
    mapping.append({'tool': tool.name, 'tool_line': tool.lineno, 'routes': endpoints})
(OUT / 'legacy-tool-routes.json').write_text(json.dumps(mapping, indent=2) + '\n', encoding='utf-8')
hashes = {}
for label, rev in [('audit', 'HEAD'), ('production_app', 'e62b17e2d305358467821aeff1cce63ce1d61254'), ('production_mcp', '854f3b956ae3600e12a3ffd2fc40d4fe624ced74'), ('upstream_head', 'e0c28bef3562de7d2d78d6c1fa480a045468a40f'), ('upstream_stable', '3e22822c1d2397be730c458c7539d0eb8c113a24')]:
    data = git('show', rev + ':mcp-server/weknora_mcp_server.py')
    hashes[label] = {'revision': git('rev-parse', rev).decode().strip(), 'python_module_sha256': hashlib.sha256(data).hexdigest()}
(OUT / 'source-baselines.json').write_text(json.dumps(hashes, indent=2) + '\n', encoding='utf-8')
paths = [
    'internal/application/repository/portal.go', 'internal/application/service/portal.go',
    'internal/types/portal.go', 'internal/types/tenant_api_key.go',
    'internal/application/service/tenant_api_key.go', 'internal/middleware/auth.go',
    'internal/middleware/mcp_endpoint_auth.go', 'internal/router/routes_mcp_endpoint.go',
    'internal/router/routes_knowledge.go', 'internal/router/routes_auth_tenant.go',
    'internal/router/routes_chat.go', 'internal/router/routes_infra.go',
    'internal/router/routes_agent.go', 'internal/router/files.go',
    'internal/application/service/knowledge.go', 'internal/application/service/chunk.go',
    'internal/application/service/wiki_page.go',
    'internal/application/service/datasource_service.go',
    'internal/application/service/feishu_personal.go',
    'internal/agent/tools/search_knowledge.go', 'internal/mcpserver/server.go',
    'internal/mcpserver/scope.go', 'internal/mcpserver/tools_retrieve.go',
    'internal/mcpserver/tools_wiki.go', 'internal/types/mcp_endpoint_tools.go',
]
index = []
for path in paths:
    data = (ROOT / path).read_text(encoding='utf-8')
    declarations = [{'line': i, 'declaration': line.strip()} for i, line in enumerate(data.splitlines(), 1)
                    if re.match(r'^func ', line)]
    revisions = {}
    for label, rev in [('audit', 'HEAD'), ('production_app', 'e62b17e2d305358467821aeff1cce63ce1d61254'), ('upstream_head', 'e0c28bef3562de7d2d78d6c1fa480a045468a40f')]:
        p = subprocess.run(['git', 'show', rev + ':' + path], cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        revisions[label] = hashlib.sha256(p.stdout).hexdigest() if p.returncode == 0 else None
    index.append({'path': path, 'functions': declarations, 'sha256_by_baseline': revisions})
(OUT / 'source-index.json').write_text(json.dumps(index, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'tool_count': len(mapping), 'mapped_tools': sum(bool(x['routes']) for x in mapping), 'baseline_count': len(hashes)}))
