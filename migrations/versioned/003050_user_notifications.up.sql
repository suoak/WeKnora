CREATE TABLE user_notifications (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    notification_type VARCHAR(64) NOT NULL,
    title VARCHAR(256) NOT NULL,
    content TEXT NOT NULL,
    reference_type VARCHAR(64) NOT NULL,
    reference_id VARCHAR(36) NOT NULL,
    tenant_id BIGINT NOT NULL,
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    read_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_user_notification_event UNIQUE (user_id, notification_type, reference_type, reference_id)
);
CREATE INDEX idx_user_notifications_inbox ON user_notifications (user_id, is_read, created_at DESC);
CREATE INDEX idx_user_notifications_reference ON user_notifications (reference_type, reference_id);

ALTER TABLE tenant_portal_configs ALTER COLUMN allow_access_request SET DEFAULT TRUE;
