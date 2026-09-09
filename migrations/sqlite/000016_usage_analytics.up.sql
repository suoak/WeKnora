CREATE TABLE model_usage_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_key TEXT NOT NULL UNIQUE,
    tenant_id INTEGER NOT NULL,
    principal_type TEXT NOT NULL DEFAULT 'system/unknown',
    principal_id TEXT,
    channel TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL,
    model_id TEXT,
    model_type TEXT,
    session_id TEXT,
    message_id TEXT,
    input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    total_tokens INTEGER NOT NULL DEFAULT 0 CHECK (total_tokens >= 0),
    cache_read_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    reasoning_tokens INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
    usage_source TEXT NOT NULL CHECK (usage_source IN ('provider', 'aggregated', 'unknown')),
    status TEXT NOT NULL,
    request_id TEXT,
    trace_id TEXT,
    occurred_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_model_usage_tenant_time ON model_usage_events (tenant_id, occurred_at DESC);
CREATE INDEX idx_model_usage_model_time ON model_usage_events (model_id, occurred_at DESC);
CREATE INDEX idx_model_usage_operation_time ON model_usage_events (operation, occurred_at DESC);

CREATE TABLE mcp_usage_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_key TEXT NOT NULL UNIQUE,
    caller_tenant_id INTEGER,
    principal_type TEXT NOT NULL DEFAULT 'system/unknown',
    principal_id TEXT,
    api_key_id INTEGER,
    direction TEXT NOT NULL CHECK (direction = 'inbound'),
    tool_name TEXT NOT NULL,
    client_name TEXT,
    client_version TEXT,
    transport TEXT,
    success INTEGER NOT NULL,
    error_code TEXT,
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    request_id TEXT,
    trace_id TEXT,
    occurred_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_mcp_usage_tenant_time ON mcp_usage_events (caller_tenant_id, occurred_at DESC);
CREATE INDEX idx_mcp_usage_tool_time ON mcp_usage_events (tool_name, occurred_at DESC);

CREATE TABLE usage_resource_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_kind TEXT NOT NULL CHECK (event_kind IN ('model', 'mcp')),
    event_id INTEGER NOT NULL,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('knowledge_base', 'knowledge')),
    resource_id TEXT NOT NULL,
    resource_tenant_id INTEGER NOT NULL,
    UNIQUE (event_kind, event_id, resource_type, resource_id)
);

CREATE INDEX idx_usage_resource_owner ON usage_resource_links (resource_tenant_id, resource_type, resource_id);
CREATE INDEX idx_usage_resource_event ON usage_resource_links (event_kind, event_id);
CREATE INDEX idx_usage_resource_lookup ON usage_resource_links (resource_type, resource_id, event_kind, event_id);
