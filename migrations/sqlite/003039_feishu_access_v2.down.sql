-- No CASCADE: rollback requires no dependent references or active V2 work.
CREATE TEMP TABLE feishu_v2_rollback_guard(n INTEGER CHECK(n=0));
INSERT INTO feishu_v2_rollback_guard SELECT
 (SELECT count(*) FROM feishu_private_kbs)+(SELECT count(*) FROM feishu_import_batches)+(SELECT count(*) FROM feishu_sharing_approvals);
DROP TABLE feishu_v2_rollback_guard;
DROP TABLE feishu_sharing_approvals;
DROP TABLE feishu_import_batch_items;
DROP TABLE feishu_import_batches;
DROP TABLE feishu_private_kbs;
