CREATE TABLE source_id_local_sessions (
 id VARCHAR(36) PRIMARY KEY REFERENCES source_id_sessions(id),
 binding_id VARCHAR(36) NOT NULL,
 subject TEXT NOT NULL,
 policy_ref TEXT NOT NULL,
 admission_policy_ref TEXT NOT NULL,
 admission_valid_until TIMESTAMP NOT NULL,
 last_seen_at TIMESTAMP NOT NULL,
 idle_seconds BIGINT NOT NULL CHECK(idle_seconds > 0),
 access_seconds BIGINT NOT NULL CHECK(access_seconds > 0),
 refresh_enabled BOOLEAN NOT NULL DEFAULT FALSE
);
