-- Application runtime remains PostgreSQL-only. Preserve schema parity and fail closed on mixed principals.
ALTER TABLE feishu_personal_imports ADD COLUMN application_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE feishu_personal_imports ADD COLUMN auth_mode VARCHAR(32) NOT NULL DEFAULT 'user_oauth' CHECK (
 (auth_mode='user_oauth' AND open_id<>'' AND application_id='') OR
 (auth_mode='application_grant' AND open_id='' AND application_id=app_id AND application_id<>''));
ALTER TABLE feishu_personal_attempts ADD COLUMN application_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE feishu_personal_attempts ADD COLUMN auth_mode VARCHAR(32) NOT NULL DEFAULT 'user_oauth' CHECK (
 (auth_mode='user_oauth' AND grant_epoch<>'' AND application_id='') OR
 (auth_mode='application_grant' AND grant_epoch='' AND application_id<>''));
CREATE UNIQUE INDEX feishu_application_resource_unique ON feishu_personal_imports(app_id,kb_id,resource_type,document_token) WHERE auth_mode='application_grant';
CREATE TRIGGER feishu_attempt_principal_insert BEFORE INSERT ON feishu_personal_attempts
 WHEN NOT EXISTS(SELECT 1 FROM feishu_personal_imports i WHERE i.id=NEW.import_id AND i.auth_mode=NEW.auth_mode AND i.application_id=NEW.application_id)
 BEGIN SELECT RAISE(ABORT,'feishu attempt principal mismatch'); END;
CREATE TRIGGER feishu_attempt_principal_update BEFORE UPDATE ON feishu_personal_attempts
 WHEN NOT EXISTS(SELECT 1 FROM feishu_personal_imports i WHERE i.id=NEW.import_id AND i.auth_mode=NEW.auth_mode AND i.application_id=NEW.application_id)
 BEGIN SELECT RAISE(ABORT,'feishu attempt principal mismatch'); END;
CREATE TRIGGER feishu_import_principal_update BEFORE UPDATE OF auth_mode,application_id ON feishu_personal_imports
 WHEN EXISTS(SELECT 1 FROM feishu_personal_attempts a WHERE a.import_id=OLD.id AND (a.auth_mode<>NEW.auth_mode OR a.application_id<>NEW.application_id))
 BEGIN SELECT RAISE(ABORT,'feishu import principal mismatch'); END;
