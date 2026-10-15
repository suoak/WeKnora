"""Read-only recovery inventory; no upgrade or migration action."""
base = pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
receipt = json.loads((base / 'deployment-result.json').read_text())
project = 'knowhub-sourceid-business'
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=' + project, '--format', '{{.ID}}']).decode().split()
current = {x['service']: x for x in (metadata(cid) for cid in ids)}
comparison = {}
for kind in ('app', 'frontend'):
    before = receipt['containers_before'][kind]
    after = current[kind]
    comparison[kind] = {'mount_order_equal': before['mounts'] == after['mounts'],
                        'mounts_equal_by_destination': sorted(before['mounts'], key=lambda x: x['Destination']) == sorted(after['mounts'], key=lambda x: x['Destination']),
                        'ports_equal': before['ports'] == after['ports']}
pg = current['postgres']['id']
schema = run(['docker', 'exec', pg, 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
other_unchanged = {k: v for k, v in receipt['containers_before'].items() if k not in ('app', 'frontend')} == {k: v for k, v in current.items() if k not in ('app', 'frontend')}
print(json.dumps({'schema': schema, 'containers': current, 'mount_comparison': comparison, 'dependent_containers_unchanged': other_unchanged}))
