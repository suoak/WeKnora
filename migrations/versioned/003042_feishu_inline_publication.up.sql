-- Explicit user action, bound to the existing batch owner, destination and identity.
-- NULL preserves the approval semantics of every pre-existing batch.
ALTER TABLE feishu_import_batches ADD COLUMN publication_consent_at TIMESTAMPTZ;
-- A first verified resource version can be bound only once. Approval creation
-- and this binding are committed together under the existing item lease.
ALTER TABLE feishu_import_batch_items ADD COLUMN sharing_approval_id VARCHAR(36)
 REFERENCES feishu_sharing_approvals(id) ON DELETE RESTRICT;
