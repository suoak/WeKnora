"""Approved packaging-only 3027 migration and pinned app/frontend upgrade.

Shared read-only helpers, EXPECTED and synthetic LOCAL_FIXTURE arrive via SSH
stdin. Never call this without explicit deployment approval. No real SID login.
"""
import fcntl
import hashlib
import os
import shutil
import time
import urllib.error
import urllib.request
import yaml

LIVE = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
CAND = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
PROJECT = 'knowhub-sourceid-business'
SHA = '431226c3b1bd5338ec06c1bd0520e637d5fed398'
RESUME = globals().get('RESUME_AFTER_VERIFIED_RECOVERY', False)
APP = 'sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
FRONTEND = 'sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1'
PIN = CAND / 'candidate-image-pin.yml'
ROLLBACK = CAND / 'rollback-oauth-form-image-pin.yml'
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
no_redirect = urllib.request.build_opener(urllib.request.ProxyHandler({}), type('NoRedirect', (urllib.request.HTTPRedirectHandler,), {'redirect_request': lambda *args: None})())


def execute(args, timeout=120, input=None, output=None):
    p = subprocess.run(args, input=input, stdout=output if output is not None else subprocess.PIPE,
                       stderr=subprocess.PIPE, timeout=timeout)
    if p.returncode:
        raise RuntimeError('controlled_command_failed')
    return p.stdout


def snapshot():
    ids = execute(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=' + PROJECT, '--format', '{{.ID}}']).decode().split()
    return {x['service']: x for x in (metadata(cid) for cid in ids)}


def canonical(containers):
    return sorted([dict(x, mounts=sorted(x['mounts'], key=lambda m: m['Destination']))
                   for x in containers], key=lambda x: x['id'])


def flags(cid):
    result = {}
    for key in EXPECTED['live_flags']:
        value = execute(['docker', 'exec', cid, 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
        assert value in ('true', 'false', 'oauth_form', 'external', 'NOT_SET')
        result[key] = value
    return result


def sql(pg, query):
    return execute(['docker', 'exec', '-i', pg, 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -At'], input=query.encode()).decode().strip()


def counts(pg):
    return {table: int(sql(pg, 'SELECT COUNT(*) FROM ' + table)) for table in (
        'users', 'tenants', 'tenant_members', 'knowledge_bases', 'external_identities', 'source_id_sessions',
        'source_id_binding_reviews', 'feishu_connections', 'feishu_personal_imports')}


def compose(pin):
    return ['docker', 'compose', '--project-name', PROJECT, '--project-directory', str(LIVE),
            '-f', str(LIVE / 'compose.yml'), '-f', str(pin)]


def request(path, method='GET', payload=None, token=None, extra=None, follow=True):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    headers.update(extra or {})
    req = urllib.request.Request('http://127.0.0.1' + path, method=method, headers=headers,
                                 data=json.dumps(payload).encode() if payload is not None else None)
    try:
        response = (opener if follow else no_redirect).open(req, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read(2097152)
        data = {}
        try:
            data = json.loads(raw)
        except ValueError:
            if response.headers.get('Content-Type', '').startswith('text/event-stream'):
                for line in raw.decode('utf-8', 'replace').splitlines():
                    if line.startswith('data: '):
                        data = json.loads(line[6:])
                        break
        return response.status, data, response.headers


def healthy(image):
    for _ in range(75):
        try:
            app = snapshot()['app']
            if app['running'] and app['image_id'] == image and app['health'] == 'healthy':
                with opener.open('http://127.0.0.1:8080/health', timeout=3) as r:
                    if r.status == 200:
                        return
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError('app_health_timeout')


def business_acceptance(expected_commit, extended=True):
    result, token, endpoint_token, kb, endpoint = {}, None, None, None, None
    secrets_in_memory = []
    try:
        status, data, _ = request('/api/v1/auth/login', 'POST', LOCAL_FIXTURE)
        assert status == 200 and data.get('success') is True
        token = data['token']
        secrets_in_memory.extend([token, LOCAL_FIXTURE['password']])
        result['local_login'] = 200
        for path in ('/api/v1/auth/me', '/api/v1/knowledge-bases', '/api/v1/agents', '/api/v1/models',
                     '/api/v1/mcp-endpoints', '/api/v1/mcp-endpoints/tools', '/api/v1/me/feishu'):
            status, data, _ = request(path, token=token)
            assert status == 200
            result[path] = status
        status, data, _ = request('/api/v1/system/info', token=token)
        assert status == 200 and data.get('data', data).get('commit_id') == expected_commit
        result['binary_commit_id'] = expected_commit
        status, data, _ = request('/api/v1/auth/oidc/config')
        assert status == 200 and data.get('sourceid_basic_encoding') == 'oauth_form'
        assert data.get('enabled') is (EXPECTED['live_flags']['OIDC_AUTH_ENABLE'] == 'true')
        assert data.get('redirect_uri') == 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
        result['oidc_config'] = {key: data.get(key) for key in ('enabled', 'sourceid_basic_encoding', 'redirect_uri')}
        if not extended:
            return result
        status, _, _ = request('/')
        assert status == 200
        result['frontend_80'] = status
        status, _, headers = request('/api/v1/auth/oidc/callback', follow=False)
        assert status == 302 and 'oidc_error=invalid_state' in headers.get('Location', '')
        result['missing_state_callback_safe_failure'] = 'PASS'

        # Only new synthetic resources are written/removed; no existing row is deleted.
        status, data, _ = request('/api/v1/knowledge-bases', 'POST', {
            'name': 'phaseb-431226c3b-upgrade-acceptance', 'type': 'document',
            'indexing_strategy': {'vector_enabled': False, 'keyword_enabled': False, 'wiki_enabled': True, 'graph_enabled': False}}, token)
        assert status == 201
        kb = data['data']['id']
        result['synthetic_kb_create'] = status
        status, data, _ = request('/api/v1/knowledge-bases/' + kb, token=token)
        assert status == 200 and data.get('data', {}).get('id') == kb
        result['synthetic_kb_read'] = status
        status, _, _ = request('/api/v1/knowledge-bases/' + kb)
        assert status == 401
        result['unauthenticated_kb_denied'] = status
        status, _, _ = request('/api/v1/knowledge-bases/' + kb + '/feishu', token=token)
        assert status == 200
        result['feishu_personal_document_list'] = status
        status, _, _ = request('/api/v1/system/admin/feishu', token=token)
        assert status in (200, 403)
        result['feishu_admin_config'] = {'status': status, 'classification': 'read_ok' if status == 200 else 'non_system_admin_denied'}

        status, data, _ = request('/api/v1/mcp-endpoints', 'POST', {
            'name': 'phaseb-431226c3b-acceptance', 'enabled': True,
            'knowledge_base_ids': [kb], 'tools': ['list_knowledge_bases', 'list_documents', 'wiki_search']}, token)
        assert status == 201
        endpoint, endpoint_token = data['data']['id'], data['data']['token']
        secrets_in_memory.append(endpoint_token)
        mcp_path = '/mcp/' + endpoint
        mcp_headers = {'Accept': 'application/json, text/event-stream'}

        def rpc(ident, method, params):
            status, body, _ = request(mcp_path, 'POST', {'jsonrpc': '2.0', 'id': ident, 'method': method, 'params': params}, endpoint_token, mcp_headers)
            assert status == 200 and body.get('id') == ident and 'error' not in body
            return body.get('result', {})

        init = rpc(1, 'initialize', {'protocolVersion': '2024-11-05', 'capabilities': {}, 'clientInfo': {'name': 'synthetic-upgrade-acceptance', 'version': '1'}})
        assert 'serverInfo' in init and 'capabilities' in init
        result['mcp_initialize'] = 'PASS'
        tools = rpc(2, 'tools/list', {})
        names = {x['name'] for x in tools['tools']}
        assert names == {'list_knowledge_bases', 'list_documents', 'wiki_search'}
        result['mcp_tools_list'] = {'result': 'PASS', 'tool_count': len(names)}
        for ident, name, args in ((3, 'list_knowledge_bases', {}), (4, 'list_documents', {'knowledge_base_id': kb}),
                                  (5, 'wiki_search', {'query': 'SYNTHETIC_PHASEB_ACCEPTANCE_NO_EMPLOYEE_DATA', 'knowledge_base_ids': [kb]})):
            tool = rpc(ident, 'tools/call', {'name': name, 'arguments': args})
            assert not tool.get('isError', False) and isinstance(tool.get('content'), list)
            result['mcp_' + name] = 'PASS'
        status, _, _ = request(mcp_path, 'POST', {'jsonrpc': '2.0', 'id': 6, 'method': 'tools/list'}, 'SyntheticInvalidEndpointToken', mcp_headers)
        assert status == 401
        result['mcp_invalid_token_denied'] = status
        return result
    finally:
        cleanup = {}
        if endpoint and token:
            cleanup['synthetic_mcp_delete'] = request('/api/v1/mcp-endpoints/' + endpoint, 'DELETE', token=token)[0]
        if kb and token:
            cleanup['synthetic_kb_delete'] = request('/api/v1/knowledge-bases/' + kb, 'DELETE', token=token)[0]
        if token:
            cleanup['local_logout'] = request('/api/v1/auth/logout', 'POST', token=token)[0]
            cleanup['local_token_revoked'] = request('/api/v1/auth/me', token=token)[0] == 401
        result['cleanup'] = cleanup
        # Log scan stays in memory; never persist raw lines or credential fingerprints.
        if extended:
            privacy = {}
            for service in ('app', 'frontend'):
                p = subprocess.run(['docker', 'logs', snapshot()[service]['id']], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
                raw = p.stdout
                found = any(value.encode() in raw for value in secrets_in_memory if value)
                patterns = (rb'Authorization:\s*(?:Basic|Bearer)\s+[A-Za-z0-9+/=_-]{8,}',
                            rb'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+',
                            rb'\b(?:OC|AT)-[0-9]+-[A-Za-z0-9_-]+')
                privacy[service] = {'scan_completed': p.returncode == 0, 'sensitive_pattern_or_test_credential_found': found or any(re.search(x, raw) for x in patterns), 'bytes_scanned': len(raw)}
            result['new_container_log_privacy'] = privacy
        token = endpoint_token = None
        secrets_in_memory.clear()


def main():
    os.umask(0o077)
    result = {'target': '172.30.232.42', 'target_sha': SHA, 'result': 'FAILED', 'upgrade_completed': False,
              'rolled_back': False, 'real_sid_login': 'NOT_EXECUTED', 'real_identity_binding': 'NOT_EXECUTED'}
    phase, stopped, migrated, replaced = 'precheck', False, False, False
    backup = None
    lock = (LIVE / 'private/phaseb-upgrade.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    try:
        before = snapshot()
        assert canonical(before.values()) == canonical(EXPECTED['live_containers'])
        assert flags(before['app']['id']) == EXPECTED['live_flags']
        pg = before['postgres']['id']
        starting_schema = '3027|f' if RESUME else '3026|f'
        assert sql(pg, 'SELECT version,dirty FROM schema_migrations') == starting_schema
        if RESUME:
            previous = PRIOR_APPROVED_UPGRADE_RECEIPT
            assert previous['rolled_back'] and previous['failure_stage'] == 'recreate_app_frontend'
            assert previous['migration_003027']['result'] == 'PASS'
            assert previous['recovery'] == 'OLD_APPLICATION_RESTORED_WITH_OAUTH_FORM'
            (CAND / 'deployment-attempt1.json').write_text(json.dumps(previous, indent=2) + '\n')
            result['controlled_resume_without_repeating_migration'] = True
        baseline = counts(pg)
        assert baseline['source_id_sessions'] == baseline['external_identities'] == 0
        result['table_counts_before'] = baseline
        result['containers_before'] = before
        for kind, iid in (('app', APP), ('frontend', FRONTEND)):
            assert execute(['docker', 'image', 'inspect', '--format', '{{.Id}}', iid]).decode().strip() == iid
            assert execute(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', iid]).decode().strip() == SHA
            assert yaml.safe_load(PIN.read_text())['services'][kind]['image'] == iid
            assert yaml.safe_load(ROLLBACK.read_text())['services'][kind]['image'] == before[kind]['image_id']
        assert yaml.safe_load(ROLLBACK.read_text())['services']['app']['environment']['SOURCEID_BASIC_ENCODING'] == 'oauth_form'
        assert flags(before['app']['id'])['AUTO_MIGRATE'] == 'false'
        assert sql(pg, "SELECT to_regclass('source_id_local_sessions') IS NULL") == ('f' if RESUME else 't')
        result['precheck'] = 'PASS'

        backup = LIVE / 'private/phaseb-upgrade-backups' / datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
        backup.mkdir(mode=0o700, parents=True, exist_ok=False)
        doc = yaml.safe_load((LIVE / 'compose.yml').read_text())
        config_paths = [LIVE / 'compose.yml', pathlib.Path(before['app']['compose_files'].split(',')[1]), PIN, ROLLBACK]
        config_paths += [LIVE / value for value in doc['services']['app']['env_file']]
        originals = {}
        for index, path in enumerate(config_paths):
            copy = backup / (str(index) + '-' + path.name)
            shutil.copyfile(str(path), str(copy))
            copy.chmod(0o600)
            originals[str(path)] = path.read_bytes()
        result['backup_directory'] = str(backup)
        result['configuration_backup_count'] = len(config_paths)
        result['maintenance_started_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        phase = 'stop_writes'
        stopped = True
        execute(['docker', 'stop', before['frontend']['id'], before['app']['id']], timeout=120)
        assert not inspect(before['app']['id'], '.State.Running') and not inspect(before['frontend']['id'], '.State.Running')
        phase = 'latest_database_backup'
        dump = backup / ('test-db-3027.dump' if RESUME else 'test-db-3026.dump')
        with dump.open('wb') as output:
            execute(['docker', 'exec', pg, 'sh', '-c', 'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'], timeout=180, output=output)
        dump.chmod(0o600)
        assert dump.stat().st_size > 0
        result['fresh_database_backup'] = {'path': str(dump), 'bytes': dump.stat().st_size, 'mode': '0600', 'writes_stopped': True}
        assert counts(pg) == baseline and sql(pg, 'SELECT version,dirty FROM schema_migrations') == starting_schema
        assert all(pathlib.Path(p).read_bytes() == value for p, value in originals.items())

        # The standard migrate CLI runs inside a credential-controlled service
        # process. No migrate.sh (it prints DB credentials), dumps or shell trace.
        phase = 'migration_003027'
        migration_pin = backup / 'migration-only.yml'
        networks = inspect(before['app']['id'], '.NetworkSettings.Networks')
        assert len(networks) == 1
        network = next(iter(networks))
        # Separate one-shot service: no inherited log options, mounts, ports,
        # dependencies, application entrypoint, health checks or restart loop.
        app_environment = doc['services']['app'].get('environment', {})
        assert 'OIDC_AUTH_CLIENT_SECRET' not in app_environment
        migration_pin.write_text(yaml.safe_dump({
            'services': {'migration': {'image': APP, 'pull_policy': 'never',
                'env_file': [str(LIVE / p) for p in doc['services']['app']['env_file']],
                'environment': app_environment, 'networks': ['existing'],
                'logging': {'driver': 'none'}, 'restart': 'no'}},
            'networks': {'existing': {'external': True, 'name': network}}}))
        migration_pin.chmod(0o600)
        runner = """import os,subprocess,urllib.parse,json,sys
q=urllib.parse.quote
uri='postgres://'+q(os.environ['DB_USER'],safe='')+':'+q(os.environ['DB_PASSWORD'],safe='')+'@'+os.environ['DB_HOST']+':'+os.environ.get('DB_PORT','5432')+'/'+q(os.environ['DB_NAME'],safe='')+'?sslmode=disable'
p=subprocess.run(['migrate','-path','/app/migrations/versioned','-database',uri,'up','1'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
print(json.dumps({'migrate_exit_code':p.returncode}))
sys.exit(p.returncode)
"""
        args = ['docker', 'compose', '--project-name', PROJECT + '-phaseb-migration', '--project-directory', str(LIVE),
                '-f', str(migration_pin), 'run', '--rm', '--no-deps', '-T', '--entrypoint', 'python3', 'migration', '-c', runner]
        if not RESUME:
            output = execute(args, timeout=180)
            migration_result = json.loads(output)
            assert migration_result == {'migrate_exit_code': 0}
        assert sql(pg, 'SELECT version,dirty FROM schema_migrations') == '3027|f'
        migrated = True
        assert counts(pg) == baseline
        assert sql(pg, "SELECT COUNT(*) FROM source_id_local_sessions") == '0'
        result['migration_003027'] = {'result': 'PASS', 'version': 3027, 'dirty': False, 'table_counts_unchanged': True,
                                      'executed_in_this_attempt': not RESUME, 'total_up_execution_count': 1}

        phase = 'recreate_app_frontend'
        replaced = True
        execute(compose(PIN) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'app', 'frontend'], timeout=180)
        healthy(APP)
        after = snapshot()
        result['containers_after'] = after
        assert after['app']['image_id'] == APP and after['frontend']['image_id'] == FRONTEND
        assert after['app']['compose_files'].split(',') == [str(LIVE / 'compose.yml'), str(PIN)]
        assert flags(after['app']['id']) == EXPECTED['live_flags']
        assert {k: v for k, v in before.items() if k not in ('app', 'frontend')} == {k: v for k, v in after.items() if k not in ('app', 'frontend')}
        for kind in ('app', 'frontend'):
            assert before[kind]['ports'] == after[kind]['ports']
            assert sorted(before[kind]['mounts'], key=lambda x: x['Destination']) == sorted(after[kind]['mounts'], key=lambda x: x['Destination'])
        assert all(pathlib.Path(p).read_bytes() == value for p, value in originals.items())
        result['non_app_frontend_containers_unchanged'] = True
        result['volumes_ports_configuration_sources_unchanged'] = True
        result['effective_flags'] = flags(after['app']['id'])
        result['containers_after'] = after
        phase = 'business_acceptance'
        acceptance = business_acceptance('431226c3b')
        result['acceptance'] = acceptance
        assert acceptance['cleanup']['local_logout'] == 200 and acceptance['cleanup']['local_token_revoked']
        assert all(acceptance['cleanup'][x] in (200, 204) for x in ('synthetic_kb_delete', 'synthetic_mcp_delete'))
        assert all(v['scan_completed'] and not v['sensitive_pattern_or_test_credential_found'] for v in acceptance['new_container_log_privacy'].values())
        assert sql(pg, 'SELECT version,dirty FROM schema_migrations') == '3027|f'
        assert sql(pg, 'SELECT COUNT(*) FROM source_id_local_sessions') == '0'
        for table in ('users', 'tenants', 'tenant_members', 'external_identities', 'source_id_sessions', 'source_id_binding_reviews', 'feishu_connections', 'feishu_personal_imports'):
            assert int(sql(pg, 'SELECT COUNT(*) FROM ' + table)) == baseline[table]
        result['identity_membership_feishu_rows_unchanged'] = True
        result['local_enterprise_session_count'] = 0
        result['schema_after'] = '3027|f'
        result['upgrade_completed'] = True
        result['result'] = 'PASS'
    except Exception as error:
        result['failure_stage'] = phase
        result['failure_category'] = type(error).__name__
        import traceback
        result['failure_lines'] = [f.lineno for f in traceback.extract_tb(error.__traceback__) if f.filename == '<stdin>']
        if stopped:
            try:
                state = sql(pg, 'SELECT version,dirty FROM schema_migrations')
                result['schema_on_failure'] = state
                if phase == 'migration_003027':
                    # Do not guess dirty/partial transaction recovery or proceed.
                    result['recovery'] = 'STOPPED_FOR_MIGRATION_STATE_REVIEW'
                elif state in ('3026|f', '3027|f'):
                    if state == '3027|f':
                        sql(pg, 'BEGIN; UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_local_sessions); UPDATE source_id_sessions SET revoked=TRUE WHERE id IN (SELECT id FROM source_id_local_sessions); COMMIT;')
                        result['local_enterprise_sessions_revoked_before_rollback'] = True
                    if replaced:
                        execute(compose(ROLLBACK) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'app', 'frontend'], timeout=180)
                    else:
                        execute(['docker', 'start', before['app']['id'], before['frontend']['id']])
                    healthy(before['app']['image_id'])
                    result['rollback_acceptance'] = business_acceptance('2fcbcaaed', extended=False)
                    assert flags(snapshot()['app']['id']) == EXPECTED['live_flags']
                    result['rolled_back'] = True
                    result['recovery'] = 'OLD_APPLICATION_RESTORED_WITH_OAUTH_FORM'
                else:
                    result['recovery'] = 'STOPPED_FOR_MIGRATION_STATE_REVIEW'
            except Exception as rollback_error:
                result['rollback_failure_category'] = type(rollback_error).__name__
    finally:
        result['completed_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        destination = backup if backup else CAND
        (destination / 'deployment-result.json').write_text(json.dumps(result, indent=2) + '\n')
        (CAND / 'deployment-result.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result))
        fcntl.flock(lock, fcntl.LOCK_UN)
        lock.close()
    return 0 if result['result'] == 'PASS' else 2


if __name__ == '__main__':
    raise SystemExit(main())
