"""Read-only aggregate timing of the single server-owned prompt transaction."""
q="BEGIN READ ONLY;SELECT json_build_object('count',COUNT(*),'started_utc',MIN((result::jsonb->>'started')::timestamptz),'expires_utc',MIN(expires_at)) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW() AND result LIKE '{\"version\":1,\"prompt\":\"login\",%';ROLLBACK;"
p=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
assert p.returncode==0
out=json.loads(p.stdout)
assert out['count'] in (0,1)
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
print(json.dumps(out))
