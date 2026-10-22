-- Schema parity only. Atomic shared pg_search publication is PostgreSQL-only;
-- application rejects this feature on SQLite. No historical row is enrolled.
ALTER TABLE data_sources ADD COLUMN legacy_access_state TEXT NOT NULL DEFAULT 'unmanaged'
 CHECK(legacy_access_state IN ('unmanaged','active','hold','revoked'));
CREATE TABLE feishu_legacy_resources (
 id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL,
 data_source_id TEXT NOT NULL, external_id TEXT NOT NULL, knowledge_id TEXT NOT NULL UNIQUE,
 generation INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'hold'
 CHECK(state IN ('active','hold','revoked')),current_attempt TEXT NOT NULL DEFAULT '',
 active_attempt TEXT NOT NULL DEFAULT '',source_revision TEXT NOT NULL DEFAULT '',
 original_file_path TEXT NOT NULL DEFAULT '',quarantine_snapshot TEXT NOT NULL DEFAULT '{}',updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(tenant_id,knowledge_base_id,data_source_id,external_id)
);
CREATE INDEX feishu_legacy_resource_state_idx ON feishu_legacy_resources(tenant_id,state);
CREATE TABLE feishu_legacy_attempts (
 id TEXT PRIMARY KEY,resource_id TEXT NOT NULL REFERENCES feishu_legacy_resources(id),
 generation INTEGER NOT NULL,owner_nonce TEXT NOT NULL,lease_until DATETIME NOT NULL,
 status TEXT NOT NULL DEFAULT 'staging' CHECK(status IN ('staging','ready','published','obsolete')),
 revision TEXT NOT NULL,source_hash TEXT NOT NULL,expected_hash TEXT NOT NULL,
 manifest TEXT NOT NULL DEFAULT '{}',receipt TEXT NOT NULL DEFAULT '{}',created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE feishu_legacy_index_candidates (
 id INTEGER PRIMARY KEY AUTOINCREMENT,attempt_id TEXT NOT NULL REFERENCES feishu_legacy_attempts(id),
 chunk_id TEXT NOT NULL,content TEXT NOT NULL,dimension INTEGER NOT NULL CHECK(dimension>0),
 embedding BLOB NOT NULL,UNIQUE(attempt_id,chunk_id)
);
CREATE TRIGGER feishu_legacy_knowledge_insert_fence BEFORE INSERT ON knowledges
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=NEW.id)
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
CREATE TRIGGER feishu_legacy_knowledge_update_fence BEFORE UPDATE ON knowledges
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id IN (OLD.id,NEW.id))
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
CREATE TRIGGER feishu_legacy_knowledge_delete_fence BEFORE DELETE ON knowledges
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
CREATE TRIGGER feishu_legacy_chunk_insert_fence BEFORE INSERT ON chunks
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=NEW.knowledge_id)
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
CREATE TRIGGER feishu_legacy_chunk_update_fence BEFORE UPDATE ON chunks
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id IN (OLD.knowledge_id,NEW.knowledge_id))
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
CREATE TRIGGER feishu_legacy_chunk_delete_fence BEFORE DELETE ON chunks
 WHEN EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id=OLD.knowledge_id)
 BEGIN SELECT RAISE(ABORT,'Feishu atomic publication unavailable on SQLite'); END;
