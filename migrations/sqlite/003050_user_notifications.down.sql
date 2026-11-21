DROP TABLE IF EXISTS user_notifications;

-- SQLite cannot ALTER COLUMN DEFAULT. Keep every stored opt-out unchanged.
-- The migrate driver wraps this entire script in one transaction.
CREATE TEMP TABLE portal_stages_backup AS SELECT * FROM tenant_portal_stages;
DELETE FROM tenant_portal_stages;
CREATE TABLE tenant_portal_configs_new (
    tenant_id                       INTEGER PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    status                          VARCHAR(16) NOT NULL DEFAULT 'draft'
                                    CHECK (status IN ('draft', 'published', 'archived')),
    display_name                    VARCHAR(128) NOT NULL,
    description                     TEXT NOT NULL DEFAULT '',
    category                        VARCHAR(64) NOT NULL DEFAULT ''
                                    CHECK (length(category) <= 64
                                       AND category = lower(trim(category))
                                       AND category NOT IN ('concept_market', 'concept_product', 'architecture', 'design', 'development', 'testing', 'lmt')),
    responsible_team                VARCHAR(128) NOT NULL DEFAULT '',
    contact                         VARCHAR(256) NOT NULL DEFAULT '',
    featured                        BOOLEAN NOT NULL DEFAULT 0,
    display_order                    INTEGER NOT NULL DEFAULT 0,
    allow_access_request             BOOLEAN NOT NULL DEFAULT 0,
    interaction_organization_id      VARCHAR(36) REFERENCES organizations(id) ON DELETE SET NULL,
    created_by                       VARCHAR(36) NOT NULL,
    updated_by                       VARCHAR(36) NOT NULL,
    published_at                     DATETIME,
    created_at                       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO tenant_portal_configs_new SELECT * FROM tenant_portal_configs;
DROP TABLE tenant_portal_configs;
ALTER TABLE tenant_portal_configs_new RENAME TO tenant_portal_configs;
CREATE INDEX idx_tenant_portal_configs_listing
    ON tenant_portal_configs (status, featured DESC, display_order, tenant_id);
CREATE INDEX idx_tenant_portal_configs_category
    ON tenant_portal_configs (category) WHERE status = 'published';

INSERT INTO tenant_portal_stages SELECT * FROM portal_stages_backup;
DROP TABLE portal_stages_backup;
