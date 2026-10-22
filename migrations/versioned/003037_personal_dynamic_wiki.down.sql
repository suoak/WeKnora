-- Deliberate safety gate: never reinterpret dynamic credentials as old keys.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM tenant_api_keys WHERE personal_scope_mode='dynamic_all') THEN
  RAISE EXCEPTION 'dynamic personal keys exist; retain schema and hardened APP';
 END IF;
END $$;
DROP TRIGGER wiki_mcp_mutation ON wiki_pages;
DROP FUNCTION wiki_mcp_invalidate();
DROP TABLE wiki_mcp_kb_epochs;
DROP TABLE wiki_mcp_provenance;
ALTER TABLE tenant_api_keys DROP CONSTRAINT personal_dynamic_user_only;
ALTER TABLE tenant_api_keys DROP COLUMN personal_scope_mode;
