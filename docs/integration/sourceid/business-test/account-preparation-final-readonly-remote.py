"""Packaging-only aggregates for readiness; no PII, auth, writes or config changes."""
project = 'knowhub-sourceid-business'
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=' + project, '--format', '{{.ID}}']).decode().split()
containers = {x['service']: x for x in (metadata(cid) for cid in ids)}
assert containers['app']['working_dir'] == '/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05'
sha = '431226c3b1bd5338ec06c1bd0520e637d5fed398'
images = {}
for service in ('app', 'frontend'):
    container = containers[service]
    revision = run(['docker', 'image', 'inspect', '--format', '{{index .Config.Labels "org.opencontainers.image.revision"}}', container['image_id']]).decode().strip()
    assert revision == (sha if service == 'app' else '1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b') and container['running']
    images[service] = {'container_id': container['id'], 'image_id': container['image_id'], 'revision': revision,
                       'health': container['health'], 'ports': container['ports']}
flags = {}
for key in ('SOURCEID_BASIC_ENCODING', 'SOURCEID_PROTOCOL_ONLY', 'SOURCEID_CONTRACT_CONFIRMED',
            'SOURCEID_EMAIL_BINDING_ENABLE', 'SOURCEID_EMAIL_CONTRACT_CONFIRMED', 'SOURCEID_AUTO_BINDING_ENABLE',
            'SOURCEID_LOGOUT_ENABLE', 'SOURCEID_SESSION_MODE', 'OIDC_AUTH_ENABLE', 'SOURCEID_DIAGNOSTICS_ENABLE',
            'OIDC_AUTH_PKCE_ENABLE', 'AUTO_MIGRATE'):
    value = run(['docker', 'exec', containers['app']['id'], 'sh', '-c', 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
    assert value in ('true', 'false', 'oauth_form', 'external', 'NOT_SET')
    flags[key] = value
assert flags['SOURCEID_PROTOCOL_ONLY'] == 'true'
assert flags['SOURCEID_CONTRACT_CONFIRMED'] == flags['SOURCEID_AUTO_BINDING_ENABLE'] == flags['SOURCEID_EMAIL_BINDING_ENABLE'] == 'false'
assert flags['SOURCEID_SESSION_MODE'] in ('external', 'NOT_SET')
sql = """BEGIN READ ONLY;
SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;
SELECT json_build_object('users',COUNT(*),'active_users',COUNT(*) FILTER (WHERE is_active AND deleted_at IS NULL),
 'system_admins',COUNT(*) FILTER (WHERE is_system_admin),'cross_tenant_admins',COUNT(*) FILTER (WHERE can_access_all_tenants),
 'reserved_domain_users',COUNT(*) FILTER (WHERE email ~* '@[^@]+[.](invalid|test|example|localhost)$')) FROM users;
SELECT json_build_object('memberships',COUNT(*),'active_memberships',COUNT(*) FILTER (WHERE status='active' AND deleted_at IS NULL),
 'owner_memberships',COUNT(*) FILTER (WHERE role='owner'),'admin_memberships',COUNT(*) FILTER (WHERE role='admin'),
 'contributor_memberships',COUNT(*) FILTER (WHERE role='contributor'),'viewer_memberships',COUNT(*) FILTER (WHERE role='viewer')) FROM tenant_members;
SELECT json_build_object('ordinary_auto_binding_candidates',COUNT(*)) FROM users u
 WHERE u.deleted_at IS NULL AND u.is_active AND NOT u.is_system_admin AND NOT u.can_access_all_tenants AND u.tenant_id<>0
 AND (SELECT COUNT(*) FROM tenant_members m WHERE m.user_id=u.id AND m.tenant_id=u.tenant_id AND m.status='active' AND m.role IN ('contributor','viewer'))=1
 AND NOT EXISTS (SELECT 1 FROM tenant_members m WHERE m.user_id=u.id AND (m.role NOT IN ('contributor','viewer') OR m.status<>'active'))
 AND NOT EXISTS (SELECT 1 FROM organization_tenant_members o WHERE o.role='admin' AND
 (o.representative_user_id=u.id OR o.tenant_id=u.tenant_id OR o.tenant_id IN (SELECT tenant_id FROM tenant_members WHERE user_id=u.id)));
SELECT json_build_object('duplicate_normalized_email_groups',COUNT(*)) FROM
 (SELECT LOWER(TRIM(email)) FROM users WHERE deleted_at IS NULL GROUP BY LOWER(TRIM(email)) HAVING COUNT(*)>1) d;
SELECT json_build_object('knowledge_bases_total',COUNT(*),'active_knowledge_bases',COUNT(*) FILTER (WHERE deleted_at IS NULL)) FROM knowledge_bases;
SELECT json_build_object('tenants',(SELECT COUNT(*) FROM tenants),'external_identities',(SELECT COUNT(*) FROM external_identities),
 'binding_reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sourceid_sessions',(SELECT COUNT(*) FROM source_id_sessions),
 'local_enterprise_sessions',(SELECT COUNT(*) FROM source_id_local_sessions));
ROLLBACK;"""
p = subprocess.run(['docker', 'exec', '-i', containers['postgres']['id'], 'sh', '-c',
                    'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -qAt'],
                   input=sql.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
assert p.returncode == 0
aggregates = [json.loads(line) for line in p.stdout.decode().splitlines() if line.strip()]
assert aggregates[0] == {'version': 3027, 'dirty': False}

assert not run(['docker','ps','-aq','--filter','label=knowhub.test=oa-isolation-lab']).strip()
assert not run(['docker','network','ls','-q','--filter','label=knowhub.test=oa-isolation-lab']).strip()
op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
entrypoints=[]
for port in (80,8080,18080,18081,18082,18182,8082):
    row={'port':port,'config_status':0,'oidc_enabled':'UNKNOWN'}
    try:
        with op.open('http://127.0.0.1:'+str(port)+'/api/v1/auth/oidc/config',timeout=4) as response:
            row['config_status']=response.status
            raw=response.read(8193)
            if len(raw)<=8192:
                try:
                    value=json.loads(raw)
                    if type(value.get('enabled')) is bool:row['oidc_enabled']=value['enabled']
                except (ValueError,AttributeError):pass
    except urllib.error.HTTPError as e: row['config_status']=e.code
    except (OSError,TimeoutError):pass
    entrypoints.append(row)
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','images':images,'flags':flags,'database_aggregates':aggregates,'public_config_entrypoints':entrypoints,'lab_containers_and_networks_removed':True,'read_only_database_transaction':True,'configuration_changed':False,'real_sid_login_started':False,'result':'PASS'}))
