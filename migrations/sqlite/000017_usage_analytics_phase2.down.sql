DROP INDEX idx_mcp_usage_service_time;
DROP INDEX idx_mcp_usage_direction_time;
DROP INDEX idx_mcp_usage_tenant_time;
DROP INDEX idx_mcp_usage_tool_time;

ALTER TABLE mcp_usage_events RENAME TO mcp_usage_events_phase2;
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

-- This insert intentionally fails transactionally if outbound rows exist;
-- operators must retain or export Phase 2 history before downgrading.
INSERT INTO mcp_usage_events (
    id, event_key, caller_tenant_id, principal_type, principal_id, api_key_id,
    direction, tool_name, client_name, client_version, transport, success,
    error_code, latency_ms, request_id, trace_id, occurred_at, created_at
)
SELECT id, event_key, caller_tenant_id, principal_type, principal_id, api_key_id,
    direction, tool_name, client_name, client_version, transport, success,
    error_code, latency_ms, request_id, trace_id, occurred_at, created_at
FROM mcp_usage_events_phase2;

DROP TABLE mcp_usage_events_phase2;
CREATE INDEX idx_mcp_usage_tenant_time ON mcp_usage_events (caller_tenant_id, occurred_at DESC);
CREATE INDEX idx_mcp_usage_tool_time ON mcp_usage_events (tool_name, occurred_at DESC);
DROP INDEX idx_model_usage_channel_time;
