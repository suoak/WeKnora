-- Additive, default-deny M2 policies. No backfill or changes to Portal, members or keys.
CREATE TABLE enterprise_space_policies (
 tenant_id INTEGER PRIMARY KEY REFERENCES tenants(id) ON DELETE RESTRICT,
 state VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (state IN ('pending_review','approved','revoked')),
 policy VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (policy IN ('pending_review','inherit','restricted','mcp_blocked','deny')),
 content_class VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (content_class IN ('ordinary','mixed','unknown')),
 source_acl_state VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (source_acl_state IN ('local','verified','unknown','revoked')),
 inherit_new_ordinary BOOLEAN NOT NULL DEFAULT FALSE,
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 proposed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_at TIMESTAMP WITH TIME ZONE,
 updated_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
 reason VARCHAR(512) NOT NULL DEFAULT 'awaiting_review' CHECK (length(trim(reason)) > 0),
 resource_version VARCHAR(64) NOT NULL DEFAULT '',
 CHECK (state <> 'approved' OR (proposed_by IS NOT NULL AND reviewed_by IS NOT NULL AND proposed_by <> reviewed_by AND reviewed_at IS NOT NULL AND length(resource_version) = 64))
);
CREATE INDEX idx_enterprise_space_policies_state ON enterprise_space_policies(state, policy);
CREATE TABLE kb_publication_policies (
 knowledge_base_id VARCHAR(36) PRIMARY KEY REFERENCES knowledge_bases(id) ON DELETE RESTRICT,
 state VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (state IN ('pending_review','approved','revoked')),
 policy VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (policy IN ('pending_review','inherit','restricted','mcp_blocked','deny')),
 content_class VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (content_class IN ('ordinary','mixed','unknown')),
 source_acl_state VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (source_acl_state IN ('local','verified','unknown','revoked')),
 inherit_new_ordinary BOOLEAN NOT NULL DEFAULT FALSE,
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 proposed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_at TIMESTAMP WITH TIME ZONE,
 updated_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
 reason VARCHAR(512) NOT NULL DEFAULT 'awaiting_review' CHECK (length(trim(reason)) > 0),
 resource_version VARCHAR(64) NOT NULL DEFAULT '',
 CHECK (state <> 'approved' OR (proposed_by IS NOT NULL AND reviewed_by IS NOT NULL AND proposed_by <> reviewed_by AND reviewed_at IS NOT NULL AND length(resource_version) = 64))
);
CREATE INDEX idx_kb_publication_policies_state ON kb_publication_policies(state, policy);
CREATE TABLE knowledge_access_policies (
 knowledge_id VARCHAR(36) PRIMARY KEY REFERENCES knowledges(id) ON DELETE RESTRICT,
 state VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (state IN ('pending_review','approved','revoked')),
 policy VARCHAR(16) NOT NULL DEFAULT 'pending_review' CHECK (policy IN ('pending_review','inherit','restricted','mcp_blocked','deny')),
 content_class VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (content_class IN ('ordinary','mixed','unknown')),
 source_acl_state VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (source_acl_state IN ('local','verified','unknown','revoked')),
 inherit_new_ordinary BOOLEAN NOT NULL DEFAULT FALSE,
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 proposed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_by VARCHAR(36) REFERENCES users(id) ON DELETE RESTRICT,
 reviewed_at TIMESTAMP WITH TIME ZONE,
 updated_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
 reason VARCHAR(512) NOT NULL DEFAULT 'awaiting_review' CHECK (length(trim(reason)) > 0),
 resource_version VARCHAR(64) NOT NULL DEFAULT '',
 CHECK (state <> 'approved' OR (proposed_by IS NOT NULL AND reviewed_by IS NOT NULL AND proposed_by <> reviewed_by AND reviewed_at IS NOT NULL AND length(resource_version) = 64))
);
CREATE INDEX idx_knowledge_access_policies_state ON knowledge_access_policies(state, policy);

