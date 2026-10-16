-- Additive trusted ownership evidence. Does not change users or privileges.
CREATE TABLE source_id_account_ownership (
 id VARCHAR(36) PRIMARY KEY,
 issuer TEXT NOT NULL, employee_no TEXT NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id), email TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 revision BIGINT NOT NULL CHECK(revision > 0),
 policy_ref TEXT NOT NULL, ownership_ref TEXT NOT NULL, source_ref TEXT NOT NULL,
 verified_at TIMESTAMP WITH TIME ZONE NOT NULL,
 valid_until TIMESTAMP WITH TIME ZONE NOT NULL,
 created_at TIMESTAMP WITH TIME ZONE NOT NULL, updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,user_id), UNIQUE(issuer,email)
);
CREATE TABLE source_id_ownership_audits (
 id VARCHAR(36) PRIMARY KEY,
 ownership_id VARCHAR(36) NOT NULL REFERENCES source_id_account_ownership(id),
 revision BIGINT NOT NULL, actor TEXT NOT NULL, action TEXT NOT NULL,
 created_at TIMESTAMP WITH TIME ZONE NOT NULL
);
CREATE INDEX idx_sourceid_ownership_audit ON source_id_ownership_audits(ownership_id,revision);
-- Nonunique: duplicate normalized legacy emails must be detected and rejected,
-- not made invisible or arbitrarily selected during bulk/first association.
CREATE INDEX idx_sourceid_users_normalized_email ON users(LOWER(TRIM(email)));
