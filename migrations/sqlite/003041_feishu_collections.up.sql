-- Discovery state only; document source cursors and publisher state are untouched.
CREATE TABLE feishu_collections (
 id VARCHAR(36) PRIMARY KEY,
 owner_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
 kb_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE RESTRICT,
 auth_mode VARCHAR(32) NOT NULL CHECK(auth_mode IN ('user_oauth','application_grant')),
 connection_id VARCHAR(64) NOT NULL,
 config_version VARCHAR(64) NOT NULL,
 kind VARCHAR(16) NOT NULL CHECK(kind IN ('drive','wiki')),
 root_id VARCHAR(256) NOT NULL,
 space_id VARCHAR(256) NOT NULL DEFAULT '',
 state VARCHAR(16) NOT NULL CHECK(state IN ('active','paused','hold','revoked')),
 frontier TEXT NOT NULL,
 cycle INTEGER NOT NULL DEFAULT 1 CHECK(cycle>0),
 pages INTEGER NOT NULL DEFAULT 0,
 discovered INTEGER NOT NULL DEFAULT 0,
 truncated BOOLEAN NOT NULL DEFAULT false,
 error_category VARCHAR(64) NOT NULL DEFAULT '',
 lease_owner VARCHAR(36) NOT NULL DEFAULT '',
 lease_until TIMESTAMP,
 next_run_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(owner_id,kb_id,auth_mode,connection_id,kind,root_id,space_id)
);
CREATE INDEX feishu_collection_due ON feishu_collections(state,next_run_at,lease_until);
CREATE TABLE feishu_collection_pages (
 id VARCHAR(36) PRIMARY KEY,
 collection_id VARCHAR(36) NOT NULL REFERENCES feishu_collections(id) ON DELETE RESTRICT,
 cycle INTEGER NOT NULL,
 ordinal INTEGER NOT NULL,
 batch_id VARCHAR(36) REFERENCES feishu_import_batches(id) ON DELETE RESTRICT,
 provenance TEXT NOT NULL,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(collection_id,cycle,ordinal)
);
