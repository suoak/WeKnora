-- First disable SourceID and revoke SourceID tokens; keep local accounts intact.
UPDATE auth_tokens SET is_revoked = TRUE WHERE session_id <> '';
DROP INDEX idx_auth_tokens_session_id;
ALTER TABLE auth_tokens DROP COLUMN session_id;
DROP TABLE oidc_transactions;
DROP TABLE source_id_logouts;
DROP TABLE source_id_sessions;
DROP TABLE external_identities;
DROP TABLE source_id_enrollments;
