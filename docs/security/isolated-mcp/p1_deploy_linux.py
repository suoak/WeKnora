"""Create only the approved, uniquely named loopback P1 test resources."""
import json
import os
from pathlib import Path
import socket
import subprocess
import time

BASE = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
PREFIX = 'knowhub-isolated-p1-20261111'
assert json.loads((BASE / 'scope.json').read_text())['owner'] == PREFIX
os.umask(0o077)


def run(args, timeout=60):
    return subprocess.check_output(args, stderr=subprocess.STDOUT, timeout=timeout).decode()


def free(port):
    with socket.socket() as s:
        s.bind(('127.0.0.1', port))


pg = PREFIX + '-pg'
existing_pg = run(['docker', 'ps', '-a', '--filter', 'name=^/' + pg + '$', '--format', '{{.Names}}']).strip()
if existing_pg:
    assert existing_pg == pg
    assert run(['docker', 'inspect', '--format', '{{index .Config.Labels "knowhub.scope"}}', pg]).strip() == PREFIX
else:
    free(18643)
for port in [18644, 18645, 8443, 18443]:
    free(port)
assert (BASE / 'fixture.test').is_file()
(BASE / 'private').mkdir(mode=0o700, exist_ok=True)
(BASE / 'logs').mkdir(mode=0o700, exist_ok=True)
private = BASE / 'private'
run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
     '-subj', '/CN=KnowHub P1 TEST ONLY CA', '-keyout', str(private / 'test-ca-key.pem'),
     '-out', str(private / 'test-ca.pem')])
run(['openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=localhost',
     '-keyout', str(private / 'test-key.pem'), '-out', str(private / 'test.csr')])
(private / 'test-extensions').write_text('subjectAltName=DNS:localhost,DNS:knowhub.ruijie.com.cn,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n')
run(['openssl', 'x509', '-req', '-in', str(private / 'test.csr'), '-CA', str(private / 'test-ca.pem'),
     '-CAkey', str(private / 'test-ca-key.pem'), '-CAcreateserial', '-days', '2',
     '-extfile', str(private / 'test-extensions'), '-out', str(private / 'test-fullchain.pem')])
run(['openssl', 'x509', '-req', '-in', str(private / 'test.csr'), '-CA', str(private / 'test-ca.pem'),
     '-CAkey', str(private / 'test-ca-key.pem'), '-CAcreateserial', '-days', '0',
     '-extfile', str(private / 'test-extensions'), '-out', str(private / 'test-expired.pem')])
for p in private.glob('*'):
    p.chmod(0o600)
if not existing_pg:
    run(['docker', 'run', '-d', '--name', pg, '--label', 'knowhub.scope=' + PREFIX,
     '-p', '127.0.0.1:18643:5432', '-e', 'POSTGRES_DB=knowhub_isolated_p1_20261111',
     '-e', 'POSTGRES_HOST_AUTH_METHOD=trust',
     '-v', PREFIX + '-pgdata:/var/lib/postgresql/data', 'paradedb/paradedb:v0.22.2-pg17',
     '-c', 'log_statement=none', '-c', 'log_min_error_statement=panic', '-c', 'log_parameter_max_length_on_error=0'])
for _ in range(60):
    p = subprocess.run(['docker', 'exec', pg, 'pg_isready', '-h', '127.0.0.1', '-U', 'postgres', '-d', 'knowhub_isolated_p1_20261111'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if p.returncode == 0:
        break
    time.sleep(1)
else:
    raise RuntimeError('dedicated_postgres_not_ready')
env = dict(os.environ, KNOWHUB_ISOLATED_P1_DIR=str(BASE), GIN_MODE='release',
           LLM_DEBUG_LOG=str(private / 'llm-debug'))
log = (BASE / 'app.log').open('wb')
p = subprocess.Popen([str(BASE / 'fixture.test'), '-test.run=^TestIsolatedP1DeployedFixture$', '-test.v', '-test.timeout=35m'],
                     cwd=BASE / 'source/internal/router', env=env, stdout=log, stderr=log,
                     start_new_session=True, stdin=subprocess.DEVNULL)
(BASE / 'fixture-pid.json').write_text(json.dumps({'pid': p.pid, 'scope': PREFIX}))
for _ in range(180):
    if p.poll() is not None:
        raise RuntimeError('fixture_exited_during_startup')
    if (BASE / 'ready.json').exists():
        break
    time.sleep(1)
else:
    raise RuntimeError('fixture_not_ready')
template = (BASE / 'source/docs/security/isolated-mcp/p1_nginx.conf.template').read_text()
(BASE / 'nginx.conf').write_text(template.replace('__ROOT__', str(BASE)))
nginx = PREFIX + '-nginx'
mounts = ['-v', str(BASE / 'nginx.conf') + ':/etc/nginx/nginx.conf:ro',
          '-v', str(BASE / 'logs') + ':' + str(BASE / 'logs')]
for name in ['test-fullchain.pem', 'test-key.pem', 'test-expired.pem']:
    mounts += ['-v', str(private / name) + ':' + str(private / name) + ':ro']
run(['docker', 'run', '--rm', '--network', 'host'] + mounts + ['nginx:1.30.3-alpine', 'nginx', '-t'])
run(['docker', 'run', '-d', '--network', 'host', '--name', nginx, '--label', 'knowhub.scope=' + PREFIX]
    + mounts + ['nginx:1.30.3-alpine'])
(BASE / 'results/deployment.json').write_text(json.dumps({'scope': PREFIX, 'test_host': '172.30.232.42',
    'postgres_container': pg, 'nginx_container': nginx, 'app_loopback_port': 18644,
    'nginx_loopback_port': 8443, 'nginx_config_test': 'PASS', 'tls': 'synthetic two-day test CA/leaf',
    'production_certificate_used': False, 'existing_80_8080_changed': False}, indent=2))
print('P1_LOOPBACK_TLS_FIXTURE_DEPLOYED')
