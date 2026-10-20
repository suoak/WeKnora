"""Final counts/credential equality only; never export credentials or claims."""
import hmac
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');app='knowhub-company-test-app-1';pg='knowhub-company-test-postgres-1'
env=environment(app);old=environment('knowhub-sourceid-business-app-1')
oldsecret=old.get('OIDC_AUTH_CLIENT_SECRET','');newsecret=env.get('OIDC_AUTH_CLIENT_SECRET','')
out={'result':'PASS','new_sid_requests':0,'real_identity_writes':0,'business_gates_changed':False,'credentials':{'client_id_matches_default':env.get('OIDC_AUTH_CLIENT_ID')==old.get('OIDC_AUTH_CLIENT_ID'),'secret_matches_default_boolean_only':bool(oldsecret and newsecret) and hmac.compare_digest(newsecret,oldsecret)},'actual_local_password_login':'NOT_EXECUTED_CLEAN_TARGET_PASSWORDS_DISABLED','real_sid_login':'NOT_EXECUTED','rollback_executed':False}
q="BEGIN READ ONLY;SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'identities',(SELECT COUNT(*) FROM external_identities),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'enterprise_sessions',(SELECT COUNT(*) FROM source_id_sessions),'active_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked),'active_keys',(SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL),'active_transactions',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()));ROLLBACK;"
z=subprocess.run(['docker','exec','-i',pg,'psql','-X','-U','clean_owner','-d','knowhub_oa_sso_pilot','-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert z.returncode==0
out['database']=[json.loads(x) for x in z.stdout.decode().splitlines() if x]
assert out['database'][0]=={'version':3029,'dirty':False} and all(v==0 for k,v in out['database'][1].items() if k!='users')
assert inspect(app,'.State.Health.Status')=='healthy'
out['public_entry_automatic_restart']=inspect('knowhub-company-entry-entry-1','.HostConfig.RestartPolicy')['Name'];assert out['public_entry_automatic_restart']=='no'
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'readiness-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
