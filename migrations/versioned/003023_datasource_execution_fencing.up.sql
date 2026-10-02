-- A logical run can be queued/retry-waiting while public status remains running.
-- Generation is the durable attempt fence; claim is its exclusive execution right.
ALTER TABLE sync_logs ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sync_logs ADD COLUMN execution_claimed BOOLEAN NOT NULL DEFAULT FALSE;
-- Quiesce/drain old workers before upgrade: pre-fencing workers cannot honor CAS.
UPDATE sync_logs SET status = 'failed', finished_at = CURRENT_TIMESTAMP,
    error_message = 'Sync interrupted by execution fencing upgrade'
WHERE status IN ('running', 'pending');
