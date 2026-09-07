CREATE TABLE tenant_portal_configs (
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

CREATE INDEX idx_tenant_portal_configs_listing
    ON tenant_portal_configs (status, featured DESC, display_order, tenant_id);
CREATE INDEX idx_tenant_portal_configs_category
    ON tenant_portal_configs (category) WHERE status = 'published';

CREATE TABLE tenant_portal_stages (
    tenant_id       INTEGER NOT NULL REFERENCES tenant_portal_configs(tenant_id) ON DELETE CASCADE,
    stage_key       VARCHAR(64) NOT NULL,
    display_order   INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, stage_key)
);

CREATE INDEX idx_tenant_portal_stages_stage
    ON tenant_portal_stages (stage_key, display_order, tenant_id);

CREATE TABLE tenant_access_requests (
    id                  VARCHAR(36) PRIMARY KEY,
    tenant_id           INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    applicant_user_id   VARCHAR(36) NOT NULL,
    source              VARCHAR(32) NOT NULL DEFAULT 'portal' CHECK (source = 'portal'),
    status              VARCHAR(16) NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    reason              TEXT NOT NULL,
    requested_role      VARCHAR(20) NOT NULL DEFAULT 'viewer' CHECK (requested_role = 'viewer'),
    reviewed_by         VARCHAR(36),
    reviewed_at         DATETIME,
    review_note         TEXT NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX uq_tenant_access_requests_pending
    ON tenant_access_requests (tenant_id, applicant_user_id)
    WHERE status = 'pending';
CREATE INDEX idx_tenant_access_requests_tenant_status_created
    ON tenant_access_requests (tenant_id, status, created_at DESC);
CREATE INDEX idx_tenant_access_requests_applicant_status_created
    ON tenant_access_requests (applicant_user_id, status, created_at DESC);
CREATE INDEX idx_tenant_access_requests_source_status
    ON tenant_access_requests (source, status);
