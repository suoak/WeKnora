-- Application rollback may keep the additive schema and version unchanged.
-- For an explicit schema downgrade, archive only the new metadata first.
-- Ledger rows, event keys, resource links, epochs and archived metadata survive.
CREATE TABLE IF NOT EXISTS mcp_usage_metadata_rollback (
    event_key VARCHAR(255) PRIMARY KEY,
    entry_point VARCHAR(32) NOT NULL,
    endpoint_id VARCHAR(64),
    client_source VARCHAR(32) NOT NULL
);
INSERT INTO mcp_usage_metadata_rollback (event_key,entry_point,endpoint_id,client_source)
SELECT event_key,entry_point,endpoint_id,client_source FROM mcp_usage_events WHERE true
ON CONFLICT (event_key) DO UPDATE SET
    entry_point=excluded.entry_point, endpoint_id=excluded.endpoint_id, client_source=excluded.client_source;
ALTER TABLE mcp_usage_events DROP COLUMN entry_point;
ALTER TABLE mcp_usage_events DROP COLUMN endpoint_id;
ALTER TABLE mcp_usage_events DROP COLUMN client_source;
-- Keep the expanded resource-type validation: older queries ignore new types.
-- The next upgrade restores archived metadata by the unique event key.
