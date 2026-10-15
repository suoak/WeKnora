"""Read-only candidate image checks, isolated nginx smoke test and release pins.

Writes only the new candidate directory; never applies Compose overrides.
"""
import hashlib
import socket
import urllib.request
import yaml
base = pathlib.Path(UI_BUILD_DIRECTORY)
manifest = json.loads((base / 'source-manifest.json').read_text())
sha = manifest['sha']
state = json.loads((base / 'build-status.json').read_text())
if state['state'] != 'COMPLETE':
    print(json.dumps({'state': state, 'candidate_ready': False, 'running_services_changed': False}))
else:
    tag = 'knowhub-sourceid-phaseb/frontend:oa-ui-' + sha[:9]
    iid = run(['docker', 'image', 'inspect', '--format', '{{.Id}}', tag]).decode().strip()
    labels = json.loads(run(['docker', 'image', 'inspect', '--format', '{{json .Config.Labels}}', iid]))
    assert labels['org.opencontainers.image.revision'] == sha
    assert labels['knowhub.compatible_app_revision'] == '431226c3b1bd5338ec06c1bd0520e637d5fed398'
    app_id = run(['docker', 'ps', '--filter', 'publish=8080', '--format', '{{.ID}}']).decode().strip()
    app = metadata(app_id)
    assert app['id'] == 'ceac394ee4632f5514cbb65821e77c369df63b35ee1aba7d291fab8a9fdb6ea5'
    assert app['image_id'] == 'sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
    containers = [metadata(cid) for cid in run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()]
    old_frontend = next(x for x in containers if x['service'] == 'frontend')
    assert old_frontend['image_id'] == 'sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1'
    image_files = run(['docker', 'run', '--rm', '--network', 'none', '--log-driver', 'none', '--entrypoint', 'sh', iid,
                       '-c', 'cd /usr/share/nginx/html && find . -type f -exec sha256sum {} \\;'], timeout=60).decode()
    observed = {line.split(None, 1)[1][2:]: line.split(None, 1)[0] for line in image_files.splitlines()}
    dist = base / 'artifacts/dist'
    built = {str(p.relative_to(dist)): hashlib.sha256(p.read_bytes()).hexdigest() for p in dist.rglob('*') if p.is_file()}
    assert observed == built
    login_files = list((dist / 'assets').glob('Login-*.js'))
    assert len(login_files) == 1
    content = login_files[0].read_text()
    assert 'oaLogin' in content and 'sourceid_basic_encoding' in content and 'oidcLoginWithProvider' in content
    networks = inspect(app_id, '.NetworkSettings.Networks')
    assert len(networks) == 1
    network = next(iter(networks))
    name = 'knowhub-oa-ui-smoke-' + sha[:9]
    created = False
    http = {}
    try:
        run(['docker', 'run', '-d', '--name', name, '--network', network, '--memory', '256m', '--log-driver', 'none',
             '-e', 'APP_HOST=knowhub-sourceid-business-app-1', iid]); created = True
        address = inspect(name, '.NetworkSettings.Networks')[network]['IPAddress']
        import time
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for _ in range(30):
            try:
                with opener.open('http://' + address + '/login', timeout=3) as r:
                    if r.status == 200: break
            except Exception: pass
            time.sleep(1)
        else: raise RuntimeError('candidate_nginx_timeout')
        for path in ('/login', '/api/v1/auth/oidc/config'):
            with opener.open('http://' + address + path, timeout=10) as r:
                assert r.status == 200
                http[path] = r.status
                if path.endswith('/config'):
                    config = json.loads(r.read(65536))
                    assert config['sourceid_basic_encoding'] == 'oauth_form'
                    http['sourceid_marker_compatible'] = True
    finally:
        if created: run(['docker', 'rm', '-f', name])
    current_pin = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml')
    for filename, image in (('frontend-ui-pin.yml', iid), ('frontend-ui-rollback-pin.yml', old_frontend['image_id'])):
        p = base / filename
        p.write_text(yaml.safe_dump({'services': {'frontend': {'image': image, 'pull_policy': 'never'}}}))
        p.chmod(0o600)
    pg = next(x for x in containers if x['service'] == 'postgres')
    schema = run(['docker', 'exec', pg['id'], 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
    assert schema == '3027|f'
    after = [metadata(cid) for cid in run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()]
    def canonical(rows):
        return sorted([dict(row, mounts=sorted(row['mounts'], key=lambda x: x['Destination'])) for row in rows], key=lambda x: x['id'])
    assert canonical(containers) == canonical(after)
    result = {'candidate_ready': True, 'sha': sha, 'tag': tag, 'image_id': iid, 'revision': labels['org.opencontainers.image.revision'],
              'frontend_file_count': len(built), 'image_matches_built_dist': True, 'sourceid_and_other_provider_presentation_present': True,
              'http_compatibility': http, 'current_app': app, 'current_frontend': old_frontend,
              'database_schema': schema, 'running_services_changed': False, 'smoke_container_removed': True,
              'current_phaseb_pin': str(current_pin), 'candidate_override': str(base / 'frontend-ui-pin.yml'),
              'rollback_override': str(base / 'frontend-ui-rollback-pin.yml'), 'overrides_applied': False}
    (base / 'candidate-result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))
