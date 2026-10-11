-- Rollback requires workers drained and all incomplete cleanup ledgers resolved.
-- Refuse to discard vector-cleanup evidence; a failed DOWN is safe and retryable.
DO $$ BEGIN
    IF to_regclass('knowledge_processing_executions') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM knowledge_processing_executions WHERE execution_id <> '' AND lease_until > CURRENT_TIMESTAMP) THEN
            RAISE EXCEPTION 'processing workers must drain before downgrade';
        END IF;
    END IF;
    IF to_regclass('knowledge_processing_owned_chunks') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM knowledge_processing_owned_chunks WHERE NOT cleanup_complete) THEN
            RAISE EXCEPTION 'processing cleanup must complete before downgrade';
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS knowledge_processing_owned_chunks;
DROP TABLE IF EXISTS knowledge_processing_executions;
