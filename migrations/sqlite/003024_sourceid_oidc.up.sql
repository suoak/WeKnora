CREATE TABLE external_identities (
 id VARCHAR(36) PRIMARY KEY, provider TEXT NOT NULL, issuer TEXT NOT NULL, subject TEXT NOT NULL,
 employee_no TEXT NOT NULL, user_id VARCHAR(36) NOT NULL REFERENCES users(id),
 created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL, last_login_at TIMESTAMP NOT NULL,
 UNIQUE(provider,issuer,subject), UNIQUE(provider,issuer,employee_no), UNIQUE(provider,issuer,user_id)
);
CREATE TABLE source_id_enrollments (
 id VARCHAR(36) PRIMARY KEY, issuer TEXT NOT NULL, employee_no TEXT NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id), reviewed_by TEXT NOT NULL,
 review_reference TEXT NOT NULL, created_at TIMESTAMP NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,user_id),
 CHECK(length(reviewed_by)>0), CHECK(length(review_reference)>0)
);
CREATE TABLE source_id_sessions (
 id VARCHAR(36) PRIMARY KEY, issuer TEXT NOT NULL, session_index_hash VARCHAR(64) NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id), revoked BOOLEAN NOT NULL DEFAULT FALSE,
 expires_at TIMESTAMP NOT NULL, created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_sourceid_sessions_index ON source_id_sessions(issuer,session_index_hash);
CREATE TABLE source_id_logouts (key VARCHAR(64) PRIMARY KEY, created_at TIMESTAMP NOT NULL);
CREATE TABLE oidc_transactions (
 key VARCHAR(64) PRIMARY KEY, nonce TEXT NOT NULL DEFAULT '', verifier TEXT NOT NULL DEFAULT '',
 result TEXT NOT NULL DEFAULT '', expires_at TIMESTAMP NOT NULL, consumed BOOLEAN NOT NULL DEFAULT FALSE
);
ALTER TABLE auth_tokens ADD COLUMN session_id VARCHAR(36) NOT NULL DEFAULT '';
CREATE INDEX idx_auth_tokens_session_id ON auth_tokens(session_id);
