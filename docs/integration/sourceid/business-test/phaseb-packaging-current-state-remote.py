"""Read-only default container health and migration state, no credentials."""
ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()
containers = [metadata(cid) for cid in ids]
pg = next(x for x in containers if x['service'] == 'postgres')
schema = run(['docker', 'exec', pg['id'], 'sh', '-c', 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
print(json.dumps({'containers': containers, 'schema': schema, 'utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}))
