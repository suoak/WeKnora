CREATE TEMP TABLE feishu_recovery_rollback_guard (n INTEGER CHECK(n=0));
INSERT INTO feishu_recovery_rollback_guard SELECT count(*) FROM feishu_personal_attempts;
DROP TABLE feishu_recovery_rollback_guard;
DROP TABLE feishu_personal_attempts;
DROP TABLE feishu_personal_grant_epochs;
DROP TABLE feishu_personal_resources;
