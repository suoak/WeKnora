"""Refresh only this run's test APP after a new verified binary is built."""
import json
from pathlib import Path
import subprocess
import time
import urllib.request

B = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
P = B.name
assert json.loads((B / 'scope.json').read_text())['owner'] == P
app = P + '-app'
assert subprocess.check_output(['docker', 'inspect', '--format', '{{index .Config.Labels "knowhub.scope"}}', app]).decode().strip() == P
subprocess.run(['docker', 'rm', '-f', app], check=True, stdout=subprocess.DEVNULL)
args = ['docker', 'run', '-d', '--network', 'host', '--name', app, '--label', 'knowhub.scope=' + P,
        '-v', str(B / 'WeKnora') + ':/p1/WeKnora:ro', '-v', str(B / 'fullapp/config') + ':/app/config:ro',
        '-v', str(B / 'source/migrations') + ':/app/migrations:ro',
        '-v', '/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/p1/jieba:ro',
        '-v', str(B / 'files') + ':/p1/files', '-v', str(B / 'logs/fullapp') + ':/p1/logs',
        '--env-file', str(B / 'private/fullapp.env'), '-w', '/app']
env = {'DB_DRIVER': 'postgres', 'DB_HOST': '127.0.0.1', 'DB_PORT': '18643', 'DB_USER': 'postgres',
       'DB_PASSWORD': 'synthetic-p1-test-only', 'DB_NAME': 'knowhub_isolated_p1_20261111',
       'RETRIEVE_DRIVER': 'postgres', 'STORAGE_TYPE': 'local', 'LOCAL_STORAGE_BASE_DIR': '/p1/files',
       'REDIS_ADDR': '127.0.0.1:18647', 'REDIS_PASSWORD': '', 'JIEBA_DICT_DIR': '/p1/jieba',
       'GIN_MODE': 'release', 'LOG_PATH': '/p1/logs/app.log', 'LOG_LEVEL': 'debug',
       'LLM_DEBUG_LOG': '/p1/logs/llm-debug', 'SSRF_WHITELIST': '127.0.0.1',
       'WEKNORA_TENANT_ENABLE_RBAC': 'true', 'GOMAXPROCS': '2', 'LANGFUSE_ENABLED': 'true',
       'LANGFUSE_HOST': 'http://127.0.0.1:18648', 'LANGFUSE_PUBLIC_KEY': 'synthetic-p1',
       'LANGFUSE_SECRET_KEY': 'synthetic-p1', 'LANGFUSE_FLUSH_INTERVAL': '100ms'}
for k, v in env.items():
    args += ['-e', k + '=' + v]
args += ['knowhub-build/golang:1.26-bookworm-ustc', '/p1/WeKnora']
subprocess.run(args, check=True, stdout=subprocess.DEVNULL)
for _ in range(90):
    try:
        with urllib.request.urlopen('http://127.0.0.1:18644/health', timeout=2) as r:
            if r.status == 200:
                break
    except Exception:
        pass
    time.sleep(1)
else:
    raise RuntimeError('test_app_not_ready')
