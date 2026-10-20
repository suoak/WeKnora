"""Read aggregate synthetic failure invariants before disposable DB cleanup."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');pg='knowhub-oa-sso-pilot-postgres-1'
q="""BEGIN READ ONLY;
SELECT json_build_object(
 'jit_registrations',(SELECT COUNT(*) FROM source_id_jit_registrations),
 'unique_jit_users',(SELECT COUNT(*)=COUNT(DISTINCT user_id) FROM source_id_jit_registrations),
 'jit_memberships',(SELECT COUNT(*) FROM tenant_members m JOIN source_id_jit_registrations j ON j.user_id=m.user_id),
 'jit_admin_flags',(SELECT COUNT(*) FROM users u JOIN source_id_jit_registrations j ON j.user_id=u.id WHERE u.is_system_admin OR u.can_access_all_tenants OR u.tenant_id IS NOT NULL),
 'new_refresh_rows',(SELECT COUNT(*) FROM auth_tokens a JOIN users u ON a.user_id=u.id WHERE a.token_type='refresh_token' AND u.email LIKE 'capacity-%@example.test'),'historical_refresh_rows',(SELECT COUNT(*) FROM auth_tokens a JOIN users u ON a.user_id=u.id WHERE a.token_type='refresh_token' AND u.email NOT LIKE 'capacity-%@example.test'),'historical_active_refresh_rows',(SELECT COUNT(*) FROM auth_tokens a JOIN users u ON a.user_id=u.id WHERE a.token_type='refresh_token' AND NOT a.is_revoked AND u.email NOT LIKE 'capacity-%@example.test'),
 'rejected_identity_rows',(SELECT COUNT(*) FROM external_identities WHERE employee_no ~ '^capacity-(limited500|failure500)-existing-[0-9]+$' AND substring(employee_no from '-([0-9]+)$')::int % 10=0),
 'rejected_user_sessions',(SELECT COUNT(*) FROM source_id_sessions s JOIN users u ON u.id=s.user_id WHERE u.email ~ '^capacity-(limited500|failure500)-existing-[0-9]+@example.test$' AND substring(u.email from '-([0-9]+)@')::int % 10=0),
 'synthetic_users',(SELECT COUNT(*) FROM users WHERE email LIKE 'capacity-%@example.test'),
 'identities',(SELECT COUNT(*) FROM external_identities));
ROLLBACK;"""
z=subprocess.run(['docker','exec','-i',pg,'psql','-X','-U','clean_owner','-d','enterprise_capacity_validation','-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
assert z.returncode==0
data=json.loads(z.stdout);assert data['unique_jit_users'] and not any(data[k] for k in ('jit_memberships','jit_admin_flags','new_refresh_rows','historical_active_refresh_rows','rejected_identity_rows','rejected_user_sessions'))
out={'result':'PASS','invariants':data,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'real_sid_called':False}
(b/'capacity-fault-evidence.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
