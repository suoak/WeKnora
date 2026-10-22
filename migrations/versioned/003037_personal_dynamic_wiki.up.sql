ALTER TABLE tenant_api_keys ADD COLUMN personal_scope_mode VARCHAR(16) NOT NULL DEFAULT 'fixed'
    CHECK (personal_scope_mode IN ('fixed','dynamic_all'));
ALTER TABLE tenant_api_keys ADD CONSTRAINT personal_dynamic_user_only
    CHECK (personal_scope_mode='fixed' OR (scope_type='user_mcp' AND owner_user_id IS NOT NULL AND full_access=false));
CREATE TABLE wiki_mcp_provenance (
    page_id VARCHAR(36) PRIMARY KEY REFERENCES wiki_pages(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    page_sha256 VARCHAR(64) NOT NULL,
    dependency_sha256 VARCHAR(64) NOT NULL,
    source_versions TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX wiki_mcp_provenance_kb ON wiki_mcp_provenance(knowledge_base_id);
-- Generation-input fence only, not another permission authority. Every writer,
-- including raw/old writers, invalidates the old proof and advances the KB fence.
CREATE TABLE wiki_mcp_kb_epochs (
 knowledge_base_id VARCHAR(36) PRIMARY KEY,
 generation BIGINT NOT NULL CHECK(generation>0),
 incarnation VARCHAR(32) NOT NULL
);
-- Deduplicate KB IDs before generating the volatile incarnation. DISTINCT
-- over the random value would retain every page and violate the KB PK.
INSERT INTO wiki_mcp_kb_epochs
 SELECT kb.knowledge_base_id,1,md5(random()::text||clock_timestamp()::text||kb.knowledge_base_id)
 FROM (SELECT DISTINCT knowledge_base_id FROM wiki_pages) kb;
CREATE FUNCTION wiki_mcp_invalidate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN
  DELETE FROM wiki_mcp_provenance WHERE page_id=OLD.id;
  INSERT INTO wiki_mcp_kb_epochs VALUES(OLD.knowledge_base_id,1,md5(random()::text||clock_timestamp()::text))
   ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=wiki_mcp_kb_epochs.generation+1;
 END IF;
 IF TG_OP='INSERT' OR (TG_OP='UPDATE' AND NEW.knowledge_base_id<>OLD.knowledge_base_id) THEN
  INSERT INTO wiki_mcp_kb_epochs VALUES(NEW.knowledge_base_id,1,md5(random()::text||clock_timestamp()::text))
   ON CONFLICT(knowledge_base_id) DO UPDATE SET generation=wiki_mcp_kb_epochs.generation+1;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER wiki_mcp_mutation AFTER INSERT OR UPDATE OR DELETE ON wiki_pages FOR EACH ROW EXECUTE FUNCTION wiki_mcp_invalidate();
