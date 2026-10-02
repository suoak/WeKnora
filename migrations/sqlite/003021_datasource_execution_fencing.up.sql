ALTER TABLE sync_logs ADD COLUMN execution_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_logs ADD COLUMN execution_claimed BOOLEAN NOT NULL DEFAULT FALSE;
-- Upgrade must run with old workers stopped, just as on PostgreSQL.
UPDATE sync_logs SET status = 'failed', finished_at = CURRENT_TIMESTAMP,
    error_message = 'Sync interrupted by execution fencing upgrade'
WHERE status IN ('running', 'pending');
