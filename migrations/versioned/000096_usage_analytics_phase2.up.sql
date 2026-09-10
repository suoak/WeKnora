ALTER TABLE mcp_usage_events
    DROP CONSTRAINT IF EXISTS mcp_usage_events_direction_check;
ALTER TABLE mcp_usage_events
    ADD CONSTRAINT mcp_usage_events_direction_check
    CHECK (direction IN ('inbound', 'outbound'));
ALTER TABLE mcp_usage_events
    ADD COLUMN IF NOT EXISTS mcp_service_id VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_model_usage_channel_time
    ON model_usage_events (channel, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_usage_direction_time
    ON mcp_usage_events (direction, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_usage_service_time
    ON mcp_usage_events (mcp_service_id, occurred_at DESC);
