CREATE TABLE feishu_personal_resources (
 import_id VARCHAR(36) PRIMARY KEY,
 logical_knowledge_id VARCHAR(36) NOT NULL UNIQUE,
 active_version_id VARCHAR(36) NOT NULL DEFAULT '',
 candidate_version_id VARCHAR(36) NOT NULL DEFAULT '',
 current_attempt_id VARCHAR(36) NOT NULL DEFAULT '',
 generation BIGINT NOT NULL DEFAULT 0 CHECK(generation >= 0)
);
CREATE TABLE feishu_personal_grant_epochs (
 user_id VARCHAR(36) PRIMARY KEY REFERENCES feishu_connections(user_id) ON DELETE CASCADE,
 epoch VARCHAR(36) NOT NULL
);
CREATE TABLE feishu_personal_attempts (
 id VARCHAR(36) PRIMARY KEY,
 import_id VARCHAR(36) NOT NULL REFERENCES feishu_personal_resources(import_id) ON DELETE RESTRICT,
 logical_knowledge_id VARCHAR(36) NOT NULL,
 candidate_version_id VARCHAR(36) NOT NULL UNIQUE,
 revision TEXT NOT NULL,
 expected_active_version VARCHAR(36) NOT NULL DEFAULT '',
 caller_tenant_id BIGINT NOT NULL,
 app_config_version VARCHAR(36) NOT NULL,
 grant_epoch VARCHAR(36) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending',
 owner_nonce VARCHAR(36) NOT NULL DEFAULT '',
 fence BIGINT NOT NULL DEFAULT 0 CHECK(fence >= 0),
 lease_until TIMESTAMP WITH TIME ZONE,
 retry_after TIMESTAMP WITH TIME ZONE,
 retry_count INTEGER NOT NULL DEFAULT 0,
 last_error_category VARCHAR(64) NOT NULL DEFAULT '',
 manifest_key TEXT NOT NULL DEFAULT '',
 manifest_hash VARCHAR(64) NOT NULL DEFAULT '',
 manifest_size BIGINT NOT NULL DEFAULT 0,
 manifest_name TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(import_id, revision, app_config_version, grant_epoch)
);
CREATE INDEX idx_feishu_personal_recovery ON feishu_personal_attempts(status, retry_after, id);
INSERT INTO feishu_personal_resources(import_id, logical_knowledge_id, active_version_id)
 SELECT id, CASE WHEN knowledge_id <> '' THEN knowledge_id ELSE gen_random_uuid()::text END, knowledge_id FROM feishu_personal_imports;
INSERT INTO feishu_personal_grant_epochs(user_id, epoch) SELECT user_id, gen_random_uuid()::text FROM feishu_connections;
