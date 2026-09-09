CREATE TABLE IF NOT EXISTS model_usage_events (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(255) NOT NULL UNIQUE,
    tenant_id BIGINT NOT NULL,
    principal_type VARCHAR(64) NOT NULL DEFAULT 'system/unknown',
    principal_id VARCHAR(512),
    channel VARCHAR(50) NOT NULL DEFAULT '',
    operation VARCHAR(64) NOT NULL,
    model_id VARCHAR(64),
    model_type VARCHAR(32),
    session_id VARCHAR(36),
    message_id VARCHAR(36),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    total_tokens BIGINT NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    reasoning_tokens BIGINT NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
    usage_source VARCHAR(16) NOT NULL CHECK (usage_source IN ('provider', 'aggregated', 'unknown')),
    status VARCHAR(16) NOT NULL,
    request_id VARCHAR(255),
    trace_id VARCHAR(255),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_usage_tenant_time ON model_usage_events (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_model_time ON model_usage_events (model_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_operation_time ON model_usage_events (operation, occurred_at DESC);

CREATE TABLE IF NOT EXISTS mcp_usage_events (
    id BIGSERIAL PRIMARY KEY,
    event_key VARCHAR(255) NOT NULL UNIQUE,
    caller_tenant_id BIGINT,
    principal_type VARCHAR(64) NOT NULL DEFAULT 'system/unknown',
    principal_id VARCHAR(512),
    api_key_id BIGINT,
    direction VARCHAR(16) NOT NULL CHECK (direction = 'inbound'),
    tool_name VARCHAR(255) NOT NULL,
    client_name VARCHAR(128),
    client_version VARCHAR(64),
    transport VARCHAR(16),
    success BOOLEAN NOT NULL,
    error_code VARCHAR(64),
    latency_ms BIGINT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    request_id VARCHAR(255),
    trace_id VARCHAR(255),
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mcp_usage_tenant_time ON mcp_usage_events (caller_tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_usage_tool_time ON mcp_usage_events (tool_name, occurred_at DESC);

CREATE TABLE IF NOT EXISTS usage_resource_links (
    id BIGSERIAL PRIMARY KEY,
    event_kind VARCHAR(16) NOT NULL CHECK (event_kind IN ('model', 'mcp')),
    event_id BIGINT NOT NULL,
    resource_type VARCHAR(32) NOT NULL CHECK (resource_type IN ('knowledge_base', 'knowledge')),
    resource_id VARCHAR(64) NOT NULL,
    resource_tenant_id BIGINT NOT NULL,
    UNIQUE (event_kind, event_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_resource_owner ON usage_resource_links (resource_tenant_id, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_usage_resource_event ON usage_resource_links (event_kind, event_id);
CREATE INDEX IF NOT EXISTS idx_usage_resource_lookup ON usage_resource_links (resource_type, resource_id, event_kind, event_id);
