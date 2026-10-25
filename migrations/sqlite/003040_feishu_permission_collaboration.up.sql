CREATE TABLE feishu_permission_collaboration (
 id VARCHAR(36) PRIMARY KEY,
 batch_id VARCHAR(36) NOT NULL REFERENCES feishu_import_batches(id) ON DELETE RESTRICT,
 item_id VARCHAR(36) NOT NULL REFERENCES feishu_import_batch_items(id) ON DELETE RESTRICT,
 actor_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 action VARCHAR(16) NOT NULL CHECK(action IN ('note','reported','recheck')),
 contact VARCHAR(128) NOT NULL DEFAULT '',
 note VARCHAR(512) NOT NULL DEFAULT '',
 result VARCHAR(64) NOT NULL,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX feishu_collaboration_batch ON feishu_permission_collaboration(batch_id,created_at);
