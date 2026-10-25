DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM feishu_permission_collaboration) THEN
  RAISE EXCEPTION 'retain Feishu permission collaboration audit before rollback';
 END IF;
END $$;
DROP TABLE feishu_permission_collaboration;
