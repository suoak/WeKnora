CREATE TABLE source_id_binding_reviews (
 id VARCHAR(36) PRIMARY KEY,
 issuer TEXT NOT NULL, employee_no TEXT NOT NULL, subject TEXT NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id), email TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','approved','revoked')),
 reviewed_by TEXT NOT NULL DEFAULT '', review_reference TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,subject), UNIQUE(issuer,user_id)
);
CREATE TABLE source_id_binding_audits (
 id VARCHAR(36) PRIMARY KEY, review_id VARCHAR(36) NOT NULL REFERENCES source_id_binding_reviews(id),
 action TEXT NOT NULL CHECK(action IN ('approve','revoke','reopen')),
 reviewed_by TEXT NOT NULL, review_reference TEXT NOT NULL, created_at TIMESTAMP NOT NULL
);
