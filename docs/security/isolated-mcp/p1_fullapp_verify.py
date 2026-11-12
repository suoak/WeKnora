"""Full APP acceptance and side-effect checks, restricted to this disposable P1."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

BASE = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
PREFIX = 'knowhub-isolated-p1-20261111'
assert json.loads((BASE / 'scope.json').read_text())['owner'] == PREFIX
for _ in range(90):
    try:
        with urllib.request.urlopen('http://127.0.0.1:18644/health', timeout=2) as r:
            if r.status == 200:
                break
    except Exception:
        pass
    time.sleep(1)
else:
    (BASE / 'results/fullapp-start.json').write_text(json.dumps({'healthy': False}))
    raise SystemExit(1)
(BASE / 'results/fullapp-start.json').write_text(json.dumps({'healthy': True, 'binary_sha256': hashlib.sha256((BASE / 'WeKnora').read_bytes()).hexdigest()}))


def run(args):
    return subprocess.check_output(args, stderr=subprocess.DEVNULL, timeout=20)


def sql(statement):
    return run(['docker', 'exec', PREFIX + '-pg', 'psql', '-X', '-U', 'postgres', '-d',
                'knowhub_isolated_p1_20261111', '-At', '-c', statement]).decode().strip()


def snapshot():
    result = {'business_tables': {}, 'object_store_files': [], 'business_tasks': {}}
    for name in ['knowledges', 'chunks', 'wiki_pages', 'sessions', 'messages', 'resources',
                 'resource_bindings', 'task_pending_ops', 'task_dead_letters']:
        result['business_tables'][name] = sql("SELECT count(*)::text || ':' || COALESCE(md5(string_agg(md5(row_to_json(t)::text), '' ORDER BY id)), '') FROM " + name + ' t')
    for p in (BASE / 'files').rglob('*'):
        if p.is_file():
            result['object_store_files'].append(hashlib.sha256(p.read_bytes()).hexdigest())
    for queue in ['default', 'chat_attachment', 'postprocess', 'summary', 'multimodal', 'graph', 'question', 'sync', 'approved_sync', 'low', 'wiki', 'memory']:
        keys = run(['docker', 'exec', PREFIX + '-redis', 'redis-cli', '--raw', 'KEYS', 'asynq:{' + queue + '}:t:*']).decode().strip()
        result['business_tasks'][queue] = len(keys.splitlines()) if keys else 0
    return result


# Expiry applies only to the two newly created synthetic test credentials.
sql("UPDATE tenant_api_keys SET expires_at=now()+interval '2 hours' WHERE name='isolated-p1-synthetic'")
before = snapshot()
subprocess.run(['python3', str(BASE / 'p1_accept.py')], env=dict(os.environ, P1_PHASE='fullapp'),
               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=600, check=True)
after = snapshot()
clients = json.loads((BASE / 'private/clients.json').read_text())
corpus = b''
for name in [PREFIX + '-app', PREFIX + '-pg', PREFIX + '-nginx']:
    corpus += subprocess.check_output(['docker', 'logs', name], stderr=subprocess.STDOUT, timeout=20)
marker = b'SYNTHETIC_P1_QUERY_CODE_FILE_CANARY'
usage_canaries = int(sql("SELECT count(*) FROM mcp_usage_events WHERE client_name LIKE '%SYNTHETIC_P1_QUERY_CODE_FILE_CANARY%' OR client_version LIKE '%SYNTHETIC_P1_QUERY_CODE_FILE_CANARY%'"))
result = {'before': before, 'after': after, 'business_unchanged': before == after,
          'container_logs_marker_absent': marker not in corpus,
          'container_logs_test_credentials_absent': all(clients[k].encode() not in corpus for k in ['key_a', 'key_c']),
          'usage_database_canary_count': usage_canaries}
(BASE / 'results/fullapp-side-effects.json').write_text(json.dumps(result, indent=2))
assert result['business_unchanged'] and result['container_logs_marker_absent'] and result['container_logs_test_credentials_absent'] and usage_canaries == 0
