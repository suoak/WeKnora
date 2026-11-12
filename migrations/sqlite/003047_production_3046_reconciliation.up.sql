-- Reconcile either historical 3046 schema without dropping event IDs or metadata.
-- The first matching projected column wins: existing metadata beats fallback
-- aliases. This is exercised against both real migration lineages in tests.
CREATE TABLE mcp_reconcile_sequences (name TEXT PRIMARY KEY, seq INTEGER);
INSERT INTO mcp_reconcile_sequences SELECT name,seq FROM sqlite_sequence
 WHERE name IN ('mcp_usage_events','usage_resource_links');
CREATE TABLE IF NOT EXISTS mcp_usage_metadata_rollback (
    event_key VARCHAR(255) PRIMARY KEY,
    entry_point VARCHAR(32) NOT NULL,
    endpoint_id VARCHAR(64),
    client_source VARCHAR(32) NOT NULL
);
CREATE TABLE mcp_usage_events_reconciled (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_key TEXT NOT NULL UNIQUE,
    caller_tenant_id INTEGER,
    principal_type TEXT NOT NULL DEFAULT 'system/unknown',
    principal_id TEXT,
    api_key_id INTEGER,
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    mcp_service_id TEXT,
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
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    entry_point VARCHAR(32) NOT NULL DEFAULT 'legacy',
    endpoint_id VARCHAR(64),
    client_source VARCHAR(32) NOT NULL DEFAULT 'unknown'
);
INSERT INTO mcp_usage_events_reconciled (id, event_key, caller_tenant_id, principal_type, principal_id, api_key_id, direction, mcp_service_id, tool_name, client_name, client_version, transport, success, error_code, latency_ms, request_id, trace_id, occurred_at, created_at, entry_point, endpoint_id, client_source)
SELECT id, event_key, caller_tenant_id, principal_type, principal_id, api_key_id, direction, mcp_service_id, tool_name, client_name, client_version, transport, success, error_code, latency_ms, request_id, trace_id, occurred_at, created_at, entry_point, endpoint_id, client_source FROM (
 SELECT *,
 COALESCE((SELECT entry_point FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key),'legacy') AS entry_point,
 (SELECT endpoint_id FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key) AS endpoint_id,
 COALESCE((SELECT client_source FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key),'unknown') AS client_source
 FROM mcp_usage_events
);
DROP TABLE mcp_usage_events;
ALTER TABLE mcp_usage_events_reconciled RENAME TO mcp_usage_events;
CREATE INDEX idx_mcp_usage_tenant_time ON mcp_usage_events (caller_tenant_id, occurred_at DESC);
CREATE INDEX idx_mcp_usage_tool_time ON mcp_usage_events (tool_name, occurred_at DESC);
CREATE INDEX idx_mcp_usage_direction_time ON mcp_usage_events (direction, occurred_at DESC);
CREATE INDEX idx_mcp_usage_service_time ON mcp_usage_events (mcp_service_id, occurred_at DESC);
CREATE TABLE usage_resource_links_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_kind TEXT NOT NULL CHECK (event_kind IN ('model', 'mcp')),
    event_id INTEGER NOT NULL,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('knowledge_base', 'knowledge', 'knowledge_base_target', 'knowledge_base_hit')),
    resource_id TEXT NOT NULL,
    resource_tenant_id INTEGER NOT NULL,
    UNIQUE (event_kind, event_id, resource_type, resource_id)
);

INSERT INTO usage_resource_links_next SELECT * FROM usage_resource_links;
DROP TABLE usage_resource_links;
ALTER TABLE usage_resource_links_next RENAME TO usage_resource_links;
CREATE INDEX idx_usage_resource_owner ON usage_resource_links (resource_tenant_id, resource_type, resource_id);
CREATE INDEX idx_usage_resource_event ON usage_resource_links (event_kind, event_id);
CREATE INDEX idx_usage_resource_lookup ON usage_resource_links (resource_type, resource_id, event_kind, event_id);
INSERT INTO system_settings (key,value,value_type,category,description,created_at,updated_at)
VALUES ('usage.analytics.mcp_server_collecting_since',json_quote(strftime('%Y-%m-%dT%H:%M:%fZ','now')),'string','internal','Go MCP server collection rollout epoch',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
ON CONFLICT (key) DO NOTHING;

INSERT INTO sqlite_sequence(name,seq)
 SELECT name,seq FROM mcp_reconcile_sequences old
 WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence current WHERE current.name=old.name);
UPDATE sqlite_sequence SET seq=MAX(seq,COALESCE((SELECT old.seq FROM mcp_reconcile_sequences old WHERE old.name=sqlite_sequence.name),0))
 WHERE name IN ('mcp_usage_events','usage_resource_links');
DROP TABLE mcp_reconcile_sequences;
