-- Retaining rollback: never delete user, binding, decision, or exception data.
UPDATE source_id_link_policy_state SET enabled=FALSE WHERE id=1;
UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_local_link_policies);
UPDATE source_id_sessions SET revoked=TRUE WHERE id IN (SELECT id FROM source_id_local_link_policies);
-- Tables/indexes are intentionally retained; a later up is idempotent.
