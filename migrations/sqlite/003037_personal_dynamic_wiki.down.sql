CREATE TEMP TABLE personal_dynamic_down_guard (allowed INTEGER CHECK (allowed=1));
INSERT INTO personal_dynamic_down_guard SELECT CASE WHEN EXISTS(SELECT 1 FROM tenant_api_keys WHERE personal_scope_mode='dynamic_all') THEN 0 ELSE 1 END;
DROP TABLE personal_dynamic_down_guard;
DROP TRIGGER wiki_mcp_insert;
DROP TRIGGER wiki_mcp_update;
DROP TRIGGER wiki_mcp_delete;
DROP TABLE wiki_mcp_kb_epochs;
DROP TABLE wiki_mcp_provenance;
ALTER TABLE tenant_api_keys DROP COLUMN personal_scope_mode;
