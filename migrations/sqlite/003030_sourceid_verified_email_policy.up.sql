-- Additive policy metadata; never rewrites users, roles, ACL or ownership.
CREATE TABLE source_id_link_decisions (
 id VARCHAR(36) PRIMARY KEY,
 review_id VARCHAR(36) NOT NULL REFERENCES source_id_binding_reviews(id),
 link_mode TEXT NOT NULL CHECK (link_mode = 'verified_email_policy'),
 policy_ref TEXT NOT NULL CHECK (length(trim(policy_ref)) > 0),
 binding_policy_ref TEXT NOT NULL CHECK (length(trim(binding_policy_ref)) > 0),
 decision TEXT NOT NULL CHECK (decision IN ('first_email_match','existing_binding','jit')),
 created_at DATETIME NOT NULL,
 UNIQUE(review_id, link_mode, policy_ref, binding_policy_ref)
);
CREATE TABLE source_id_local_link_policies (
 id VARCHAR(36) PRIMARY KEY REFERENCES source_id_local_sessions(id) ON DELETE CASCADE,
 decision_id VARCHAR(36) NOT NULL REFERENCES source_id_link_decisions(id),
 link_mode TEXT NOT NULL CHECK (link_mode = 'verified_email_policy'),
 policy_ref TEXT NOT NULL,
 binding_policy_ref TEXT NOT NULL
);
CREATE TABLE source_id_link_blocks (
 id VARCHAR(36) PRIMARY KEY,
 issuer TEXT NOT NULL,
 user_id TEXT NOT NULL DEFAULT '',
 employee_no TEXT NOT NULL DEFAULT '',
 subject TEXT NOT NULL DEFAULT '',
 email TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL CHECK (reason IN ('email_reassigned','ownership_conflict','identity_disabled','security_hold')),
 actor TEXT NOT NULL,
 reference TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 CHECK (user_id <> '' OR employee_no <> '' OR subject <> '' OR email <> '')
);
CREATE INDEX source_id_link_blocks_user ON source_id_link_blocks(issuer,user_id);
CREATE INDEX source_id_link_blocks_employee ON source_id_link_blocks(issuer,employee_no);
CREATE INDEX source_id_link_blocks_subject ON source_id_link_blocks(issuer,subject);
CREATE INDEX source_id_link_blocks_email ON source_id_link_blocks(issuer,email);
-- Historical exceptions must match the same canonical mailbox as users.
CREATE INDEX source_id_reviews_normalized_email ON source_id_binding_reviews(issuer,LOWER(TRIM(email)));
CREATE INDEX source_id_ownership_normalized_email ON source_id_account_ownership(issuer,LOWER(TRIM(email)));
CREATE INDEX source_id_jit_normalized_email ON source_id_jit_registrations(issuer,LOWER(TRIM(email)));
CREATE INDEX source_id_blocks_normalized_email ON source_id_link_blocks(issuer,LOWER(TRIM(email)));
