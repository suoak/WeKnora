-- No CASCADE: rollback requires no dependent references or active V2 work.
-- +migrate StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM feishu_private_kbs) OR EXISTS(SELECT 1 FROM feishu_import_batches) OR EXISTS(SELECT 1 FROM feishu_sharing_approvals) THEN
  RAISE EXCEPTION 'V2 rollback requires controlled data evacuation; refusing to drop privacy or task state';
 END IF;
END $$;
-- +migrate StatementEnd
DROP TABLE feishu_sharing_approvals;
DROP TABLE feishu_import_batch_items;
DROP TABLE feishu_import_batches;
DROP TABLE feishu_private_kbs;
