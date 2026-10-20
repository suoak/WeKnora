"""Reset synthetic-only auth assertions after harness failure, not real identities."""
import importlib.util
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
assert m.sql("SELECT COUNT(*) FROM external_identities WHERE issuer<>'https://127.0.0.1:18443' OR employee_no NOT IN ('SYNTHETIC-ADMIN','SYNTHETIC-VIEWER')")=='0'
m.sql("BEGIN; UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_sessions WHERE issuer='https://127.0.0.1:18443'); DELETE FROM source_id_local_sessions; DELETE FROM source_id_sessions; DELETE FROM source_id_binding_audits; DELETE FROM external_identities; DELETE FROM source_id_enrollments; DELETE FROM source_id_binding_reviews; DELETE FROM oidc_transactions; COMMIT;")
run(['docker','restart','oa-legacy79-mock-20261016'])
print(json.dumps({'reset_synthetic_clone_auth_only':True,'real_identity_data_modified':False}))
