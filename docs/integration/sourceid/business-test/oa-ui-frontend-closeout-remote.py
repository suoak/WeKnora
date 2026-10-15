"""Read-only independent frontend release closeout; no auth or config mutation."""
base = pathlib.Path('/home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f')
receipt = json.loads((base / 'frontend-deployment-result.json').read_text())
assert receipt['result'] == 'PASS' and not receipt['rolled_back']
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()
current = {x['service']: x for x in (metadata(cid) for cid in ids)}
def canonical(row):
    return dict(row, mounts=sorted(row['mounts'], key=lambda x: x['Destination']))
for service in current:
    assert canonical(current[service]) == canonical(receipt['containers_after'][service])
    if service != 'frontend': assert canonical(current[service]) == canonical(receipt['containers_before'][service])
assert current['app']['health'] == 'healthy'
assert current['frontend']['running'] and current['frontend']['restart_count'] == 0
revisions = {}
for service in ('app', 'frontend'):
    revisions[service] = run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', current[service]['image_id']]).decode().strip()
assert revisions == {'app': receipt['app_sha'], 'frontend': receipt['frontend_sha']}
flags = {}
for key, expected in receipt['effective_flags'].items():
    value = run(['docker', 'exec', current['app']['id'], 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
    assert value == expected
    flags[key] = value
schema = run(['docker', 'exec', current['postgres']['id'], 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
assert schema == '3027|f'
backup = pathlib.Path(receipt['backup_directory'])
assert backup.stat().st_mode & 0o777 == 0o700
assert all(p.stat().st_mode & 0o777 == 0o600 for p in backup.iterdir() if p.is_file())
for image in (receipt['containers_before']['frontend']['image_id'], current['frontend']['image_id']):
    assert run(['docker', 'image', 'inspect', '--format', '{{.Id}}', image]).decode().strip() == image
asset_hashes = {}
for name in ('main-CJrpCTow.js', 'Login-BwWSdVPg.js'):
    value = run(['docker', 'exec', current['frontend']['id'], 'sha256sum', '/usr/share/nginx/html/assets/' + name]).decode().split()[0]
    assert re.fullmatch(r'[a-f0-9]{64}', value)
    asset_hashes['/assets/' + name] = value
privacy = {}
for service in ('app', 'frontend'):
    p = subprocess.run(['docker', 'logs', '--since', receipt['started_utc'], current[service]['id']], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
    assert p.returncode == 0
    raw = p.stdout
    patterns = (rb'Authorization:\s*(?:Basic|Bearer)\s+[A-Za-z0-9+/=_-]{8,}',
                rb'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+',
                rb'\b(?:OC|AT)-[0-9]+-[A-Za-z0-9_-]+')
    found = any(re.search(pattern, raw) for pattern in patterns)
    privacy[service] = {'bytes_scanned': len(raw), 'credential_pattern_found': found}
    assert not found
print(json.dumps({'result': 'PASS', 'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  'containers': current, 'revisions': revisions, 'flags': flags, 'schema': schema,
                  'all_non_frontend_containers_unchanged': True, 'backup_permissions': '0700/0600',
                  'old_frontend_image_retained': True, 'asset_sha256': asset_hashes, 'log_privacy': privacy,
                  'real_sid_login': 'NOT_EXECUTED', 'rolled_back': False}))
