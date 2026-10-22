-- Application rollback retains schema. Down is only for empty isolated fixtures.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM feishu_legacy_resources) OR EXISTS(SELECT 1 FROM data_sources WHERE legacy_access_state<>'unmanaged') THEN
 RAISE EXCEPTION 'managed Feishu resources exist: retain schema and roll back application with suspension enabled';
END IF; END $$;
-- Remove only an index created by this migration; retain a pre-existing V1 or
-- operator-managed index with the same canonical name.
DO $$ BEGIN
 IF obj_description(to_regclass('public.embeddings_embedding_idx_1024'),'pg_class')='knowhub:migration3036:owned' THEN
  DROP INDEX embeddings_embedding_idx_1024;
 END IF;
END $$;
DROP TRIGGER IF EXISTS feishu_legacy_embedding_fence ON embeddings;
DROP TRIGGER IF EXISTS feishu_legacy_chunk_fence ON chunks;
DROP TRIGGER IF EXISTS feishu_legacy_knowledge_fence ON knowledges;
DROP FUNCTION knowhub_feishu_legacy_write_fence();
DROP TABLE IF EXISTS feishu_legacy_index_candidates;
DROP TABLE feishu_legacy_attempts;
DROP TABLE feishu_legacy_resources;
ALTER TABLE data_sources DROP COLUMN legacy_access_state;
