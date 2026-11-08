CREATE TABLE IF NOT EXISTS mcp_usage_metadata_rollback (
    event_key VARCHAR(255) PRIMARY KEY,
    entry_point VARCHAR(32) NOT NULL,
    endpoint_id VARCHAR(64),
    client_source VARCHAR(32) NOT NULL
);
ALTER TABLE mcp_usage_events ADD COLUMN entry_point VARCHAR(32) NOT NULL DEFAULT 'legacy';
ALTER TABLE mcp_usage_events ADD COLUMN endpoint_id VARCHAR(64);
ALTER TABLE mcp_usage_events ADD COLUMN client_source VARCHAR(32) NOT NULL DEFAULT 'unknown';
UPDATE mcp_usage_events SET
    entry_point=(SELECT b.entry_point FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key),
    endpoint_id=(SELECT b.endpoint_id FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key),
    client_source=(SELECT b.client_source FROM mcp_usage_metadata_rollback b WHERE b.event_key=mcp_usage_events.event_key)
WHERE event_key IN (SELECT event_key FROM mcp_usage_metadata_rollback);
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
