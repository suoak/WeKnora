"""Independent post-deployment read-only verification. Never logs in to SID."""
import urllib.request

cand = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
result = json.loads((cand / 'deployment-result.json').read_text())
assert result['result'] == 'PASS' and result['upgrade_completed']
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()
current = {x['service']: x for x in (metadata(cid) for cid in ids)}
for kind in ('app', 'frontend'):
    assert current[kind]['id'] == result['containers_after'][kind]['id']
    assert current[kind]['running']
    assert current[kind]['restart_count'] == 0
    assert current[kind]['image_id'] == result['containers_after'][kind]['image_id']
for kind in set(current) - {'app', 'frontend'}:
    assert current[kind] == result['containers_before'][kind]
assert current['app']['health'] == 'healthy'
flags = {}
for key, expected in result['effective_flags'].items():
    value = run(['docker', 'exec', current['app']['id'], 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
    assert value == expected
    flags[key] = value
revisions = {}
for kind in ('app', 'frontend'):
    value = run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', current[kind]['image_id']]).decode().strip()
    assert value == result['target_sha']
    revisions[kind] = value
pg = current['postgres']['id']
def sql(query):
    return subprocess.run(
        ['docker', 'exec', '-i', pg, 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -At'],
        input=query.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout.decode().strip()
schema = sql('SELECT version,dirty FROM schema_migrations')
assert schema == '3027|f'
rows = {table: int(sql('SELECT COUNT(*) FROM ' + table)) for table in result['table_counts_before']}
assert int(sql('SELECT COUNT(*) FROM source_id_local_sessions')) == 0
active_kb = int(sql('SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL'))
backup_audit = json.loads((cand / 'fresh-backup-audit-result.json').read_text())
assert backup_audit['result'] == 'PASS'
assert active_kb == backup_audit['active_knowledge_bases']
assert rows['knowledge_bases'] == backup_audit['row_counts']['knowledge_bases'] + 1
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
http = {}
for port, path in ((80, '/'), (80, '/login'), (8080, '/health'), (80, '/api/v1/auth/oidc/config')):
    with opener.open('http://127.0.0.1:' + str(port) + path, timeout=10) as response:
        http[str(port) + path] = response.status
        assert response.status == 200
        if path.endswith('/config'):
            config = json.loads(response.read(65536))
            assert config['sourceid_basic_encoding'] == 'oauth_form'
            assert config['enabled'] is True
            assert config['redirect_uri'] == 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
backups = []
for name in ('deployment-attempt1.json', 'deployment-result.json'):
    receipt = json.loads((cand / name).read_text())
    path = pathlib.Path(receipt['fresh_database_backup']['path'])
    assert path.stat().st_mode & 0o777 == 0o600
    assert path.parent.stat().st_mode & 0o777 == 0o700
    backups.append({'path': str(path), 'bytes': path.stat().st_size, 'permissions': '0600'})
for filename in ('candidate-image-pin.yml', 'rollback-oauth-form-image-pin.yml'):
    assert (cand / filename).is_file()
temporary = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business-phaseb-migration', '--format', '{{.ID}}']).decode().strip()
assert temporary == ''
print(json.dumps({'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'containers': current,
                  'revisions': revisions, 'flags': flags, 'schema': schema, 'row_counts': rows,
                  'active_knowledge_bases': active_kb, 'local_enterprise_sessions': 0, 'http': http,
                  'protected_database_backups': backups, 'migration_container_removed': True,
                  'dependencies_unchanged': True, 'real_sid_login': 'NOT_EXECUTED', 'result': 'PASS'}))
