ALTER TABLE tenant_api_keys ADD COLUMN IF NOT EXISTS owner_user_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE tenant_api_keys DROP CONSTRAINT IF EXISTS chk_tenant_api_keys_scope;
ALTER TABLE tenant_api_keys ADD CONSTRAINT chk_tenant_api_keys_scope CHECK (
  (scope_type = 'tenant' AND tenant_id IS NOT NULL AND owner_user_id IS NULL)
  OR (scope_type = 'platform' AND tenant_id IS NULL AND owner_user_id IS NULL AND full_access = FALSE)
  OR (scope_type = 'user_mcp' AND tenant_id IS NULL AND owner_user_id IS NOT NULL AND full_access = FALSE)
);
CREATE INDEX IF NOT EXISTS idx_tenant_api_keys_owner_user ON tenant_api_keys(owner_user_id);

CREATE TABLE IF NOT EXISTS api_key_tenant_scopes (
  id BIGSERIAL PRIMARY KEY,
  api_key_id BIGINT NOT NULL REFERENCES tenant_api_keys(id) ON DELETE CASCADE,
  tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kb_scope_mode VARCHAR(16) NOT NULL CHECK (kb_scope_mode IN ('all','selected')),
  UNIQUE(api_key_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_api_key_tenant_scopes_tenant ON api_key_tenant_scopes(tenant_id);

CREATE TABLE IF NOT EXISTS api_key_kb_scopes (
  id BIGSERIAL PRIMARY KEY,
  api_key_tenant_scope_id BIGINT NOT NULL REFERENCES api_key_tenant_scopes(id) ON DELETE CASCADE,
  api_key_id BIGINT NOT NULL REFERENCES tenant_api_keys(id) ON DELETE CASCADE,
  tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  knowledge_base_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  source_type VARCHAR(16) NOT NULL CHECK (source_type IN ('owned','shared')),
  kb_share_id VARCHAR(36) REFERENCES kb_shares(id) ON DELETE CASCADE,
  CHECK ((source_type='owned' AND kb_share_id IS NULL) OR (source_type='shared' AND kb_share_id IS NOT NULL)),
  UNIQUE(api_key_tenant_scope_id, knowledge_base_id, source_type)
);
CREATE INDEX IF NOT EXISTS idx_api_key_kb_scopes_lookup ON api_key_kb_scopes(api_key_id, tenant_id, knowledge_base_id);

CREATE OR REPLACE FUNCTION validate_api_key_tenant_scope() RETURNS trigger AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM tenant_api_keys k
    WHERE k.id = NEW.api_key_id AND k.scope_type = 'user_mcp' AND k.owner_user_id IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'tenant scope must belong to a user_mcp key';
  END IF;
  IF NEW.kb_scope_mode = 'all' AND EXISTS (
    SELECT 1 FROM api_key_kb_scopes s WHERE s.api_key_tenant_scope_id = NEW.id
  ) THEN
    RAISE EXCEPTION 'all KB scope cannot contain selected KB rows';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_validate_api_key_tenant_scope
BEFORE INSERT OR UPDATE ON api_key_tenant_scopes
FOR EACH ROW EXECUTE FUNCTION validate_api_key_tenant_scope();

CREATE OR REPLACE FUNCTION validate_api_key_kb_scope() RETURNS trigger AS $$
DECLARE parent_mode VARCHAR(16);
BEGIN
  SELECT kb_scope_mode INTO parent_mode
  FROM api_key_tenant_scopes
  WHERE id = NEW.api_key_tenant_scope_id
    AND api_key_id = NEW.api_key_id
    AND tenant_id = NEW.tenant_id;
  IF parent_mode IS DISTINCT FROM 'selected' THEN
    RAISE EXCEPTION 'KB scope must match a selected tenant scope';
  END IF;
  IF NEW.source_type = 'owned' AND NOT EXISTS (
    SELECT 1 FROM knowledge_bases kb
    WHERE kb.id = NEW.knowledge_base_id AND kb.tenant_id = NEW.tenant_id AND kb.deleted_at IS NULL
  ) THEN
    RAISE EXCEPTION 'owned KB must belong to the target tenant';
  END IF;
  IF NEW.source_type = 'shared' AND NOT EXISTS (
    SELECT 1
    FROM kb_shares ks
    JOIN organization_tenant_members otm ON otm.organization_id = ks.organization_id
    JOIN organizations o ON o.id = ks.organization_id AND o.deleted_at IS NULL
    WHERE ks.id = NEW.kb_share_id
      AND ks.knowledge_base_id = NEW.knowledge_base_id
      AND ks.deleted_at IS NULL
      AND otm.tenant_id = NEW.tenant_id
  ) THEN
    RAISE EXCEPTION 'shared KB must use an active share for the target tenant';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_validate_api_key_kb_scope
BEFORE INSERT OR UPDATE ON api_key_kb_scopes
FOR EACH ROW EXECUTE FUNCTION validate_api_key_kb_scope();
