-- Disposable CI database only. Reuse the actual existing Portal DDL.
CREATE TABLE tenants (id INTEGER PRIMARY KEY);
CREATE TABLE organizations (id VARCHAR(36) PRIMARY KEY);
\i migrations/versioned/000093_knowledge_portal.up.sql
INSERT INTO tenants VALUES (7), (8);
INSERT INTO tenant_portal_configs (tenant_id,status,display_name,created_by,updated_by)
VALUES (7,'published','Existing explicit opt-out','owner','owner');
INSERT INTO tenant_portal_stages (tenant_id,stage_key) VALUES (7,'concept_product');
INSERT INTO tenant_access_requests (id,tenant_id,applicant_user_id,reason)
VALUES ('historical-request',7,'applicant','A historical reason');

BEGIN;
\i migrations/versioned/003050_user_notifications.up.sql
COMMIT;
INSERT INTO tenant_portal_configs (tenant_id,status,display_name,created_by,updated_by)
VALUES (8,'published','New requestable space','owner','owner');
DO $$ BEGIN
  IF (SELECT allow_access_request FROM tenant_portal_configs WHERE tenant_id=7) THEN
    RAISE EXCEPTION 'migration changed an existing opt-out';
  END IF;
  IF NOT (SELECT allow_access_request FROM tenant_portal_configs WHERE tenant_id=8) THEN
    RAISE EXCEPTION 'new config did not default to requestable';
  END IF;
END $$;
INSERT INTO user_notifications (id,user_id,notification_type,title,content,reference_type,reference_id,tenant_id)
VALUES ('n1','owner','access_request_submitted','title','body','tenant_access_request','historical-request',7);
DO $$ BEGIN
  BEGIN
    INSERT INTO user_notifications (id,user_id,notification_type,title,content,reference_type,reference_id,tenant_id)
    VALUES ('n2','owner','access_request_submitted','title','body','tenant_access_request','historical-request',7);
    RAISE EXCEPTION 'duplicate event was allowed';
  EXCEPTION WHEN unique_violation THEN NULL;
  END;
END $$;
UPDATE user_notifications SET is_read=TRUE, read_at=CURRENT_TIMESTAMP WHERE id='n1';

BEGIN;
\i migrations/versioned/003050_user_notifications.down.sql
COMMIT;
DO $$ BEGIN
  IF to_regclass('user_notifications') IS NOT NULL THEN RAISE EXCEPTION 'notification table survived rollback'; END IF;
  IF (SELECT COUNT(*) FROM tenant_portal_stages WHERE tenant_id=7) <> 1 THEN RAISE EXCEPTION 'stages changed'; END IF;
  IF (SELECT COUNT(*) FROM tenant_access_requests WHERE id='historical-request' AND status='pending') <> 1 THEN RAISE EXCEPTION 'requests changed'; END IF;
END $$;
DELETE FROM tenant_portal_configs WHERE tenant_id=8;
INSERT INTO tenant_portal_configs (tenant_id,status,display_name,created_by,updated_by)
VALUES (8,'published','Rollback default','owner','owner');
DO $$ BEGIN
  IF (SELECT allow_access_request FROM tenant_portal_configs WHERE tenant_id=8) THEN RAISE EXCEPTION 'rollback did not restore default false'; END IF;
END $$;
BEGIN;
\i migrations/versioned/003050_user_notifications.up.sql
COMMIT;
DO $$ BEGIN
  IF (SELECT allow_access_request FROM tenant_portal_configs WHERE tenant_id=8) THEN RAISE EXCEPTION 'repeat upgrade changed stored opt-out'; END IF;
  IF to_regclass('user_notifications') IS NULL THEN RAISE EXCEPTION 'repeat upgrade failed'; END IF;
END $$;
