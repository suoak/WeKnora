ALTER TABLE feishu_import_batches ADD COLUMN publication_consent_at TIMESTAMP;
ALTER TABLE feishu_import_batch_items ADD COLUMN sharing_approval_id VARCHAR(36)
 REFERENCES feishu_sharing_approvals(id) ON DELETE RESTRICT;
