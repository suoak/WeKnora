import importlib.util
import pathlib
import copy
import json
import contextlib
import io

root = pathlib.Path(__file__).resolve()
spec = importlib.util.spec_from_file_location('exporter', root.with_name('export_token_diagnostics.py'))
exporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(exporter)
# Reuse the independently validated canonical transport fixture.
fixture_path = root.parents[1] / 'linux-probe' / 'probe_report_filter_test.py'
spec = importlib.util.spec_from_file_location('filter_fixtures', fixture_path)
fixtures = importlib.util.module_from_spec(spec)
with contextlib.redirect_stdout(io.StringIO()):
    spec.loader.exec_module(fixtures)

transport = fixtures.transport
event = {'event': 'sourceid_token_send', 'trace': transport['trace'], 'transport': transport}
assert exporter.extract(json.dumps(event)) == event
assert exporter.extract('WARN log prefix '+json.dumps(event)+' trailing logger text') == event
assert exporter.extract(json.dumps({'message':json.dumps(event)})) == event
bad = copy.deepcopy(event)
bad['raw_body'] = 'synthetic-secret'
assert exporter.extract(json.dumps(bad)) is None
bad = copy.deepcopy(event)
bad['transport']['server_class'] = 'nginx/synthetic-secret'
assert exporter.extract(json.dumps(bad)) is None
bad = copy.deepcopy(event)
bad['trace'] = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
assert exporter.extract(json.dumps(bad)) is None
bad = copy.deepcopy(event)
bad['transport']['network_events'][0]['authorization'] = 'Basic synthetic-private'
assert exporter.extract(json.dumps(bad)) is None
assert exporter.extract('x'*65537) is None
assert exporter.extract('raw token=synthetic-private') is None
failure = {'event': 'sourceid_id_token_failure', 'trace': transport['trace'], 'categories': ['key_selection'], 'at_hash_validation': 'NOT_IMPLEMENTED'}
assert exporter.extract(json.dumps(failure)) == failure
for patch in [{'categories': ['synthetic-secret']}, {'raw_error': 'synthetic-token'}, {'categories': []}, {'at_hash_validation': 'PASS'}]:
    assert exporter.extract(json.dumps({**failure, **patch})) is None
for evidence in ['NOT_PROVIDED_CONTRACT_REQUIRED', 'INVALID_OR_STALE', 'SIGNED_AUTH_TIME_VERIFIED']:
    reauth = {'event': 'sourceid_reauthentication', 'trace': transport['trace'], 'evidence': evidence}
    assert exporter.extract(json.dumps(reauth)) == reauth
    assert exporter.extract(json.dumps({**reauth, 'auth_time': 1234567890})) is None
    assert exporter.extract(json.dumps({**reauth, 'subject': 'synthetic-private'})) is None
    assert exporter.extract(json.dumps({**reauth, 'evidence': 'synthetic-private'})) is None
print(json.dumps({'exporter': 'PASS', 'unknown_raw_fields_rejected': True, 'trace_binding_checked': True, 'reauthentication_enum_only': True}))
