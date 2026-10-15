"""Finalize candidate-only files and read-only evidence; never apply Compose."""
import hashlib
import os
import urllib.request
import yaml

os.umask(0o077)
base = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
live = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
sha = '431226c3b1bd5338ec06c1bd0520e637d5fed398'
receipt = json.loads((base / 'rehearsal-result.json').read_text())
assert receipt['result'] == 'PASS' and all(receipt['isolated_cleanup'].values())
cid = run(['docker', 'ps', '--filter', 'publish=8080', '--format', '{{.ID}}']).decode().strip()
app = metadata(cid)
assert app['id'] == '89f47e199a142788e5203e826d3f7298b56bea67515846d35d4d6454e1d0415a'
assert app['working_dir'] == str(live)
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=' + app['project'], '--format', '{{.ID}}']).decode().split()
containers = [metadata(x) for x in ids]
flags = {}
for key in ('SOURCEID_BASIC_ENCODING', 'SOURCEID_PROTOCOL_ONLY', 'SOURCEID_CONTRACT_CONFIRMED',
            'SOURCEID_AUTO_BINDING_ENABLE', 'SOURCEID_EMAIL_BINDING_ENABLE', 'SOURCEID_EMAIL_CONTRACT_CONFIRMED',
            'SOURCEID_SESSION_MODE', 'SOURCEID_LOGOUT_ENABLE', 'SOURCEID_DIAGNOSTICS_ENABLE', 'OIDC_AUTH_ENABLE',
            'OIDC_AUTH_PKCE_ENABLE', 'AUTO_MIGRATE', 'AUTO_RECOVER_DIRTY'):
    value = run(['docker', 'exec', cid, 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
    assert value in ('true', 'false', 'oauth_form', 'external', 'NOT_SET')
    flags[key] = value
assert flags['SOURCEID_BASIC_ENCODING'] == 'oauth_form'
assert flags['SOURCEID_PROTOCOL_ONLY'] == 'true'
assert flags['SOURCEID_CONTRACT_CONFIRMED'] == flags['SOURCEID_AUTO_BINDING_ENABLE'] == 'false'
assert flags['SOURCEID_SESSION_MODE'] in ('external', 'NOT_SET')
pg = next(x for x in containers if x['service'] == 'postgres')
schema = run(['docker', 'exec', pg['id'], 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
assert schema == '3026|f'
assert not run(['docker', 'ps', '-a', '--filter', 'name=sourceid-phaseb-rehearsal-431226c3b-', '--format', '{{.ID}}']).strip()
assert not run(['docker', 'network', 'ls', '--filter', 'name=sourceid-phaseb-rehearsal-431226c3b-net', '--format', '{{.ID}}']).strip()

# Reviewable override files, confined to the independent preflight directory.
# These are NOT applied, and no real credential/configuration value is copied.
candidate = {'services': {kind: {'image': record['id'], 'pull_policy': 'never'}
                         for kind, record in receipt['candidate_images'].items()}}
candidate['services']['app']['environment'] = {
    'SOURCEID_BASIC_ENCODING': 'oauth_form', 'SOURCEID_PROTOCOL_ONLY': 'true',
    'SOURCEID_CONTRACT_CONFIRMED': 'false', 'SOURCEID_AUTO_BINDING_ENABLE': 'false'}
rollback = {'services': {x['service']: {'image': x['image_id'], 'pull_policy': 'never'}
                        for x in containers if x['service'] in ('app', 'frontend')}}
rollback['services']['app']['environment'] = dict(candidate['services']['app']['environment'])
files = {}
for name, content in (('candidate-image-pin.yml', candidate), ('rollback-oauth-form-image-pin.yml', rollback)):
    path = base / name
    path.write_text(yaml.safe_dump(content, sort_keys=False))
    path.chmod(0o600)
    files[name] = {'path': str(path), 'mode': '0600', 'applied': False}
assert rollback['services']['app']['image'] == app['image_id']
base_doc = yaml.safe_load((live / 'compose.yml').read_text())
old_pin = pathlib.Path(app['compose_files'].split(',')[1])
old_doc = yaml.safe_load(old_pin.read_text())
assert old_doc['services']['app']['image'] == app['image_id']
assert candidate['services']['app']['image'] != old_doc['services']['app']['image']

# Check bytes in images, not tags alone. No process is connected to SID/DB.
artifacts = {}
for kind, record in receipt['candidate_images'].items():
    observed_revision = run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', record['id']]).decode().strip()
    assert observed_revision == sha
    if kind == 'app':
        image_hash = run(['docker', 'run', '--rm', '--network', 'none', '--log-driver', 'none', '--entrypoint', 'sha256sum', record['id'], '/app/WeKnora']).decode().split()[0]
        built_hash = hashlib.sha256((base / 'artifacts/WeKnora').read_bytes()).hexdigest()
        assert image_hash == built_hash
        artifacts[kind] = {'revision': observed_revision, 'binary_sha256': image_hash, 'built_artifact_matches_image': True}
    else:
        output = run(['docker', 'run', '--rm', '--network', 'none', '--log-driver', 'none', '--entrypoint', 'sh', record['id'],
                      '-c', 'cd /usr/share/nginx/html && find . -type f -exec sha256sum {} \\;'], timeout=60).decode()
        image_files = {line.split(None, 1)[1][2:]: line.split(None, 1)[0] for line in output.splitlines()}
        dist = base / 'artifacts/dist'
        built_files = {str(path.relative_to(dist)): hashlib.sha256(path.read_bytes()).hexdigest() for path in dist.rglob('*') if path.is_file()}
        assert image_files == built_files and 'index.html' in image_files
        artifacts[kind] = {'revision': observed_revision, 'file_count': len(image_files), 'built_artifact_matches_image': True}

opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
http = {}
for port, path in ((80, '/'), (8080, '/health'), (80, '/api/v1/auth/oidc/config')):
    with opener.open('http://127.0.0.1:' + str(port) + path, timeout=10) as response:
        http[str(port) + path] = response.status
        assert response.status == 200
backup = receipt['database_backup']
assert pathlib.Path(backup['path']).stat().st_mode & 0o777 == 0o600
for item in receipt['protected_configuration_backups']:
    assert pathlib.Path(item['backup']).stat().st_mode & 0o777 == 0o600
print(json.dumps({'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'live_containers': containers,
                  'live_flags': flags, 'live_schema': schema, 'http': http, 'candidate_artifacts': artifacts,
                  'backup_permissions_verified': True, 'candidate_only_files': files,
                  'old_pin_would_override_new_app': True, 'isolated_resources_removed': True,
                  'live_version_changed': False, 'live_database_migrated': False, 'real_sid_login': 'NOT_EXECUTED'}))
