-- Personal grants are independent of existing Wiki/Drive sources.
CREATE TABLE feishu_connections (
    user_id VARCHAR(36) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id VARCHAR(128) NOT NULL,
    open_id VARCHAR(128) NOT NULL,
    tokens TEXT NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    refresh_expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE feishu_oauth_states (
    hash VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    config_version VARCHAR(36) NOT NULL,
    claimed BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);
CREATE INDEX idx_feishu_oauth_expiry ON feishu_oauth_states(expires_at);
CREATE TABLE feishu_personal_imports (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    app_id VARCHAR(128) NOT NULL,
    open_id VARCHAR(128) NOT NULL,
    kb_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    document_token VARCHAR(256) NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    source_modified VARCHAR(64) NOT NULL DEFAULT '',
    coverage TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    sync_lease VARCHAR(36) NOT NULL DEFAULT '',
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, app_id, kb_id, resource_type, document_token)
);
