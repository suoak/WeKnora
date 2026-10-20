-- No downgrade may discard application identity while its data still exists.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM feishu_personal_imports WHERE auth_mode='application_grant') OR EXISTS(SELECT 1 FROM feishu_personal_attempts WHERE auth_mode='application_grant') THEN
  RAISE EXCEPTION 'application identity data exists; disable admission and reconcile before downgrade';
 END IF;
END $$;
DROP INDEX feishu_application_resource_unique;
ALTER TABLE feishu_personal_attempts DROP CONSTRAINT feishu_attempt_principal_fk;
ALTER TABLE feishu_personal_imports DROP CONSTRAINT feishu_import_principal_key;
ALTER TABLE feishu_personal_attempts DROP CONSTRAINT feishu_attempt_auth_identity;
ALTER TABLE feishu_personal_imports DROP CONSTRAINT feishu_import_auth_identity;
ALTER TABLE feishu_personal_attempts DROP COLUMN application_id, DROP COLUMN auth_mode;
ALTER TABLE feishu_personal_imports DROP COLUMN application_id, DROP COLUMN auth_mode;
