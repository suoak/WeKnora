"""Read-only/negative acceptance against the dedicated synthetic P1 fixture.

Credentials and protocol bodies remain in memory. The output is fixed test
metadata; it must never contain raw replies, queries or client credentials.
"""
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
from pathlib import Path
import ssl
import socket
import subprocess
import time

BASE = Path('/home/aqyfzx/ai/knowhub-isolated-p1-20261111')
runtime = json.loads((BASE / 'private/clients.json').read_text())
ca = str(BASE / 'private/test-ca.pem')
ctx = ssl.create_default_context(cafile=ca)
PATH = '/api/v1/mcp/personal'
MARK = 'SYNTHETIC_P1_QUERY_CODE_FILE_CANARY'
EXPECTED = {'list_knowledge_bases', 'search_knowledge', 'grep_chunks', 'list_documents',
            'read_document', 'wiki_search', 'wiki_read_page', 'wiki_index'}
rows = []
PHASE = os.environ.get('P1_PHASE', 'fixture')
assert PHASE in ['fixture', 'fullapp']
RESULT = 'acceptance' if PHASE == 'fixture' else 'fullapp-acceptance'


def record(name, passed, **meta):
    rows.append(dict(name=name, status='PASS' if passed else 'FAIL', **meta))
    (BASE / 'results' / (RESULT + '.json')).write_text(json.dumps(rows, indent=2))


def request(body=None, *, tls=True, path=PATH, method='POST', media='application/json', key=None, extra=None):
    conn = http.client.HTTPSConnection('localhost', 8443, context=ctx, timeout=40) if tls else http.client.HTTPConnection('127.0.0.1', 18644, timeout=40)
    data = json.dumps(body).encode() if isinstance(body, (dict, list)) else body
    headers = {'Accept': 'application/json, text/event-stream'}
    if media is not None:
        headers['Content-Type'] = media
    if key is not False:
        headers['Authorization'] = 'Bearer ' + (key or runtime['key_a'])
    headers.update(extra or {})
    conn.request(method, path, data, headers)
    response = conn.getresponse()
    raw = response.read((4 << 20) + 1)
    status = response.status
    conn.close()
    return status, raw


def rpc(method, params=None, *, tls=True, key=None):
    body = {'jsonrpc': '2.0', 'id': 1, 'method': method}
    if params is not None:
        body['params'] = params
    status, raw = request(body, tls=tls, key=key)
    try:
        reply = json.loads(raw) if status == 200 else None
    except ValueError:
        reply = None
    return status, reply


def tool(name, args, **kw):
    return rpc('tools/call', {'name': name, 'arguments': args}, **kw)


def success(status, reply):
    return status == 200 and isinstance(reply, dict) and 'error' not in reply and not reply.get('result', {}).get('isError', False)


def decoded(reply):
    text = reply['result']['content'][0]['text']
    return json.loads(text)


try:
    for tls_version, label in [(ssl.TLSVersion.TLSv1_2, 'tls12'), (ssl.TLSVersion.TLSv1_3, 'tls13')]:
        tlsctx = ssl.create_default_context(cafile=ca)
        tlsctx.minimum_version = tlsctx.maximum_version = tls_version
        with socket.create_connection(('127.0.0.1', 8443), timeout=5) as raw:
            with tlsctx.wrap_socket(raw, server_hostname='localhost') as secure:
                record(label + '_trusted_handshake', secure.version() in ['TLSv1.2', 'TLSv1.3'])
    for trust, hostname, port, label in [(ssl.create_default_context(), 'localhost', 8443, 'untrusted_ca'), (ctx, 'wrong.invalid', 8443, 'wrong_hostname'), (ctx, 'localhost', 18443, 'expired_leaf')]:
        rejected = False
        try:
            with socket.create_connection(('127.0.0.1', port), timeout=5) as raw:
                with trust.wrap_socket(raw, server_hostname=hostname):
                    pass
        except ssl.SSLCertVerificationError:
            rejected = True
        record(label + '_rejected', rejected)
    for tls in [False, True]:
        status, reply = rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': MARK, 'version': MARK}}, tls=tls)
        record('initialize_https' if tls else 'initialize_http', success(status, reply), http=status)
        status, _ = request({'jsonrpc': '2.0', 'method': 'notifications/initialized'}, tls=tls)
        record('initialized_https' if tls else 'initialized_http', status == 202, http=status)
        status, reply = rpc('tools/list', tls=tls)
        names = {x['name'] for x in reply['result']['tools']} if success(status, reply) else set()
        record('eight_tools_https' if tls else 'eight_tools_http', names == EXPECTED, tool_count=len(names))

    operations = [
        ('list_knowledge_bases', {}),
        ('search_knowledge', {'query': 'PUBLIC', 'mode': 'hybrid', 'limit': 5}),
        ('grep_chunks', {'query': 'PUBLIC', 'limit': 5}),
        ('list_documents', {'knowledge_base_id': 'public-two'}),
        ('read_document', {'knowledge_id': 'public-two-doc'}),
        ('wiki_search', {'query': 'PUBLIC'}),
        ('wiki_read_page', {'slug': 'concept/verified', 'knowledge_base_id': 'public-two'}),
        ('wiki_index', {'knowledge_base_id': 'public-two'}),
    ]
    for name, args in operations:
        status, reply = tool(name, args)
        ok = success(status, reply)
        if ok:
            value = decoded(reply)
            # Validate useful query results, not merely HTTP 200.
            if name in ['search_knowledge', 'grep_chunks']:
                ok = len(value.get('results', [])) > 0 and value.get('failed_kbs', 1) == 0
            if name.startswith('wiki_'):
                ok = len(value.get('pages', [])) > 0
            serialized = json.dumps(value)
            ok = ok and all(x not in serialized for x in ['SIGNED_LINK_CANARY', 'SYNTHETIC_PATH', 'file://', 'https://storage.invalid', '/etc/passwd'])
        record('call_' + name, ok, http=status)
        time.sleep(.22)
    status, reply = tool('search_knowledge', {'query': MARK, 'mode': 'semantic', 'limit': 1})
    record('normal_query_canary_exercises_model', success(status, reply), http=status)

    status, a = tool('list_knowledge_bases', {})
    status2, b = tool('list_knowledge_bases', {}, tls=False)
    record('same_key_two_entries_same_scope', success(status, a) and success(status2, b) and decoded(a) == decoded(b))
    status, other = tool('list_knowledge_bases', {}, key=runtime['key_c'])
    record('different_user_scope', success(status, other) and all(x['id'] != 'private-home' for x in decoded(other)))
    status, other = tool('read_document', {'knowledge_id': 'private-home-doc'}, key=runtime['key_c'])
    record('other_user_private_document_denied', status == 403 or (status == 200 and bool(other.get('result', {}).get('isError'))))
    status, other = tool('read_document', {'knowledge_id': 'sensitive'})
    record('unauthorized_document_denied', status == 403 or (status == 200 and bool(other.get('result', {}).get('isError'))))

    # The synthetic marker is intentionally submitted in every forbidden input;
    # it must not be echoed into the access/debug/usage logs.
    for name in ['ask', 'add_document', 'update_document', 'delete_document', 'upload_file', 'http_proxy', 'execute_agent', 'future_internal_upload']:
        body = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call', 'params': {'name': name, 'arguments': {'content': MARK, 'url': 'https://files.invalid/' + MARK}}}
        for tls in [False, True]:
            status, raw = request(body, tls=tls)
            record('deny_' + name + ('_tls' if tls else '_go'), status in [400, 403], http=status)
            time.sleep(.22)
    for field in ['content', 'file', 'base64', 'path', 'url']:
        status, _ = request({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call', 'params': {'name': 'search_knowledge', 'arguments': {'query': 'PUBLIC', field: MARK}}})
        record('json_disguised_' + field, status == 400, http=status)
        time.sleep(.22)
    malformed = [
        b'{"jsonrpc":"2.0","id":1,"method":"tools/list","method":"tools/call"}',
        b'[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]',
        b'{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":' + b'[' * 20 + b'0' + b']' * 20 + b'}}',
        b'{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":"\xff"}}',
    ]
    for i, payload in enumerate(malformed):
        status, _ = request(payload)
        record('bounded_json_envelope_' + str(i), status == 400, http=status)
    for ext in ['pdf', 'docx', 'xlsx', 'zip', 'go']:
        body = ('--p1\r\nContent-Disposition: form-data; name="file"; filename="x.' + ext + '"\r\n\r\n' + MARK + '\r\n--p1--\r\n').encode()
        for tls in [False, True]:
            status, _ = request(body, media='multipart/form-data; boundary=p1', tls=tls)
            record('multipart_' + ext + ('_tls' if tls else '_go'), status == 415, http=status)
            time.sleep(.22)

    paths = ['/', '/api/v1/knowledge-bases/x/knowledge/file', '/api/v1/knowledge-bases/x/knowledge/url',
             '/api/v1/knowledge-bases/x/knowledge/manual', '/api/v1/chunks/x/y', '/api/v1/knowledgebase/x/wiki/pages',
             '/api/v1/knowledge-bases/x/faq/entries', '/api/v1/sessions/x/attachments', '/api/v1/skills/catalog',
             '/api/v1/system/admin', '/mcp/team', '/files', '/r/token', '/api/v1/files/presigned',
             '/internal/mcp', '/health', PATH + '/', PATH + '?query=' + MARK, '/api/v1/mcp/%70ersonal',
             '/api//v1/mcp/personal', '/api/v1/mcp/../mcp/personal']
    for i, path in enumerate(paths):
        status, _ = request(MARK.encode(), path=path)
        record('rest_or_noncanonical_path_' + str(i), status in [400, 404], http=status)
        time.sleep(.22)
    for method in ['PUT', 'PATCH', 'HEAD', 'OPTIONS', 'TRACE', 'CONNECT']:
        status, _ = request(None, method=method, media=None)
        # Nginx rejects CONNECT's unsupported request-target during parsing,
        # before location method handling; both refusals prevent forwarding.
        record('http_method_' + method, status in ([400, 405] if method == 'CONNECT' else [405]), http=status)
    time.sleep(2)
    good = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/list'}).encode()
    for size in [1 << 20, (1 << 20) + 1]:
        for tls in [False, True]:
            status, _ = request(good + b' ' * (size - len(good)), tls=tls)
            record('size_' + str(size) + ('_tls' if tls else '_go'), status == (200 if size == 1 << 20 else 413), http=status)
    status, _ = request(good, extra={'Content-Encoding': 'gzip'})
    record('compressed_payload_denied', status == 415, http=status)
    status, _ = request(good, extra={'Origin': 'https://attacker.invalid'})
    record('foreign_origin_denied', status == 403, http=status)
    status, _ = request(good, extra={'Host': 'attacker.invalid'})
    record('unapproved_host_denied', status == 421, http=status)
    status, reply = request(good, extra={'X-Isolated': 'false', 'X-Environment': 'internal', 'X-Forwarded-For': '127.0.0.2'})
    record('environment_header_cannot_change_registry', status == 200 and {x['name'] for x in json.loads(reply)['result']['tools']} == EXPECTED)
    status, _ = request(good, key=False)
    record('missing_key_denied', status == 401, http=status)
    status, _ = request(good, key='synthetic-invalid-key')
    record('invalid_key_denied', status == 401, http=status)
    # Concurrent admission must be bounded; 429 is an expected rejection.
    with ThreadPoolExecutor(max_workers=16) as pool:
        statuses = list(pool.map(lambda _: request(good)[0], range(60)))
    record('concurrency_and_rate_limit', 429 in statuses and all(s in [200, 429] for s in statuses), accepted=statuses.count(200), limited=statuses.count(429))
    time.sleep(9)  # refill the deliberately small proxy burst before timeout checks
    start = time.monotonic()
    with ThreadPoolExecutor(max_workers=4) as pool:
        slow = [pool.submit(tool, 'search_knowledge', {'query': 'P1_SLOW_QUERY', 'mode': 'semantic', 'limit': 5}) for _ in range(4)]
        active = 0
        for _ in range(30):
            metrics = http.client.HTTPConnection('127.0.0.1', 18645, timeout=3)
            metrics.request('GET', '/metrics')
            active = json.loads(metrics.getresponse().read())['slow_active']
            metrics.close()
            if active == 4:
                break
            time.sleep(.2)
        record('four_isolated_requests_reach_model', active == 4)
        status, _ = request(good)
        record('global_isolation_concurrency_bound', status == 429, http=status)
        status, reply = rpc('tools/list', tls=False)
        record('internal_entry_remains_available_under_isolated_load', success(status, reply), http=status)
        ended = [f.result(timeout=40) for f in slow]
    elapsed = time.monotonic() - start
    record('request_deadline_enforced', elapsed < 35 and all(not success(status, reply) for status, reply in ended), elapsed_seconds=round(elapsed, 2))
    status, reply = rpc('tools/list')
    record('slots_released_after_timeout', success(status, reply), http=status)
    for method in ['GET', 'DELETE']:
        status, _ = request(None, method=method, media=None)
        record('streamable_' + method.lower(), status in [200, 202, 204, 405], http=status)
    status, _ = request(good, media='application/json; charset=utf-8')
    record('json_charset_compatible', status == 200, http=status)
    time.sleep(1)
    corpus = b''
    for p in [BASE / 'app.log', BASE / 'logs/nginx-access.log', BASE / 'usage-metadata.jsonl']:
        if p.exists():
            corpus += p.read_bytes()
    debug_files = list((BASE / 'private/llm-debug').glob('*'))
    if PHASE == 'fullapp':
        container = 'knowhub-isolated-p1-20261111-app'
        scope = subprocess.check_output(['docker', 'inspect', '--format', '{{index .Config.Labels "knowhub.scope"}}', container]).decode().strip()
        assert scope == 'knowhub-isolated-p1-20261111'
        # The test APP runs as the stock image user. Read its protected log
        # mount through the already authorized Docker API, never chmod it.
        corpus += subprocess.check_output(['docker', 'exec', container, 'sh', '-c', 'find /p1/logs -type f -exec cat {} +'], stderr=subprocess.DEVNULL)
        count = int(subprocess.check_output(['docker', 'exec', container, 'sh', '-c', 'find /p1/logs/llm-debug -type f | wc -l'], stderr=subprocess.DEVNULL).decode())
        debug_files.extend([None] * count)
        metrics = http.client.HTTPConnection('127.0.0.1', 18645, timeout=3)
        metrics.request('GET', '/metrics')
        counts = json.loads(metrics.getresponse().read())
        metrics.close()
        record('apm_query_canary_absent', counts.get('apm_canary_exports') == 0)
    credential_absent = all(runtime[k].encode() not in corpus for k in ['key_a', 'key_c'])
    record('log_canary_and_credentials_absent', MARK.encode() not in corpus and credential_absent and not debug_files, debug_file_count=len(debug_files))
    (BASE / 'results' / (RESULT + '-done.json')).write_text(json.dumps({'done': True, 'checks': len(rows), 'all_pass': all(r['status'] == 'PASS' for r in rows)}))
except Exception as exc:
    # No exception text: HTTP libraries can include a URL or payload in errors.
    record('harness_terminated', False, error_class=type(exc).__name__)
    (BASE / 'results' / (RESULT + '-done.json')).write_text(json.dumps({'done': True, 'all_pass': False}))
