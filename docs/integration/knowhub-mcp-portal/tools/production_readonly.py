"""Run on the approved host through SSH stdin. No remote files or data writes.

Output is an allowlist projection; never emit inspect/env, SQL errors, source,
credentials, user identifiers, titles, paths, metadata or document content.
"""
import ast
import datetime
import hashlib
import json
import subprocess


def run(args, data=None):
    p = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                       stderr=subprocess.DEVNULL, timeout=25)
    if p.returncode:
        raise RuntimeError("read_unavailable")
    return p.stdout


def sql(query):
    statement = "BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; " + query + "; ROLLBACK;"
    raw = run(['docker', 'exec', '-i', '-u', 'postgres', 'WeKnora-postgres',
               'psql', '-X', '-w', '-U', 'postgres', '-d', 'weknora',
               '-qAt', '-v', 'ON_ERROR_STOP=1'], statement.encode())
    return json.loads(raw.decode())


def rows(query):
    return sql("SELECT COALESCE(json_agg(x), '[]'::json) FROM (" + query + ") x")


def columns(table):
    return {x['column_name'] for x in rows(
        "SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='" + table + "'")}


def collect():
    result = {'observed_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'read_only': True, 'production_changes': False, 'containers': []}
    envs = {}
    candidates = [json.loads(line) for line in run(['docker', 'ps', '-a', '--format', '{{json .}}']).decode().splitlines()]
    mcp_names = [x['Names'] for x in candidates if 'mcp' in (x['Names'] + ' ' + x['Image']).lower()]
    result['mcp_container_candidates'] = mcp_names
    for name in ['WeKnora-app', 'WeKnora-frontend', 'WeKnora-postgres'] + mcp_names:
        try:
            info = json.loads(run(['docker', 'inspect', name]))[0]
            envs[name] = dict(x.split('=', 1) for x in info['Config'].get('Env', []) if '=' in x)
            image = json.loads(run(['docker', 'image', 'inspect', info['Image']]))[0]
            labels = image['Config'].get('Labels') or {}
            result['containers'].append({'name': name, 'image_id': info['Image'],
                'image_ref': info['Config']['Image'], 'running': info['State']['Running'],
                'revision': labels.get('org.opencontainers.image.revision'),
                'version': labels.get('org.opencontainers.image.version')})
        except Exception as exc:
            result['containers'].append({'name': name, 'status': 'BLOCKED', 'error_class': type(exc).__name__})
    mcp_name = mcp_names[0] if len(mcp_names) == 1 else None
    mcp_env = envs.get(mcp_name, {})
    result['legacy_auth'] = {'configured_mode': mcp_env.get('MCP_AUTH_MODE', 'UNSET'),
                            'process_api_key_present': bool(mcp_env.get('WEKNORA_API_KEY')),
                            'gateway_secret_present': bool(mcp_env.get('MCP_SERVER_AUTH_TOKEN'))}
    # Parse only the deployed MCP module to identify tool names and auth default.
    code = """import ast,hashlib,json,pathlib
for p in ['/app/weknora_mcp_server.py','/app/mcp-server/weknora_mcp_server.py']:
 f=pathlib.Path(p)
 if f.is_file():
  b=f.read_bytes(); t=b.decode(); tree=ast.parse(t)
  names=[n.name for n in tree.body if isinstance(n,(ast.FunctionDef,ast.AsyncFunctionDef)) and any(isinstance(d,ast.Call) and isinstance(d.func,ast.Attribute) and d.func.attr=='tool' for d in n.decorator_list)]
  defaults=[n.args[1].value for n in ast.walk(tree) if isinstance(n,ast.Call) and isinstance(n.func,ast.Attribute) and n.func.attr=='getenv' and len(n.args)>1 and isinstance(n.args[0],ast.Constant) and n.args[0].value=='MCP_AUTH_MODE' and isinstance(n.args[1],ast.Constant)]
  print(json.dumps({'sha256':hashlib.sha256(b).hexdigest(),'tools':names,'auth_defaults':defaults}));break
else: print(json.dumps({'status':'NOT_TESTED','reason':'module_path_not_found'}))
"""
    try:
        if not mcp_name:
            raise RuntimeError('ambiguous_mcp_target')
        result['deployed_python'] = json.loads(run(['docker', 'exec', mcp_name, 'python', '-c', code]))
    except Exception as exc:
        result['deployed_python'] = {'status': 'BLOCKED', 'error_class': type(exc).__name__}
    queries = {
        'migration': 'SELECT version, dirty FROM schema_migrations',
        'portal_statuses': "SELECT pc.status, t.status AS tenant_status, count(*) FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id WHERE t.deleted_at IS NULL GROUP BY pc.status,t.status",
        'published_kbs': "SELECT pc.tenant_id,kb.type,kb.is_temporary,count(*) FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id AND t.deleted_at IS NULL AND t.status='active' JOIN knowledge_bases kb ON kb.tenant_id=t.id AND kb.deleted_at IS NULL WHERE pc.status='published' GROUP BY pc.tenant_id,kb.type,kb.is_temporary",
        'published_documents': "SELECT k.tenant_id,k.type,k.parse_status,k.enable_status,k.channel,count(*) FROM knowledges k JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id AND kb.tenant_id=k.tenant_id AND kb.deleted_at IS NULL AND NOT kb.is_temporary JOIN tenants t ON t.id=k.tenant_id AND t.deleted_at IS NULL AND t.status='active' JOIN tenant_portal_configs pc ON pc.tenant_id=t.id AND pc.status='published' WHERE k.deleted_at IS NULL GROUP BY k.tenant_id,k.type,k.parse_status,k.enable_status,k.channel",
        'derivative_counts': 'SELECT (SELECT count(*) FROM wiki_pages) AS wiki_pages,(SELECT count(*) FROM chunks WHERE deleted_at IS NULL) AS chunks',
        'all_portal_statuses': 'SELECT pc.status,t.status AS tenant_status,(t.deleted_at IS NOT NULL) AS tenant_deleted,count(*) FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id GROUP BY pc.status,t.status,(t.deleted_at IS NOT NULL)',
        'published_index_coverage': "SELECT k.tenant_id,count(*) AS documents,count(*) FILTER (WHERE EXISTS (SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id)) AS documents_with_embeddings FROM knowledges k JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id AND kb.tenant_id=k.tenant_id AND kb.deleted_at IS NULL AND NOT kb.is_temporary JOIN tenants t ON t.id=k.tenant_id AND t.deleted_at IS NULL AND t.status='active' JOIN tenant_portal_configs pc ON pc.tenant_id=t.id AND pc.status='published' WHERE k.deleted_at IS NULL GROUP BY k.tenant_id",
    }
    for key, query in queries.items():
        try:
            result[key] = rows(query)
        except Exception as exc:
            result[key] = {'status': 'BLOCKED', 'error_class': type(exc).__name__}
    try:
        known = columns('tenant_api_keys')
        result['credential_schema_fields'] = sorted(known & {'scope_type', 'owner_user_id', 'capabilities', 'full_access', 'knowledge_base_ids', 'expires_at', 'revoked_at', 'key_hash'})
        if {'key_hash', 'scope_type', 'capabilities', 'full_access', 'knowledge_base_ids'} <= known:
            # Key hashes never leave this process. Compare with the process key in memory.
            keys = rows('SELECT key_hash,scope_type,full_access,capabilities,knowledge_base_ids' +
                        (',revoked_at' if 'revoked_at' in known else '') +
                        (',expires_at' if 'expires_at' in known else '') + ' FROM tenant_api_keys')
            secret = mcp_env.get('WEKNORA_API_KEY', '')
            matched = [k for k in keys if secret and k['key_hash'] == hashlib.sha256(secret.encode()).hexdigest()]
            result['legacy_process_key_scope'] = [{k: v for k, v in row.items() if k != 'key_hash'} for row in matched]
            result['legacy_process_key_match_count'] = len(matched)
            result['credential_scope_counts'] = rows('SELECT scope_type,full_access,count(*) FROM tenant_api_keys GROUP BY scope_type,full_access')
            if {'revoked_at', 'expires_at'} <= known:
                result['active_credential_scope_counts'] = rows('SELECT scope_type,full_access,count(*) FROM tenant_api_keys WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()) GROUP BY scope_type,full_access')
        result['acl_schema'] = {t: sorted(columns(t) & {'access_policy', 'mcp_policy', 'enterprise_policy', 'publication_policy', 'acl_version'}) for t in ('knowledges', 'knowledge_bases', 'tenant_portal_configs')}
        result['acl_candidate_tables'] = rows("SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND (table_name LIKE '%acl%' OR table_name LIKE '%enterprise%policy%' OR table_name LIKE '%document%grant%')")
    except Exception as exc:
        result['credential_audit'] = {'status': 'BLOCKED', 'error_class': type(exc).__name__}
    # Synchronization provenance is counted by channel above; source IDs/URLs are not exported.
    return result


if __name__ == '__main__':
    try:
        print(json.dumps(collect(), ensure_ascii=False, indent=2))
    except Exception as exc:
        print(json.dumps({'status': 'BLOCKED', 'error_class': type(exc).__name__, 'production_changes': False}))
