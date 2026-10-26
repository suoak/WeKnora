-- Retain publication audits in feishu_sharing_approvals.
-- +migrate StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM feishu_import_batches WHERE publication_consent_at IS NOT NULL)
 OR EXISTS(SELECT 1 FROM feishu_import_batch_items WHERE sharing_approval_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Refusing to discard Feishu publication consent and version bindings';
 END IF;
END $$;
-- +migrate StatementEnd
ALTER TABLE feishu_import_batch_items DROP COLUMN sharing_approval_id;
ALTER TABLE feishu_import_batches DROP COLUMN publication_consent_at;
