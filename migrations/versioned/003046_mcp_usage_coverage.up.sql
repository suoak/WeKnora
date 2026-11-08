ALTER TABLE mcp_usage_events ADD COLUMN entry_point VARCHAR(32) NOT NULL DEFAULT 'legacy';
ALTER TABLE mcp_usage_events ADD COLUMN endpoint_id VARCHAR(64);
ALTER TABLE mcp_usage_events ADD COLUMN client_source VARCHAR(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_resource_links DROP CONSTRAINT IF EXISTS usage_resource_links_resource_type_check;
ALTER TABLE usage_resource_links ADD CONSTRAINT usage_resource_links_resource_type_check
CHECK (resource_type IN ('knowledge_base', 'knowledge', 'knowledge_base_target', 'knowledge_base_hit'));
INSERT INTO system_settings (key,value,value_type,category,description,created_at,updated_at)
VALUES ('usage.analytics.mcp_server_collecting_since',to_jsonb(to_char(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),'string','internal','Go MCP server collection rollout epoch',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)
ON CONFLICT (key) DO NOTHING;
