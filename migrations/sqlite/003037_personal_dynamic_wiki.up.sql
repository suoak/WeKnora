ALTER TABLE tenant_api_keys ADD COLUMN personal_scope_mode VARCHAR(16) NOT NULL DEFAULT 'fixed'
 CHECK (personal_scope_mode IN ('fixed','dynamic_all') AND (personal_scope_mode='fixed' OR (scope_type='user_mcp' AND owner_user_id IS NOT NULL AND full_access=0)));
CREATE TABLE wiki_mcp_provenance (
 page_id VARCHAR(36) PRIMARY KEY REFERENCES wiki_pages(id) ON DELETE CASCADE,
 tenant_id BIGINT NOT NULL,
 knowledge_base_id VARCHAR(36) NOT NULL,
 page_sha256 VARCHAR(64) NOT NULL,
 dependency_sha256 VARCHAR(64) NOT NULL,
 source_versions TEXT NOT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX wiki_mcp_provenance_kb ON wiki_mcp_provenance(knowledge_base_id);
CREATE TABLE wiki_mcp_kb_epochs (
 knowledge_base_id VARCHAR(36) PRIMARY KEY,
 generation BIGINT NOT NULL CHECK(generation>0),
 incarnation VARCHAR(32) NOT NULL
);
-- Deduplicate KB IDs before evaluating the volatile incarnation expression.
INSERT INTO wiki_mcp_kb_epochs
 SELECT knowledge_base_id,1,lower(hex(randomblob(16)))
 FROM (SELECT DISTINCT knowledge_base_id FROM wiki_pages);
CREATE TRIGGER wiki_mcp_insert AFTER INSERT ON wiki_pages BEGIN
 INSERT INTO wiki_mcp_kb_epochs VALUES(NEW.knowledge_base_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER wiki_mcp_update AFTER UPDATE ON wiki_pages BEGIN
 DELETE FROM wiki_mcp_provenance WHERE page_id=OLD.id;
 INSERT INTO wiki_mcp_kb_epochs VALUES(OLD.knowledge_base_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=generation+1;
 INSERT INTO wiki_mcp_kb_epochs SELECT NEW.knowledge_base_id,1,lower(hex(randomblob(16))) WHERE NEW.knowledge_base_id<>OLD.knowledge_base_id ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER wiki_mcp_delete AFTER DELETE ON wiki_pages BEGIN
 DELETE FROM wiki_mcp_provenance WHERE page_id=OLD.id;
 INSERT INTO wiki_mcp_kb_epochs VALUES(OLD.knowledge_base_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=generation+1;
END;
