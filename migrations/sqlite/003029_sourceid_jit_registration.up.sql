CREATE TABLE source_id_jit_registrations (
 id TEXT PRIMARY KEY, issuer TEXT NOT NULL, employee_no TEXT NOT NULL,
 email TEXT NOT NULL, user_id TEXT NOT NULL REFERENCES users(id),
 policy_ref TEXT NOT NULL, created_at DATETIME NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,user_id), UNIQUE(issuer,email)
);
