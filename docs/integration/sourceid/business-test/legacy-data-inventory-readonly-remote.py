"""KnowHub projects only: read-only catalog/aggregate inventory. No row values."""
import time

started = datetime.datetime.now(datetime.timezone.utc).isoformat()
allowed_projects = {'weknora', 'weknora-segment-test', 'knowhub-sourceid-business', 'knowhub-sourceid-p6', 'knowhub24-rc1-smoke', 'smoke-v080-knowhub8-32e97760'}
ids = run(['docker', 'ps', '-a', '--format', '{{.ID}}']).decode().split()
projects = {}
postgres = []
for cid in ids:
    labels = inspect(cid, '.Config.Labels') or {}
    project = labels.get('com.docker.compose.project')
    service = labels.get('com.docker.compose.service')
    if project not in allowed_projects:
        continue
    state = inspect(cid, '.State')
    image = inspect(cid, '.Image')
    image_labels = json.loads(run(['docker', 'image', 'inspect', '--format', '{{json .Config.Labels}}', image])) or {}
    revision = image_labels.get('org.opencontainers.image.revision', 'NOT_CAPTURED')
    if revision != 'NOT_CAPTURED' and not re.fullmatch(r'[a-f0-9]{7,40}', revision):
        revision = 'NOT_CAPTURED'
    entry = {'container_id': inspect(cid, '.Id'), 'service': service, 'image_id': image,
             'revision': revision, 'running': state['Running'], 'health': state.get('Health', {}).get('Status', 'NOT_CONFIGURED'),
             'compose_files': labels.get('com.docker.compose.project.config_files'),
             'mounts': [{k: m.get(k) for k in ('Type', 'Name', 'Source', 'Destination', 'RW')} for m in inspect(cid, '.Mounts')],
             'published_ports': inspect(cid, '.NetworkSettings.Ports')}
    projects.setdefault(project, []).append(entry)
    if service == 'postgres':
        postgres.append((project, cid, state['Running']))

def query(cid, database, sql):
    # Database identifier comes from the catalog and is an argv item, not shell code.
    args = ['docker', 'exec', '-i', cid, 'sh', '-c',
            'exec psql -X -U "$POSTGRES_USER" -d "$1" -v ON_ERROR_STOP=1 -qAt', 'sh', database]
    command = "BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='8s';\n"+sql+"\nROLLBACK;"
    q = subprocess.run(args, input=command.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
    if q.returncode:
        return None
    try:
        return [json.loads(x) for x in q.stdout.decode().splitlines() if x.strip()]
    except ValueError:
        return None

databases = []
known_tables = ('schema_migrations', 'users', 'tenants', 'tenant_members', 'organization_tenant_members',
                'knowledge_bases', 'knowledges', 'chunks', 'embeddings', 'models', 'kb_shares', 'agent_shares',
                'external_identities', 'source_id_binding_reviews', 'source_id_sessions', 'source_id_local_sessions')
table_list = ','.join("'"+x+"'" for x in known_tables)
for project, cid, running in postgres:
    if not running:
        databases.append({'project': project, 'status': 'NOT_ACCESSIBLE_STOPPED_CONTAINER'})
        continue
    catalog = query(cid, 'postgres', "SELECT json_build_object('database',datname) FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY datname;")
    if catalog is None:
        databases.append({'project': project, 'status': 'NOT_ACCESSIBLE_DATABASE_CATALOG'})
        continue
    slot = 0
    for record in catalog:
        database = record['database']
        columns = query(cid, database, "SELECT json_build_object('table',table_name,'column',column_name) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ("+table_list+");")
        if columns is None:
            databases.append({'project': project, 'status': 'NOT_ACCESSIBLE_SCHEMA'})
            continue
        cols = {}
        for x in columns:
            cols.setdefault(x['table'], set()).add(x['column'])
        if 'knowledge_bases' not in cols and 'users' not in cols:
            continue
        slot += 1
        row = {'project': project, 'database_slot': slot, 'present_tables': sorted(cols), 'status': 'READ_ONLY_AGGREGATES'}
        queries = []
        def active(table, alias=''):
            return (alias+'.' if alias else '')+'deleted_at IS NULL' if 'deleted_at' in cols.get(table, set()) else 'TRUE'
        def add(name, sql):
            queries.append("SELECT json_build_object('section','"+name+"','data',("+sql+"));")
        if 'schema_migrations' in cols:
            add('migration', "SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations LIMIT 1")
        if 'users' in cols:
            flags = ["'total',COUNT(*)", "'active_rows',COUNT(*) FILTER(WHERE "+active('users')+")"]
            for name in ('is_active','is_system_admin','can_access_all_tenants'):
                if name in cols['users']:
                    flags.append("'"+name+"',COUNT(*) FILTER(WHERE "+active('users')+" AND "+name+")")
            add('users', "SELECT json_build_object("+','.join(flags)+") FROM users")
            if 'email' in cols['users']:
                add('email_uniqueness', "SELECT json_build_object('duplicate_normalized_groups',COUNT(*)) FROM (SELECT LOWER(TRIM(email)) FROM users WHERE "+active('users')+" GROUP BY LOWER(TRIM(email)) HAVING COUNT(*)>1) e")
        if 'tenants' in cols:
            add('tenants', "SELECT json_build_object('total',COUNT(*),'active_rows',COUNT(*) FILTER(WHERE "+active('tenants')+")) FROM tenants")
        if 'tenant_members' in cols:
            fields = ["'active_rows',COUNT(*) FILTER(WHERE "+active('tenant_members')+" AND status='active')"]
            fields += ["'"+r+"',COUNT(*) FILTER(WHERE "+active('tenant_members')+" AND status='active' AND role='"+r+"')" for r in ('owner','admin','contributor','viewer')]
            add('memberships', "SELECT json_build_object("+','.join(fields)+") FROM tenant_members")
            if all(t in cols for t in ('users','organization_tenant_members')) and all(x in cols['users'] for x in ('is_active','is_system_admin','can_access_all_tenants')):
                add('ordinary_candidates', """SELECT json_build_object('count',COUNT(*)) FROM users u WHERE """+active('users','u')+""" AND u.is_active AND NOT u.is_system_admin AND NOT u.can_access_all_tenants AND u.tenant_id<>0
AND (SELECT COUNT(*) FROM tenant_members m WHERE m.user_id=u.id AND m.tenant_id=u.tenant_id AND m.status='active' AND m.role IN ('contributor','viewer'))=1
AND NOT EXISTS (SELECT 1 FROM tenant_members m WHERE m.user_id=u.id AND (m.role NOT IN ('contributor','viewer') OR m.status<>'active'))
AND NOT EXISTS (SELECT 1 FROM organization_tenant_members o WHERE o.role='admin' AND (o.representative_user_id=u.id OR o.tenant_id=u.tenant_id OR o.tenant_id IN (SELECT tenant_id FROM tenant_members WHERE user_id=u.id)))""")
        if 'knowledge_bases' in cols:
            add('knowledge_bases', "SELECT json_build_object('total',COUNT(*),'active',COUNT(*) FILTER(WHERE "+active('knowledge_bases')+"),'active_spaces',COUNT(DISTINCT tenant_id) FILTER(WHERE "+active('knowledge_bases')+")) FROM knowledge_bases")
        if 'knowledges' in cols:
            fields = ["'total',COUNT(*)", "'active',COUNT(*) FILTER(WHERE "+active('knowledges')+")"]
            if 'parse_status' in cols['knowledges']:
                fields += ["'"+r+"',COUNT(*) FILTER(WHERE "+active('knowledges')+" AND parse_status='"+r+"')" for r in ('completed','failed','pending','processing','finalizing')]
            for col in ('file_path','embedding_model_id'):
                if col in cols['knowledges']:
                    fields.append("'"+col+"_present',COUNT(*) FILTER(WHERE "+active('knowledges')+" AND COALESCE("+col+",'')<>'')")
            add('documents', "SELECT json_build_object("+','.join(fields)+") FROM knowledges")
        if 'chunks' in cols:
            fields = ["'total',COUNT(*)", "'active',COUNT(*) FILTER(WHERE "+active('chunks')+")"]
            if 'content' in cols['chunks']:
                fields.append("'nonempty_active',COUNT(*) FILTER(WHERE "+active('chunks')+" AND LENGTH(TRIM(COALESCE(content,'')))>0)")
            if 'status' in cols['chunks']:
                fields.append("'indexed_active',COUNT(*) FILTER(WHERE "+active('chunks')+" AND status=2)")
            add('chunks', "SELECT json_build_object("+','.join(fields)+") FROM chunks")
        for table in ('embeddings','models','kb_shares','agent_shares','external_identities','source_id_binding_reviews','source_id_sessions','source_id_local_sessions'):
            if table in cols:
                add(table, "SELECT json_build_object('total',COUNT(*),'active_rows',COUNT(*) FILTER(WHERE "+active(table)+")) FROM "+table)
        if all(t in cols for t in ('knowledge_bases','knowledges','chunks')):
            add('reusable_content', "SELECT json_build_object('active_kbs_with_completed_nonempty_chunks',COUNT(DISTINCT b.id),'spaces_with_content',COUNT(DISTINCT b.tenant_id)) FROM knowledge_bases b JOIN knowledges k ON k.knowledge_base_id=b.id JOIN chunks c ON c.knowledge_id=k.id WHERE "+active('knowledge_bases','b')+" AND "+active('knowledges','k')+" AND "+active('chunks','c')+" AND k.parse_status='completed' AND LENGTH(TRIM(COALESCE(c.content,'')))>0")
        add('extensions', "SELECT COALESCE(json_agg(json_build_object('name',extname,'version',extversion)), '[]'::json) FROM pg_extension WHERE extname IN ('vector','pg_search')")
        result = query(cid, database, '\n'.join(queries))
        if result is None:
            row['status'] = 'NOT_ACCESSIBLE_AGGREGATE_QUERY'
        else:
            row['aggregates'] = {x['section']:x['data'] for x in result}
        databases.append(row)

backup_root = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/20261014T080114Z')
backups = {}
for name in ('old-database.dump','old-data-files.tar.gz','old-config','rollback.sh'):
    f = backup_root/name
    backups[name] = {'exists': f.exists()}
    if f.exists():
        st = f.stat()
        backups[name].update({'bytes': st.st_size, 'mode': oct(st.st_mode & 0o777), 'mtime_utc': datetime.datetime.fromtimestamp(st.st_mtime,datetime.timezone.utc).isoformat()})
print(json.dumps({'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  'target':'172.30.232.42','projects':projects,'databases':databases,'old_upgrade_backup_metadata':backups,
                  'all_data_queries_read_only':True,'personal_row_values_exported':False,'credentials_read':False,
                  'documents_or_vectors_exported':False,'database_copied_or_migrated':False,'containers_or_configuration_changed':False}))
