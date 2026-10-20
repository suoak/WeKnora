-- Retain users, bindings and ownership/audit records. Disable derived sessions.
UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT s.id FROM source_id_sessions s JOIN source_id_jit_registrations j ON s.issuer=j.issuer AND s.user_id=j.user_id);
UPDATE source_id_sessions SET revoked=TRUE WHERE EXISTS (SELECT 1 FROM source_id_jit_registrations j WHERE j.issuer=source_id_sessions.issuer AND j.user_id=source_id_sessions.user_id);
DROP TABLE source_id_jit_registrations;
