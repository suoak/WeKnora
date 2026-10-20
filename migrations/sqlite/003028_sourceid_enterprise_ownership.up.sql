CREATE TABLE source_id_account_ownership (
 id TEXT PRIMARY KEY, issuer TEXT NOT NULL, employee_no TEXT NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(id), email TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','disabled')),
 revision INTEGER NOT NULL CHECK(revision > 0),
 policy_ref TEXT NOT NULL, ownership_ref TEXT NOT NULL, source_ref TEXT NOT NULL,
 verified_at DATETIME NOT NULL, valid_until DATETIME NOT NULL,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,user_id), UNIQUE(issuer,email)
);
CREATE TABLE source_id_ownership_audits (
 id TEXT PRIMARY KEY, ownership_id TEXT NOT NULL REFERENCES source_id_account_ownership(id),
 revision INTEGER NOT NULL, actor TEXT NOT NULL, action TEXT NOT NULL, created_at DATETIME NOT NULL
);
CREATE INDEX idx_sourceid_ownership_audit ON source_id_ownership_audits(ownership_id,revision);
CREATE INDEX idx_sourceid_users_normalized_email ON users(LOWER(TRIM(email)));
