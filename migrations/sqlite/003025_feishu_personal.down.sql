-- Imported knowledge and indexes are intentionally retained.
DROP TABLE IF EXISTS feishu_personal_imports;
DROP TABLE IF EXISTS feishu_oauth_states;
DROP TABLE IF EXISTS feishu_connections;
DELETE FROM system_settings WHERE key = 'integration.feishu.personal_app';
