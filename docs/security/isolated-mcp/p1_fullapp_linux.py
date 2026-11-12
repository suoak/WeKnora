"""Start the built, complete APP with the already seeded disposable P1 DB.

This replaces only this run's opt-in fixture, never any existing APP. The
fixture must have shut down; the loopback proxy and synthetic credentials stay.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import secrets
import socket
import subprocess
import threading
import time

BASE = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
PREFIX = 'knowhub-isolated-p1-20261111'
assert json.loads((BASE / 'scope.json').read_text())['owner'] == PREFIX
assert (BASE / 'side-effects.json').exists()
os.umask(0o077)
for port in [18644, 18645, 18647, 18648]:
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', port))
runtime = BASE / 'fullapp'
runtime.mkdir(mode=0o700, exist_ok=True)
shutil.copytree(BASE / 'source/config', runtime / 'config', dirs_exist_ok=True)
cfg = runtime / 'config/config.yaml'
text = cfg.read_text().replace('  enabled: false\n  policy_reference: ""', '  enabled: true\n  policy_reference: "ISOLATED_P1_SYNTHETIC_ONLY"', 1)
text = text.replace('  port: 8080', '  port: 18644', 1).replace('  host: "0.0.0.0"', '  host: "127.0.0.1"', 1)
cfg.write_text(text)
files = BASE / 'files'
files.mkdir(mode=0o700, exist_ok=True)
(BASE / 'logs/fullapp').mkdir(mode=0o700, exist_ok=True)
metrics = {'model_calls': 0, 'slow_active': 0, 'apm_exports': 0, 'apm_canary_exports': 0}
lock = threading.Lock()


class Model(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        with lock:
            self.wfile.write(json.dumps(metrics).encode())

    def do_POST(self):
        payload = self.rfile.read(min(int(self.headers.get('Content-Length', 0)), 65536))
        with lock:
            metrics['model_calls'] += 1
        slow = b'P1_SLOW_QUERY' in payload
        if slow:
            with lock:
                metrics['slow_active'] += 1
            time.sleep(31)
            with lock:
                metrics['slow_active'] -= 1
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        try:
            self.wfile.write(json.dumps({'data': [{'index': 0, 'embedding': [1, 0, 0]}], 'usage': {'prompt_tokens': 1, 'total_tokens': 1}}).encode())
        except (BrokenPipeError, ConnectionResetError):
            pass


class APM(Model):
    def do_POST(self):
        payload = self.rfile.read(min(int(self.headers.get('Content-Length', 0)), 1 << 20))
        with lock:
            metrics['apm_exports'] += 1
            metrics['apm_canary_exports'] += int(b'SYNTHETIC_P1_QUERY_CODE_FILE_CANARY' in payload)
        self.send_response(200)
        self.end_headers()


for port, handler in [(18645, Model), (18648, APM)]:
    server = ThreadingHTTPServer(('127.0.0.1', port), handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
subprocess.run(['docker', 'run', '-d', '--name', PREFIX + '-redis', '--label', 'knowhub.scope=' + PREFIX,
                '-p', '127.0.0.1:18647:6379', 'redis:7.0-alpine', 'redis-server', '--save', '', '--appendonly', 'no'], check=True, stdout=subprocess.DEVNULL)
args = ['docker', 'run', '-d', '--network', 'host', '--name', PREFIX + '-app', '--label', 'knowhub.scope=' + PREFIX,
        '-v', str(BASE / 'WeKnora') + ':/p1/WeKnora:ro', '-v', str(runtime / 'config') + ':/app/config:ro',
        '-v', str(BASE / 'source/migrations') + ':/app/migrations:ro',
        '-v', '/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/p1/jieba:ro',
        '-v', str(files) + ':/p1/files', '-v', str(BASE / 'logs/fullapp') + ':/p1/logs', '-w', '/app']
service_env = BASE / 'private/fullapp.env'
if not service_env.exists():
    service_env.write_text('JWT_SECRET=' + secrets.token_hex(32) + '\nSYSTEM_AES_KEY=' + secrets.token_hex(16) + '\nSYSTEM_SIGNING_KEY=' + secrets.token_hex(32) + '\n')
service_env.chmod(0o600)
args += ['--env-file', str(service_env)]
env = {'DB_DRIVER': 'postgres', 'DB_HOST': '127.0.0.1', 'DB_PORT': '18643', 'DB_USER': 'postgres', 'DB_PASSWORD': 'synthetic-p1-test-only',
       'DB_NAME': 'knowhub_isolated_p1_20261111', 'RETRIEVE_DRIVER': 'postgres', 'STORAGE_TYPE': 'local',
       'LOCAL_STORAGE_BASE_DIR': '/p1/files', 'REDIS_ADDR': '127.0.0.1:18647', 'REDIS_PASSWORD': '',
       'JIEBA_DICT_DIR': '/p1/jieba',
       'GIN_MODE': 'release', 'LOG_PATH': '/p1/logs/app.log', 'LOG_LEVEL': 'debug', 'LLM_DEBUG_LOG': '/p1/logs/llm-debug',
       'SSRF_WHITELIST': '127.0.0.1', 'WEKNORA_TENANT_ENABLE_RBAC': 'true', 'GOMAXPROCS': '2',
       'LANGFUSE_ENABLED': 'true', 'LANGFUSE_HOST': 'http://127.0.0.1:18648',
       'LANGFUSE_PUBLIC_KEY': 'synthetic-p1', 'LANGFUSE_SECRET_KEY': 'synthetic-p1', 'LANGFUSE_FLUSH_INTERVAL': '100ms'}
for key, value in env.items():
    args += ['-e', key + '=' + value]
args += ['knowhub-build/golang:1.26-bookworm-ustc', '/p1/WeKnora']
subprocess.run(args, check=True, stdout=subprocess.DEVNULL)
(BASE / 'fullapp-pid.json').write_text(json.dumps({'pid': os.getpid(), 'scope': PREFIX}))
for _ in range(3600):
    if (BASE / 'stop-fullapp-model').exists():
        break
    (BASE / 'results/fullapp-model-metrics.json').write_text(json.dumps(metrics))
    time.sleep(1)
