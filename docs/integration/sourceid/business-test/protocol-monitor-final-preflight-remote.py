"""Final read-only inventory. No authorize request or runtime change."""
app='knowhub-sourceid-business-app-1'
front='knowhub-sourceid-business-frontend-1'
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
receipt=json.loads((root/'app-deployment-result.json').read_text())
out={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','containers':{},'flags':{},'runtime_changed':False,'login_started':False}
for name in (app,front):
 m=metadata(name)
 revision=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',m['image_id']]).decode().strip()
 out['containers'][m['service']]={'id':m['id'],'image':m['image_id'],'revision':revision,'running':m['running'],'health':m['health']}
 assert m['running'] and m['project']=='knowhub-sourceid-business'
 if name==app:
  assert revision=='479416c9086d91de4484dbbd2cc5f912cdcdb697' and m['health']=='healthy'
  assert m['compose_files'].split(',')==receipt['compose_files_in_order']
 else:assert revision=='1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b'
required={'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED':'false','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_SESSION_MODE':'NOT_SET','SOURCEID_LOGOUT_ENABLE':'false','OIDC_AUTH_ENABLE':'true','SOURCEID_DIAGNOSTICS_ENABLE':'true','AUTO_MIGRATE':'false'}
for k,expected in required.items():
 v=run(['docker','exec',app,'sh','-c','printf "%s" "${'+k+'-NOT_SET}"']).decode()
 assert v==expected
 out['flags'][k]=v
q="BEGIN READ ONLY;SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;SELECT json_build_object('active_unconsumed', (SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()),'users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions));ROLLBACK;"
p=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
assert p.returncode==0
rows=[json.loads(x) for x in p.stdout.decode().splitlines() if x]
assert rows[0]=={'version':3027,'dirty':False} and rows[1]['active_unconsumed']==0 and rows[1]['bindings']==0 and rows[1]['local_sessions']==0
out['database']=rows
out['compose_files_in_order']=receipt['compose_files_in_order']
out['restore_files_present']=all(pathlib.Path(f).is_file() for f in receipt['compose_files_in_order'])
out['app_rollback_pin_present']=(root/'prompt-login-app-rollback-pin.yml').is_file()
out['backup_present']=(pathlib.Path(receipt['backup_directory'])/'current-db-3027.dump').is_file()
assert out['restore_files_present'] and out['app_rollback_pin_present'] and out['backup_present']
o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
out['health']={}
for label,url in [('frontend','http://127.0.0.1/'),('app','http://127.0.0.1:8080/health'),('proxy_config','http://127.0.0.1/api/v1/auth/oidc/config')]:
 with o.open(url,timeout=5) as r:out['health'][label]=r.status;assert r.status==200
out['result']='READONLY_PASS'
print(json.dumps(out))
