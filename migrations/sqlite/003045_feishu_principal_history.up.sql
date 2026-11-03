CREATE TABLE feishu_import_principal_history (
 import_id VARCHAR(36) NOT NULL REFERENCES feishu_personal_imports(id) ON DELETE RESTRICT,
 auth_mode VARCHAR(32) NOT NULL CHECK(auth_mode IN ('user_oauth','application_grant')),
 application_id VARCHAR(128) NOT NULL,
 snapshot TEXT NOT NULL,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(import_id,auth_mode,application_id)
);
INSERT INTO feishu_import_principal_history(import_id,auth_mode,application_id,snapshot)
 SELECT id,auth_mode,application_id,json_object('id',id,'auth_mode',auth_mode,'application_id',application_id,'user_id',user_id,'open_id',open_id,'knowledge_id',knowledge_id) FROM feishu_personal_imports;
CREATE TRIGGER feishu_record_import_principal_insert AFTER INSERT ON feishu_personal_imports
 BEGIN INSERT OR IGNORE INTO feishu_import_principal_history(import_id,auth_mode,application_id,snapshot)
 VALUES(NEW.id,NEW.auth_mode,NEW.application_id,json_object('id',NEW.id,'auth_mode',NEW.auth_mode,'application_id',NEW.application_id,'user_id',NEW.user_id,'open_id',NEW.open_id,'knowledge_id',NEW.knowledge_id)); END;
CREATE TRIGGER feishu_record_import_principal_update AFTER UPDATE OF auth_mode,application_id ON feishu_personal_imports
 BEGIN INSERT OR IGNORE INTO feishu_import_principal_history(import_id,auth_mode,application_id,snapshot)
 VALUES(NEW.id,NEW.auth_mode,NEW.application_id,json_object('id',NEW.id,'auth_mode',NEW.auth_mode,'application_id',NEW.application_id,'user_id',NEW.user_id,'open_id',NEW.open_id,'knowledge_id',NEW.knowledge_id)); END;
DROP TRIGGER feishu_import_principal_update;
DROP TRIGGER feishu_attempt_principal_update;
CREATE TRIGGER feishu_attempt_principal_update BEFORE UPDATE ON feishu_personal_attempts
 WHEN (NEW.import_id<>OLD.import_id OR NEW.auth_mode<>OLD.auth_mode OR NEW.application_id<>OLD.application_id)
 AND NOT EXISTS(SELECT 1 FROM feishu_personal_imports i WHERE i.id=NEW.import_id AND i.auth_mode=NEW.auth_mode AND i.application_id=NEW.application_id)
 BEGIN SELECT RAISE(ABORT,'feishu attempt principal mismatch'); END;
