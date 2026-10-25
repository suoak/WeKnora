-- Additive V2 control state. No updates to existing credentials or knowledge.
CREATE TABLE feishu_private_kbs (
 kb_id VARCHAR(36) PRIMARY KEY REFERENCES knowledge_bases(id) ON DELETE RESTRICT,
 owner_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE feishu_import_batches (
 id VARCHAR(36) PRIMARY KEY,
 owner_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
 kb_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE RESTRICT,
 auth_mode VARCHAR(32) NOT NULL CHECK (auth_mode IN ('user_oauth','application_grant')),
 connection_id VARCHAR(64) NOT NULL,
 config_version VARCHAR(64) NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 payload_hash VARCHAR(64) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(owner_id,kb_id,idempotency_key)
);
CREATE TABLE feishu_import_batch_items (
 id VARCHAR(36) PRIMARY KEY,
 batch_id VARCHAR(36) NOT NULL REFERENCES feishu_import_batches(id) ON DELETE RESTRICT,
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 99),
 url TEXT NOT NULL,
 canonical VARCHAR(64) NOT NULL DEFAULT '',
 status VARCHAR(32) NOT NULL DEFAULT 'pending',
 error_category VARCHAR(64) NOT NULL DEFAULT '',
 import_id VARCHAR(36) NOT NULL DEFAULT '',
 retry_count INTEGER NOT NULL DEFAULT 0 CHECK(retry_count BETWEEN 0 AND 3),
 lease_owner VARCHAR(36) NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(batch_id,ordinal)
);
CREATE INDEX feishu_batch_pending ON feishu_import_batch_items(status,lease_until,updated_at);
CREATE TABLE feishu_sharing_approvals (
 id VARCHAR(36) PRIMARY KEY,
 owner_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 kb_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE RESTRICT,
 app_id VARCHAR(128) NOT NULL,
 resource_type VARCHAR(32) NOT NULL,
 resource_token VARCHAR(256) NOT NULL,
 revision VARCHAR(128) NOT NULL,
 config_version VARCHAR(64) NOT NULL,
 state VARCHAR(16) NOT NULL DEFAULT 'approved' CHECK(state IN('approved','revoked')),
 approved_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 reason VARCHAR(512) NOT NULL CHECK(length(trim(reason))>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX feishu_sharing_lookup ON feishu_sharing_approvals(owner_id,kb_id,app_id,resource_type,resource_token,revision,state);
