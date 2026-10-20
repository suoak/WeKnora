-- Application identity shares the existing resource/attempt/outbox protocol.
ALTER TABLE feishu_personal_imports ADD COLUMN auth_mode VARCHAR(32) NOT NULL DEFAULT 'user_oauth';
ALTER TABLE feishu_personal_imports ADD COLUMN application_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE feishu_personal_attempts ADD COLUMN auth_mode VARCHAR(32) NOT NULL DEFAULT 'user_oauth';
ALTER TABLE feishu_personal_attempts ADD COLUMN application_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE feishu_personal_imports ADD CONSTRAINT feishu_import_auth_identity CHECK (
 (auth_mode='user_oauth' AND open_id<>'' AND application_id='') OR
 (auth_mode='application_grant' AND open_id='' AND application_id=app_id AND application_id<>''));
ALTER TABLE feishu_personal_attempts ADD CONSTRAINT feishu_attempt_auth_identity CHECK (
 (auth_mode='user_oauth' AND grant_epoch<>'' AND application_id='') OR
 (auth_mode='application_grant' AND grant_epoch='' AND application_id<>''));
ALTER TABLE feishu_personal_imports ADD CONSTRAINT feishu_import_principal_key UNIQUE(id,auth_mode,application_id);
ALTER TABLE feishu_personal_attempts ADD CONSTRAINT feishu_attempt_principal_fk FOREIGN KEY(import_id,auth_mode,application_id)
 REFERENCES feishu_personal_imports(id,auth_mode,application_id) ON UPDATE RESTRICT ON DELETE RESTRICT;
CREATE UNIQUE INDEX feishu_application_resource_unique ON feishu_personal_imports(app_id,kb_id,resource_type,document_token) WHERE auth_mode='application_grant';
