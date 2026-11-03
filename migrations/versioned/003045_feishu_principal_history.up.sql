-- Preserve historical attempt principals when an unpublished failed resource
-- is explicitly handed to its same owner's newly verified personal identity.
-- 3043/3044 are reserved by the independent execution-ledger work.
CREATE TABLE feishu_import_principal_history (
 import_id VARCHAR(36) NOT NULL REFERENCES feishu_personal_imports(id) ON DELETE RESTRICT,
 auth_mode VARCHAR(32) NOT NULL CHECK(auth_mode IN ('user_oauth','application_grant')),
 application_id VARCHAR(128) NOT NULL,
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(import_id,auth_mode,application_id)
);
INSERT INTO feishu_import_principal_history(import_id,auth_mode,application_id,snapshot)
 SELECT id,auth_mode,application_id,to_jsonb(i) FROM feishu_personal_imports i;
CREATE FUNCTION feishu_record_import_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO feishu_import_principal_history(import_id,auth_mode,application_id,snapshot)
 VALUES(NEW.id,NEW.auth_mode,NEW.application_id,to_jsonb(NEW)) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER feishu_record_import_principal AFTER INSERT OR UPDATE OF auth_mode,application_id
 ON feishu_personal_imports FOR EACH ROW EXECUTE FUNCTION feishu_record_import_principal();
ALTER TABLE feishu_personal_attempts DROP CONSTRAINT feishu_attempt_principal_fk;
ALTER TABLE feishu_personal_attempts ADD CONSTRAINT feishu_attempt_principal_fk
 FOREIGN KEY(import_id,auth_mode,application_id)
 REFERENCES feishu_import_principal_history(import_id,auth_mode,application_id) ON UPDATE RESTRICT ON DELETE RESTRICT;
-- Historical identity is not authority for a new or changed attempt. Old
-- attempts may only retain their identity; current imports authorize new ones.
CREATE FUNCTION feishu_enforce_current_attempt_principal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.import_id,NEW.auth_mode,NEW.application_id)=(OLD.import_id,OLD.auth_mode,OLD.application_id) THEN
   RETURN NEW;
  END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM feishu_personal_imports i WHERE i.id=NEW.import_id
  AND i.auth_mode=NEW.auth_mode AND i.application_id=NEW.application_id) THEN
  RAISE EXCEPTION 'feishu attempt principal mismatch';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER feishu_enforce_current_attempt_principal BEFORE INSERT OR UPDATE OF import_id,auth_mode,application_id
 ON feishu_personal_attempts FOR EACH ROW EXECUTE FUNCTION feishu_enforce_current_attempt_principal();
