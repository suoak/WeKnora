DROP TRIGGER IF EXISTS trg_validate_api_key_kb_scope ON api_key_kb_scopes;
DROP FUNCTION IF EXISTS validate_api_key_kb_scope();
DROP TRIGGER IF EXISTS trg_validate_api_key_tenant_scope ON api_key_tenant_scopes;
DROP FUNCTION IF EXISTS validate_api_key_tenant_scope();
DROP TABLE IF EXISTS api_key_kb_scopes;
DROP TABLE IF EXISTS api_key_tenant_scopes;
DELETE FROM tenant_api_keys WHERE scope_type = 'user_mcp';
DROP INDEX IF EXISTS idx_tenant_api_keys_owner_user;
ALTER TABLE tenant_api_keys DROP CONSTRAINT IF EXISTS chk_tenant_api_keys_scope;
ALTER TABLE tenant_api_keys DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE tenant_api_keys ADD CONSTRAINT chk_tenant_api_keys_scope CHECK (
  (scope_type = 'tenant' AND tenant_id IS NOT NULL)
  OR (scope_type = 'platform' AND tenant_id IS NULL AND full_access = FALSE)
);
