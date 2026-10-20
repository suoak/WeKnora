"""Approved packaging-only frontend replacement; no migration or app writes.

Existing synthetic local login is used only for normal acceptance; no SID auth.
"""
import fcntl
import os
import shutil
import time
import urllib.error
import urllib.request
import yaml

LIVE = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
CAND = pathlib.Path('/home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f')
PHASEB = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml')
PIN = CAND / 'frontend-ui-pin.yml'
ROLLBACK = CAND / 'frontend-ui-rollback-pin.yml'
IMAGE = 'sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
OLD = 'sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1'
SHA = '1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b'
PROJECT = 'knowhub-sourceid-business'
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def snapshot():
    ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=' + PROJECT, '--format', '{{.ID}}']).decode().split()
    return {x['service']: x for x in (metadata(cid) for cid in ids)}


def canonical(rows):
    return {key: dict(value, mounts=sorted(value['mounts'], key=lambda m: m['Destination'])) for key, value in rows.items()}


def flags(cid):
    result = {}
    for key, expected in EXPECTED['flags'].items():
        value = run(['docker', 'exec', cid, 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
        assert value == expected
        result[key] = value
    return result


def sql(pg, query):
    p = subprocess.run(['docker', 'exec', '-i', pg, 'sh', '-c',
                        'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -qAt'],
                       input=('BEGIN READ ONLY;\n' + query + ';\nROLLBACK;').encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    if p.returncode: raise RuntimeError('read_only_query_failed')
    return p.stdout.decode().strip()


def counts(pg):
    return {table: int(sql(pg, 'SELECT COUNT(*) FROM ' + table)) for table in (
        'users', 'tenants', 'tenant_members', 'knowledge_bases', 'external_identities', 'source_id_binding_reviews',
        'source_id_sessions', 'source_id_local_sessions', 'feishu_connections', 'feishu_personal_imports')}


def request(path, method='GET', payload=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token: headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request('http://127.0.0.1' + path, method=method, headers=headers,
                                 data=json.dumps(payload).encode() if payload is not None else None)
    try: response = opener.open(req, timeout=15)
    except urllib.error.HTTPError as error: response = error
    with response:
        raw = response.read(2097152)
        try: body = json.loads(raw)
        except ValueError: body = {}
        return response.status, body


def acceptance():
    result, token = {}, None
    try:
        for path in ('/', '/login', '/api/v1/auth/oidc/config'):
            status, data = request(path)
            assert status == 200
            result[path] = status
            if path.endswith('/config'):
                assert data['sourceid_basic_encoding'] == 'oauth_form'
                assert data['enabled'] is (EXPECTED['flags']['OIDC_AUTH_ENABLE'] == 'true')
                assert data['redirect_uri'] == 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
                result['oidc_config'] = {key: data[key] for key in ('sourceid_basic_encoding', 'enabled', 'redirect_uri')}
        status, body = request('/api/v1/auth/login', 'POST', LOCAL_FIXTURE)
        assert status == 200 and body.get('success') is True
        token = body['token']
        result['local_login'] = status
        for path in ('/api/v1/auth/me', '/api/v1/knowledge-bases', '/api/v1/mcp-endpoints', '/api/v1/me/feishu'):
            status, _ = request(path, token=token)
            assert status == 200
            result[path] = status
        status, data = request('/api/v1/system/info', token=token)
        assert status == 200 and data.get('data', data)['commit_id'] == '431226c3b'
        result['app_binary_commit'] = '431226c3b'
        return result
    finally:
        if token:
            result['local_logout'] = request('/api/v1/auth/logout', 'POST', token=token)[0]
            result['token_revoked'] = request('/api/v1/auth/me', token=token)[0] == 401
            assert result['local_logout'] == 200 and result['token_revoked']
        token = None


def compose(pin):
    return ['docker', 'compose', '--project-name', PROJECT, '--project-directory', str(LIVE),
            '-f', str(LIVE / 'compose.yml'), '-f', str(PHASEB), '-f', str(pin),
            'up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'frontend']


def wait_frontend(image):
    for _ in range(45):
        try:
            front = snapshot()['frontend']
            if front['running'] and front['image_id'] == image and request('/login')[0] == 200:
                return front
        except Exception: pass
        time.sleep(1)
    raise RuntimeError('frontend_health_timeout')


def main():
    os.umask(0o077)
    lock = (LIVE / 'private/oa-ui-frontend-deploy.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    result = {'target': '172.30.232.42', 'frontend_sha': SHA, 'app_sha': '431226c3b1bd5338ec06c1bd0520e637d5fed398',
              'result': 'FAILED', 'frontend_replaced': False, 'rolled_back': False, 'database_migration': 'NOT_EXECUTED',
              'real_sid_login': 'NOT_EXECUTED', 'real_identity_binding': 'NOT_EXECUTED'}
    changed, backup = False, None
    stage = 'precheck'
    try:
        before = snapshot()
        for kind in ('app', 'frontend'):
            assert before[kind]['id'] == EXPECTED['images'][kind]['container_id']
            assert before[kind]['image_id'] == EXPECTED['images'][kind]['image_id']
            assert before[kind]['working_dir'] == str(LIVE)
            assert run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', before[kind]['image_id']]).decode().strip() == EXPECTED['images'][kind]['revision']
        assert before['app']['health'] == 'healthy'
        assert before['frontend']['image_id'] == OLD
        pg = before['postgres']['id']
        assert sql(pg, 'SELECT version,dirty FROM schema_migrations') == '3027|f'
        baseline_counts = counts(pg)
        baseline_flags = flags(before['app']['id'])
        for pin, image in ((PIN, IMAGE), (ROLLBACK, OLD)):
            data = yaml.safe_load(pin.read_text())
            assert data == {'services': {'frontend': {'image': image, 'pull_policy': 'never'}}}
            assert run(['docker', 'image', 'inspect', '--format', '{{.Id}}', image]).decode().strip() == image
        assert run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', IMAGE]).decode().strip() == SHA
        result['precheck'] = 'PASS'
        result['containers_before'] = before
        result['flags_before'] = baseline_flags
        result['counts_before'] = baseline_counts
        result['baseline_acceptance'] = acceptance()

        stage = 'backup'
        backup = LIVE / 'private/oa-ui-frontend-backups' / datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
        backup.mkdir(mode=0o700, parents=True, exist_ok=False)
        sources = [LIVE / 'compose.yml', PHASEB, PIN, ROLLBACK]
        original_bytes = {str(p): p.read_bytes() for p in sources}
        for index, path in enumerate(sources):
            target = backup / (str(index) + '-' + path.name)
            shutil.copyfile(str(path), str(target)); target.chmod(0o600)
        # Metadata only; never saves env, Docker inspect or expanded Compose.
        (backup / 'containers-before.json').write_text(json.dumps(before, indent=2) + '\n')
        (backup / 'compose-order.json').write_text(json.dumps({'before': before['frontend']['compose_files'].split(','),
            'upgrade': [str(LIVE / 'compose.yml'), str(PHASEB), str(PIN)],
            'rollback': [str(LIVE / 'compose.yml'), str(PHASEB), str(ROLLBACK)]}, indent=2) + '\n')
        # Frontend nginx files have no employee credentials; preserve exact config.
        for name, container_path in (('frontend-nginx-template.conf', '/etc/nginx/templates/default.conf.template'),
                                     ('frontend-api-proxy.conf', '/etc/nginx/api-proxy.conf')):
            raw = run(['docker', 'exec', before['frontend']['id'], 'cat', container_path])
            target = backup / name; target.write_bytes(raw); target.chmod(0o600)
        result['backup_directory'] = str(backup)
        result['compose_order'] = [str(LIVE / 'compose.yml'), str(PHASEB), str(PIN)]

        stage = 'replace_frontend'
        result['started_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        changed = True
        run(compose(PIN), timeout=180)
        frontend = wait_frontend(IMAGE)
        after = snapshot()
        result['containers_after'] = after
        assert canonical({k: v for k, v in before.items() if k != 'frontend'}) == canonical({k: v for k, v in after.items() if k != 'frontend'})
        assert frontend['compose_files'].split(',') == result['compose_order']
        assert frontend['ports'] == before['frontend']['ports'] and frontend['mounts'] == before['frontend']['mounts']
        assert all(pathlib.Path(path).read_bytes() == value for path, value in original_bytes.items())
        assert flags(after['app']['id']) == baseline_flags
        assert sql(pg, 'SELECT version,dirty FROM schema_migrations') == '3027|f'
        assert counts(pg) == baseline_counts
        stage = 'acceptance'
        result['acceptance'] = acceptance()
        result['non_frontend_containers_unchanged'] = True
        result['database_core_rows_unchanged'] = True
        result['schema_after'] = '3027|f'
        result['effective_flags'] = flags(after['app']['id'])
        result['frontend_replaced'] = True
        result['result'] = 'PASS'
    except Exception as error:
        result['failure_stage'] = stage
        result['failure_category'] = type(error).__name__
        import traceback
        result['failure_lines'] = [f.lineno for f in traceback.extract_tb(error.__traceback__) if f.filename == '<stdin>']
        if changed:
            try:
                run(compose(ROLLBACK), timeout=180)
                wait_frontend(OLD)
                assert canonical({k: v for k, v in before.items() if k != 'frontend'}) == canonical({k: v for k, v in snapshot().items() if k != 'frontend'})
                assert flags(snapshot()['app']['id']) == baseline_flags
                result['rollback_acceptance'] = acceptance()
                result['rolled_back'] = True
            except Exception as rollback_error:
                result['rollback_failure_category'] = type(rollback_error).__name__
    finally:
        result['completed_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        (CAND / 'frontend-deployment-result.json').write_text(json.dumps(result, indent=2) + '\n')
        if backup: (backup / 'deployment-result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result))
        fcntl.flock(lock, fcntl.LOCK_UN); lock.close()
    return 0 if result['result'] == 'PASS' else 2


if __name__ == '__main__':
    raise SystemExit(main())
