-- Refuse rollback after a handoff rather than rewriting historical principals.
ALTER TABLE feishu_personal_attempts DROP CONSTRAINT feishu_attempt_principal_fk;
ALTER TABLE feishu_personal_attempts ADD CONSTRAINT feishu_attempt_principal_fk
 FOREIGN KEY(import_id,auth_mode,application_id)
 REFERENCES feishu_personal_imports(id,auth_mode,application_id) ON UPDATE RESTRICT ON DELETE RESTRICT;
DROP TRIGGER feishu_enforce_current_attempt_principal ON feishu_personal_attempts;
DROP FUNCTION feishu_enforce_current_attempt_principal();
DROP TRIGGER feishu_record_import_principal ON feishu_personal_imports;
DROP FUNCTION feishu_record_import_principal();
DROP TABLE feishu_import_principal_history;
