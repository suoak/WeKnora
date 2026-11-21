ALTER TABLE tenant_portal_configs ALTER COLUMN allow_access_request SET DEFAULT FALSE;
DROP TABLE IF EXISTS user_notifications;
