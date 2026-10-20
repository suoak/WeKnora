"""Restore the actual pre-upgrade backup into a disposable isolated DB.

Only aggregate counts leave the process. Never mounts existing DB volumes.
"""
import time
base = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
receipt = json.loads((base / 'deployment-attempt1.json').read_text())
dump = pathlib.Path(receipt['fresh_database_backup']['path'])
image = receipt['containers_before']['postgres']['image_id']
name = 'sourceid-phaseb-fresh-backup-audit-431226c3b'
network = name + '-net'
created = connected = False
result = {'result': 'FAILED', 'real_sid_login': 'NOT_EXECUTED'}
def query(sql):
    p = subprocess.run(['docker', 'exec', '-i', name, 'psql', '-U', 'synthetic_backup_audit', '-d', 'synthetic_backup_audit', '-At', '-v', 'ON_ERROR_STOP=1'],
                       input=sql.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    assert p.returncode == 0
    return p.stdout.decode().strip()
try:
    run(['docker', 'network', 'create', '--internal', network]); connected = True
    run(['docker', 'run', '-d', '--name', name, '--network', network, '--memory', '3g', '--cpus', '2', '--log-driver', 'none',
         '--tmpfs', '/var/lib/postgresql/data:rw,size=2147483648', '-e', 'POSTGRES_HOST_AUTH_METHOD=trust',
         '-e', 'POSTGRES_USER=synthetic_backup_audit', '-e', 'POSTGRES_DB=synthetic_backup_audit', image]); created = True
    for _ in range(45):
        p = subprocess.run(['docker', 'exec', name, 'pg_isready', '-h', '127.0.0.1', '-U', 'synthetic_backup_audit'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if p.returncode == 0: break
        time.sleep(1)
    else: raise RuntimeError('backup_audit_pg_timeout')
    with dump.open('rb') as f:
        p = subprocess.run(['docker', 'exec', '-i', name, 'pg_restore', '-U', 'synthetic_backup_audit', '-d', 'synthetic_backup_audit',
                            '--clean', '--if-exists', '--no-owner', '--no-acl', '--exit-on-error'], stdin=f,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
        assert p.returncode == 0
    assert query('SELECT version,dirty FROM schema_migrations') == '3026|f'
    counts = {table: int(query('SELECT COUNT(*) FROM ' + table)) for table in receipt['table_counts_before']}
    assert counts == receipt['table_counts_before']
    result.update({'result': 'PASS', 'backup_restored_schema': '3026|f', 'row_counts': counts,
                   'active_knowledge_bases': int(query('SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL')),
                   'preupgrade_backup_path': str(dump), 'actual_fresh_backup_restored': True})
finally:
    if created: run(['docker', 'rm', '-f', name])
    if connected: run(['docker', 'network', 'rm', network])
    result['disposable_resources_removed'] = True
    (base / 'fresh-backup-audit-result.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result))
