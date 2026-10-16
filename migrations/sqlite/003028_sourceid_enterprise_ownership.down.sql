UPDATE source_id_sessions SET revoked=TRUE WHERE EXISTS (SELECT 1 FROM source_id_account_ownership o WHERE o.user_id=source_id_sessions.user_id AND o.issuer=source_id_sessions.issuer);
UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_sessions WHERE revoked=TRUE);
DROP TABLE source_id_ownership_audits;
DROP TABLE source_id_account_ownership;
DROP INDEX idx_sourceid_users_normalized_email;
