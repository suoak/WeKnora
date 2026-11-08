CREATE TABLE IF NOT EXISTS mcp_usage_metadata_rollback (
    event_key VARCHAR(255) PRIMARY KEY,
    entry_point VARCHAR(32) NOT NULL,
    endpoint_id VARCHAR(64),
    client_source VARCHAR(32) NOT NULL
);
ALTER TABLE mcp_usage_events ADD COLUMN IF NOT EXISTS entry_point VARCHAR(32) NOT NULL DEFAULT 'legacy';
ALTER TABLE mcp_usage_events ADD COLUMN IF NOT EXISTS endpoint_id VARCHAR(64);
ALTER TABLE mcp_usage_events ADD COLUMN IF NOT EXISTS client_source VARCHAR(32) NOT NULL DEFAULT 'unknown';
UPDATE mcp_usage_events e
SET entry_point=b.entry_point, endpoint_id=b.endpoint_id, client_source=b.client_source
FROM mcp_usage_metadata_rollback b WHERE e.event_key=b.event_key;
ALTER TABLE usage_resource_links DROP CONSTRAINT IF EXISTS usage_resource_links_resource_type_check;
ALTER TABLE usage_resource_links ADD CONSTRAINT usage_resource_links_resource_type_check
CHECK (resource_type IN ('knowledge_base', 'knowledge', 'knowledge_base_target', 'knowledge_base_hit'));
INSERT INTO system_settings (key,value,value_type,category,description,created_at,updated_at)
VALUES ('usage.analytics.mcp_server_collecting_since',to_jsonb(to_char(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),'string','internal','Go MCP server collection rollout epoch',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
ON CONFLICT (key) DO NOTHING;
