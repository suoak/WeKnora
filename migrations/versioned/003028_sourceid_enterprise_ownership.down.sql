-- Keep existing enterprise sessions unusable before removing ownership proof.
UPDATE source_id_sessions SET revoked=TRUE WHERE user_id IN (SELECT user_id FROM source_id_account_ownership);
UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_sessions WHERE revoked=TRUE);
DROP TABLE source_id_ownership_audits;
DROP TABLE source_id_account_ownership;
DROP INDEX idx_sourceid_users_normalized_email;
