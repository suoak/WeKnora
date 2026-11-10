"""Identity revocation, fault and rollback tests: this run's test resources only."""
import http.client
import json
from pathlib import Path
import socket
import ssl
import subprocess
import time

B = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
P = B.name
assert P == 'knowhub-isolated-p1-20261111' and json.loads((B / 'scope.json').read_text())['owner'] == P
keys = json.loads((B / 'private/clients.json').read_text())
ctx = ssl.create_default_context(cafile=str(B / 'private/test-ca.pem'))
results = []


def record(name, passed, **meta):
    results.append(dict(name=name, status='PASS' if passed else 'FAIL', **meta))
    (B / 'results/faults.json').write_text(json.dumps(results, indent=2))


def sql(q):
    return subprocess.check_output(['docker', 'exec', P + '-pg', 'psql', '-X', '-U', 'postgres', '-d', 'knowhub_isolated_p1_20261111', '-At', '-c', q], stderr=subprocess.DEVNULL, timeout=15).decode().strip()


def call(method='tools/list', params=None, key=None, tls=True):
    c = http.client.HTTPSConnection('localhost', 8443, context=ctx, timeout=8) if tls else http.client.HTTPConnection('127.0.0.1', 18644, timeout=8)
    try:
        body = {'jsonrpc': '2.0', 'id': 1, 'method': method}
        if params is not None:
            body['params'] = params
        c.request('POST', '/api/v1/mcp/personal', json.dumps(body), {'Content-Type': 'application/json', 'Accept': 'application/json,text/event-stream', 'Authorization': 'Bearer ' + (key or keys['key_a'])})
        r = c.getresponse()
        raw = r.read(4 << 20)
        try:
            reply = json.loads(raw)
        except ValueError:
            reply = None
        return r.status, reply
    except (OSError, http.client.HTTPException):
        return 0, None
    finally:
        c.close()


def owned(name):
    assert name in [P + '-pg', P + '-app', P + '-nginx']
    assert subprocess.check_output(['docker', 'inspect', '--format', '{{index .Config.Labels "knowhub.scope"}}', name]).decode().strip() == P


def docker(action, name):
    owned(name)
    subprocess.run(['docker', action, name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=40)


def wait_key():
    for _ in range(60):
        status, reply = call()
        if status == 200 and reply and 'result' in reply:
            return True
        time.sleep(1)
    return False


with socket.create_connection(('127.0.0.1', 8443), timeout=5) as raw:
    with ctx.wrap_socket(raw, server_hostname='knowhub.ruijie.com.cn') as secure:
        record('planned_domain_sni_and_test_chain', secure.version() in ['TLSv1.2', 'TLSv1.3'])

for column, bad, restore, label in [
    ('revoked_at', 'now()', 'NULL', 'key_revocation'),
    ('expires_at', "now()-interval '1 minute'", "now()+interval '2 hours'", 'key_expiry'),
]:
    sql('UPDATE tenant_api_keys SET ' + column + '=' + bad + " WHERE name='isolated-p1-synthetic' AND owner_user_id='C'")
    status, _ = call(key=keys['key_c'])
    record(label + '_denied', status in [401, 403], http=status)
    sql('UPDATE tenant_api_keys SET ' + column + '=' + restore + " WHERE name='isolated-p1-synthetic' AND owner_user_id='C'")
for table, predicate, column, bad, restore, label in [
    ('users', "id='C'", 'is_active', 'false', 'true', 'disabled_user'),
    ('tenant_members', "user_id='C' AND tenant_id=99", 'status', "'inactive'", "'active'", 'revoked_member'),
    ('tenants', 'id=99', 'status', "'inactive'", "'active'", 'disabled_workspace'),
]:
    sql('UPDATE ' + table + ' SET ' + column + '=' + bad + ' WHERE ' + predicate)
    status, _ = call(key=keys['key_c'])
    record(label + '_denied', status in ([400, 401, 403] if label == 'disabled_workspace' else [401, 403]), http=status)
    sql('UPDATE ' + table + ' SET ' + column + '=' + restore + ' WHERE ' + predicate)
sql("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99")
status, reply = call('tools/call', {'name': 'list_knowledge_bases', 'arguments': {}})
kb_ids = []
if status == 200 and reply and not reply.get('result', {}).get('isError'):
    kb_ids = [x['id'] for x in json.loads(reply['result']['content'][0]['text'])]
record('public_withdrawal_preserves_independent_private_scope', 'public-kb' not in kb_ids and 'private-home' in kb_ids)
sql("UPDATE tenant_portal_configs SET status='published' WHERE tenant_id=99")

sql("UPDATE tenant_api_keys SET capabilities='[\"retrieve\",\"enterprise_retrieve\",\"ingest\",\"chat\"]' WHERE name='isolated-p1-synthetic' AND owner_user_id='A'")
for name in ['ask', 'add_document', 'update_document', 'delete_document', 'future_internal_upload']:
    status, _ = call('tools/call', {'name': name, 'arguments': {'content': 'SYNTHETIC_P1_QUERY_CODE_FILE_CANARY'}})
    record('ingest_capability_cannot_enable_' + name, status == 400, http=status)
sql("UPDATE tenant_api_keys SET capabilities='[\"retrieve\",\"enterprise_retrieve\"]' WHERE name='isolated-p1-synthetic' AND owner_user_id='A'")
for i, path in enumerate(['file:///etc/passwd', '../../etc/passwd', '\\\\server\\file']):
    status, reply = call('tools/call', {'name': 'read_document', 'arguments': {'knowledge_id': path}})
    record('document_id_is_not_a_local_path_' + str(i), status == 200 and bool(reply.get('result', {}).get('isError')))

for name, framing in [
    ('conflicting_length_and_chunked', b'Content-Length: 5\r\nTransfer-Encoding: chunked\r\n'),
    ('duplicate_conflicting_lengths', b'Content-Length: 5\r\nContent-Length: 8\r\n'),
]:
    with socket.create_connection(('127.0.0.1', 8443), timeout=5) as raw:
        with ctx.wrap_socket(raw, server_hostname='localhost') as secure:
            secure.sendall(b'POST /api/v1/mcp/personal HTTP/1.1\r\nHost: localhost:8443\r\n' + framing + b'Connection: close\r\n\r\n0\r\n\r\n')
            line = secure.recv(1024).split(b'\r\n')[0]
            record(name + '_rejected', b' 400 ' in line)
with socket.create_connection(('127.0.0.1', 8443), timeout=20) as raw:
    with ctx.wrap_socket(raw, server_hostname='localhost') as secure:
        secure.settimeout(20)
        secure.sendall(b'POST /api/v1/mcp/personal HTTP/1.1\r\nHost: localhost:8443\r\nContent-Type: application/json\r\nAuthorization: Bearer ' + keys['key_a'].encode() + b'\r\nContent-Length: 1000\r\n\r\n{')
        started = time.monotonic()
        try:
            line = secure.recv(1024).split(b'\r\n')[0]
        except socket.timeout:
            line = b'timeout'
        elapsed = time.monotonic() - started
        # Nginx may close an incomplete client stream without writing a status
        # line. Both timeout forms must be bounded; auth failures do not count.
        record('slow_body_deadline', (b' 408 ' in line or not line) and 10 <= elapsed < 20, elapsed_seconds=round(elapsed, 2), response='408' if b' 408 ' in line else ('closed' if not line else 'other'))

# An authorization DB failure must never fall back to an unrestricted catalog.
docker('stop', P + '-pg')
status, _ = call()
record('authorization_store_failure_closed', status != 200, http=status)
docker('start', P + '-pg')
record('authorization_store_recovery', wait_key())
docker('stop', P + '-app')
status, _ = call()
record('upstream_failure_has_no_fallback', status == 502, http=status)
docker('start', P + '-app')
record('app_recovery_same_key', wait_key())

# Roll back only the new isolated proxy. The existing internal handler survives.
docker('stop', P + '-nginx')
closed = False
try:
    with socket.create_connection(('127.0.0.1', 8443), timeout=2):
        pass
except OSError:
    closed = True
record('rollback_closes_only_isolated_tls', closed)
status, reply = call(tls=False)
record('rollback_keeps_internal_mcp', status == 200 and reply is not None)
docker('start', P + '-nginx')
record('isolated_proxy_restored', wait_key())
(B / 'results/faults-done.json').write_text(json.dumps({'all_pass': all(x['status'] == 'PASS' for x in results), 'checks': len(results)}))
