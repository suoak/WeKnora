import pathlib, json, subprocess, tarfile, hashlib

b = pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
assert json.loads((b / 'status.json').read_text())['result'] == 'PASS'
assert not subprocess.check_output(['docker', 'ps', '-aq', '--filter', 'name=^oa-accountprep-pg-20261016$']).strip()
with tarfile.open(b / 'overlay-new.tar.gz') as archive:
    for item in archive.getmembers():
        assert item.isfile() and not item.issym()
        (b / 'source' / item.name).resolve().relative_to((b / 'source').resolve())
    archive.extractall(b / 'source')
(b / 'artifacts/tests-initial.log').write_bytes((b / 'artifacts/tests.log').read_bytes())
source_hashes = {}
for name in ('internal/accountprep/prepare.go', 'internal/accountprep/prepare_test.go', 'cmd/sourceid-test-account/main.go', 'cmd/sourceid-test-account/main_test.go'):
    source_hashes[name] = hashlib.sha256((b / 'source' / name).read_bytes()).hexdigest()
(b / 'source-checksums.json').write_text(json.dumps(source_hashes))
with (b / 'worker.log').open('wb') as f:
    q = subprocess.Popen(['python3', str(b / 'worker.py')], stdout=f, stderr=subprocess.STDOUT, start_new_session=True)
print(json.dumps({'result': 'REFRESHED_FINAL_SOURCE', 'pid': q.pid, 'source_sha256': source_hashes, 'business_database_accessed': False}))
