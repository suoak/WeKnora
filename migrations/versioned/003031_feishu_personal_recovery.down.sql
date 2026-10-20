DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM feishu_personal_attempts) THEN
  RAISE EXCEPTION 'personal recovery attempts must be archived and explicitly drained before rollback';
 END IF;
END $$;
DROP TABLE feishu_personal_attempts;
DROP TABLE feishu_personal_grant_epochs;
DROP TABLE feishu_personal_resources;
