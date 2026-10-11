-- Additive only. Do not activate fenced workers until all generation writers use
-- the knowledge-row transaction lock. Drain legacy workers before activation.
CREATE TABLE IF NOT EXISTS knowledge_processing_executions (
    tenant_id BIGINT NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    generation BIGINT NOT NULL CHECK (generation > 0),
    execution_id VARCHAR(36) NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, knowledge_id)
);
CREATE TABLE IF NOT EXISTS knowledge_processing_owned_chunks (
    tenant_id BIGINT NOT NULL,
    knowledge_id VARCHAR(36) NOT NULL,
    generation BIGINT NOT NULL,
    execution_id VARCHAR(36) NOT NULL,
    chunk_id VARCHAR(36) NOT NULL,
    cleanup_complete BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (tenant_id, knowledge_id, generation, chunk_id),
    UNIQUE (tenant_id, chunk_id)
);
