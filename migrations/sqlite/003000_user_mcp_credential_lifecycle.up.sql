ALTER TABLE tenant_api_keys
    ADD COLUMN client_type TEXT NOT NULL DEFAULT 'generic';

ALTER TABLE tenant_api_keys
    ADD COLUMN token_hint TEXT NOT NULL DEFAULT '';
