-- Revoke before removing metadata: a rollback cannot turn a local session into
-- an unbounded external session. Archive metadata before an approved downgrade.
UPDATE auth_tokens SET is_revoked = TRUE WHERE session_id IN (SELECT id FROM source_id_local_sessions);
UPDATE source_id_sessions SET revoked = TRUE WHERE id IN (SELECT id FROM source_id_local_sessions);
DROP TABLE source_id_local_sessions;
