-- Abort if old attempts cannot be represented under the pre-handoff schema.
CREATE TEMP TABLE feishu_principal_rollback_guard(ok INTEGER CHECK(ok=1));
INSERT INTO feishu_principal_rollback_guard SELECT CASE WHEN EXISTS(
 SELECT 1 FROM feishu_personal_attempts a JOIN feishu_personal_imports i ON i.id=a.import_id
 WHERE a.auth_mode<>i.auth_mode OR a.application_id<>i.application_id) THEN 0 ELSE 1 END;
DROP TABLE feishu_principal_rollback_guard;
DROP TRIGGER feishu_record_import_principal_insert;
DROP TRIGGER feishu_record_import_principal_update;
DROP TRIGGER feishu_attempt_principal_update;
CREATE TRIGGER feishu_attempt_principal_update BEFORE UPDATE ON feishu_personal_attempts
 WHEN NOT EXISTS(SELECT 1 FROM feishu_personal_imports i WHERE i.id=NEW.import_id AND i.auth_mode=NEW.auth_mode AND i.application_id=NEW.application_id)
 BEGIN SELECT RAISE(ABORT,'feishu attempt principal mismatch'); END;
CREATE TRIGGER feishu_import_principal_update BEFORE UPDATE OF auth_mode,application_id ON feishu_personal_imports
 WHEN EXISTS(SELECT 1 FROM feishu_personal_attempts a WHERE a.import_id=OLD.id AND (a.auth_mode<>NEW.auth_mode OR a.application_id<>NEW.application_id))
 BEGIN SELECT RAISE(ABORT,'feishu import principal mismatch'); END;
DROP TABLE feishu_import_principal_history;
