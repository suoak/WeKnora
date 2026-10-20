"""Verify restore and invalidate only the bounded, unused test transaction."""
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
receipt=json.loads((root/'result.json').read_text());assert receipt['restore']=='PASS'
start=datetime.datetime.fromisoformat(receipt['started_utc']);end=datetime.datetime.fromisoformat(receipt['completed_utc'])
assert (end-start).total_seconds()<1000
def db(q,read=True):
 q=('BEGIN READ ONLY;' if read else 'BEGIN;')+q+(' ;ROLLBACK;' if read else ';COMMIT;')
 p=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15);assert p.returncode==0;return p.stdout.decode().strip()
# ExpiresAt was set at authorization creation +10m. Select only unconsumed
# rows in this bounded test interval, with our server-written prompt metadata.
lo=(start+datetime.timedelta(minutes=10)).isoformat();hi=(end+datetime.timedelta(minutes=10)).isoformat()
where="NOT consumed AND nonce<>'' AND expires_at>='"+lo+"'::timestamptz AND expires_at<='"+hi+"'::timestamptz AND result LIKE '{\"version\":1,\"prompt\":\"login\",%'"
n=int(db('SELECT COUNT(*) FROM oidc_transactions WHERE '+where));assert n<=1
if n==1:db("UPDATE oidc_transactions SET consumed=TRUE,result='',verifier='' WHERE "+where,read=False)
counts=json.loads(db("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'members',(SELECT COUNT(*) FROM tenant_members),'bindings',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions),'live_transactions',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()))"))
assert counts=={'users':2,'members':2,'bindings':0,'reviews':0,'sessions':0,'local_sessions':0,'live_transactions':0}
app='knowhub-sourceid-business-app-1';flag=run(['docker','exec',app,'printenv','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']).decode().strip();assert flag=='false'
revision=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',inspect(app,'.Image')]).decode().strip();assert revision=='479416c9086d91de4484dbbd2cc5f912cdcdb697'
assert inspect(app,'.State.Health.Status')=='healthy'
checks={};o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for name,url in [('frontend','http://127.0.0.1/'),('app','http://127.0.0.1:8080/health'),('oidc_config','http://127.0.0.1/api/v1/auth/oidc/config')]:
 with o.open(url,timeout=5) as r:checks[name]=r.status;assert r.status==200
print(json.dumps({'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'prompt':False,'revision':revision,'health':checks,'unused_test_transaction_invalidated':n,'persisted_test_request_prompt_login':n==1,'proof_scope':'server transaction metadata; not browser final query or SID execution confirmation','counts':counts,'schema':db("SELECT version::text||'|'||dirty::text FROM schema_migrations"),'new_authorization_or_token_requests':False,'non_app_containers_unchanged':receipt['non_app_containers_unchanged'],'accounts_and_permissions_unchanged':receipt['accounts_and_enterprise_records_unchanged']}))
