-- Only accounts actually created by approved JIT. Never historical ownership.
CREATE TABLE source_id_jit_registrations (
 id VARCHAR(36) PRIMARY KEY,
 issuer TEXT NOT NULL, employee_no TEXT NOT NULL, email TEXT NOT NULL,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id), policy_ref TEXT NOT NULL,
 created_at TIMESTAMP WITH TIME ZONE NOT NULL,
 UNIQUE(issuer,employee_no), UNIQUE(issuer,user_id), UNIQUE(issuer,email)
);
