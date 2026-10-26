CREATE TEMP TABLE feishu_publication_rollback_guard(n INTEGER CHECK(n=0));
INSERT INTO feishu_publication_rollback_guard
 SELECT count(*) FROM feishu_import_batches WHERE publication_consent_at IS NOT NULL;
INSERT INTO feishu_publication_rollback_guard
 SELECT count(*) FROM feishu_import_batch_items WHERE sharing_approval_id IS NOT NULL;
ALTER TABLE feishu_import_batch_items DROP COLUMN sharing_approval_id;
ALTER TABLE feishu_import_batches DROP COLUMN publication_consent_at;
DROP TABLE feishu_publication_rollback_guard;
