DROP INDEX IF EXISTS idx_mcp_usage_service_time;
DROP INDEX IF EXISTS idx_mcp_usage_direction_time;
DROP INDEX IF EXISTS idx_model_usage_channel_time;

ALTER TABLE mcp_usage_events DROP COLUMN IF EXISTS mcp_service_id;
ALTER TABLE mcp_usage_events
    DROP CONSTRAINT IF EXISTS mcp_usage_events_direction_check;
ALTER TABLE mcp_usage_events
    ADD CONSTRAINT mcp_usage_events_direction_check
    CHECK (direction = 'inbound');
