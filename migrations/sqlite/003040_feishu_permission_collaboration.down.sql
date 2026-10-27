CREATE TEMP TABLE feishu_collaboration_rollback_guard (n INTEGER CHECK(n=0));
INSERT INTO feishu_collaboration_rollback_guard SELECT count(*) FROM feishu_permission_collaboration;
DROP TABLE feishu_permission_collaboration;
DROP TABLE feishu_collaboration_rollback_guard;
