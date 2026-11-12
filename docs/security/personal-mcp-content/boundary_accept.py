"""Real HTTP upload boundary probes for the disposable synthetic lab only."""
import base64
import json
import pathlib
import urllib.request
import urllib.error

B = pathlib.Path('/home/aqyfzx/ai/knowhub-personal-upload-r1-20261112')
source = (B / 'accept.py').read_text().split("check('schema3046-before'")[0]
source = source.replace("'accept-results.json'", "'boundary-results.json'")
exec(source)

upload = {
    'knowledge_base_id': 'upload-a-kb',
    'filename': 'boundary.txt',
    'content_base64': base64.b64encode(b'KNOWHUB boundary acceptance fixture').decode(),
    'idempotency_key': 'boundary-upload-0001',
}
for label, key, kb in [
    ('same-space', 'key_a', 'upload-b-kb'),
    ('viewer', 'key_a', 'upload-viewer-kb'),
    ('retrieve-only', 'key_read', 'upload-a-kb'),
    ('fixed-scope', 'key_fixed', 'upload-private-kb'),
]:
    response = call('upload_document', dict(upload, knowledge_base_id=kb), key)
    check('upload-denies-' + label, denied(response))

for name, extra in [
    ('local-path', {'filename': 'C:\\private\\document.txt'}),
    ('wrong-type', {'filename': 'boundary.pdf'}),
    ('url-import', {'url': 'http://127.0.0.1:18871/private'}),
    ('tenant-field', {'tenant_id': 2}),
]:
    check('upload-denies-' + name, denied(call('upload_document', dict(upload, **extra))))

def forged_request(isolated):
    path = '/internal/mcp/isolated/personal' if isolated else '/api/v1/mcp/personal'
    args = dict(upload, knowledge_base_id='upload-b-kb')
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                       'params': {'name': 'upload_document', 'arguments': args}}).encode()
    request = urllib.request.Request('http://127.0.0.1:18873' + path, data=body, headers={
        'Authorization': 'Bearer ' + keys['key_a'],
        'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream',
        'X-Tenant-ID': '2', 'X-User-ID': 'upload-B',
        'X-Environment': 'external', 'X-Isolated': 'false',
    })
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            result = json.load(response)
        return result.get('result', result)
    except urllib.error.HTTPError as error:
        return {'http_error': error.code}

check('external-forged-headers-denied', denied(forged_request(False)))
check('isolated-forged-headers-denied', denied(forged_request(True)))

response = call('upload_document', dict(upload, knowledge_base_id='upload-editor-kb',
                                      idempotency_key='boundary-editor-0001'))
check('upload-cross-space-editor', not denied(response))
editor_id = decoded(response)['document_id']
response = call('upload_document', dict(upload, knowledge_base_id='upload-private-kb',
                                      idempotency_key='boundary-private-0001'))
check('upload-private-owner', not denied(response))

response = call('upload_document', dict(upload, knowledge_base_id='upload-b-kb',
                                      idempotency_key='boundary-key-b-0001'), 'key_b')
check('upload-second-user-owner', not denied(response))
document_b = decoded(response)['document_id']
check('status-other-key-denied', denied(call('get_document_processing_status', {'document_id': document_b})))
check('status-own-key', not denied(call('get_document_processing_status', {'document_id': document_b}, 'key_b')))

sql("UPDATE kb_shares SET permission='viewer' WHERE id='upload-editor-share'")
try:
    check('upload-editor-revoked', denied(call('upload_document', dict(upload, knowledge_base_id='upload-editor-kb'))))
    check('status-editor-revoked', denied(call('get_document_processing_status', {'document_id': editor_id})))
finally:
    sql("UPDATE kb_shares SET permission='editor' WHERE id='upload-editor-share'")

sql("UPDATE tenant_api_keys SET capabilities='[\"retrieve\",\"enterprise_retrieve\"]' WHERE id=101")
try:
    check('upload-capability-revoked', denied(call('upload_document', upload)))
    tools = rpc('key_a', 'tools/list').get('tools', [])
    check('revoked-capability-eight-tools', len(tools) == 8)
finally:
    sql("UPDATE tenant_api_keys SET capabilities='[\"retrieve\",\"enterprise_retrieve\",\"add_content\"]' WHERE id=101")
check('schema3046', sql('select version from schema_migrations') == '3046')
(B / 'boundary-done.json').write_text(json.dumps({'pass': True, 'production_changed': False}))
