-- Private source-local candidates. Historical resources are not enrolled by migration.
ALTER TABLE data_sources ADD COLUMN legacy_access_state text NOT NULL DEFAULT 'unmanaged'
 CHECK(legacy_access_state IN ('unmanaged','active','hold','revoked'));
CREATE TABLE feishu_legacy_resources (
 id varchar(64) PRIMARY KEY, tenant_id bigint NOT NULL, knowledge_base_id varchar(36) NOT NULL,
 data_source_id varchar(36) NOT NULL, external_id text NOT NULL, knowledge_id varchar(36) NOT NULL UNIQUE,
 generation bigint NOT NULL DEFAULT 0, state text NOT NULL DEFAULT 'hold'
 CHECK (state IN ('active','hold','revoked')), current_attempt varchar(36) NOT NULL DEFAULT '',
 active_attempt varchar(36) NOT NULL DEFAULT '', source_revision text NOT NULL DEFAULT '',
 original_file_path text NOT NULL DEFAULT '',quarantine_snapshot jsonb NOT NULL DEFAULT '{}',updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id,knowledge_base_id,data_source_id,external_id)
);
CREATE INDEX feishu_legacy_resource_state_idx ON feishu_legacy_resources(tenant_id,state);
CREATE TABLE feishu_legacy_attempts (
 id varchar(36) PRIMARY KEY, resource_id varchar(64) NOT NULL REFERENCES feishu_legacy_resources(id),
 generation bigint NOT NULL, owner_nonce varchar(64) NOT NULL, lease_until timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'staging' CHECK(status IN ('staging','ready','published','obsolete')),
 revision text NOT NULL, source_hash varchar(64) NOT NULL, expected_hash text NOT NULL,
 manifest jsonb NOT NULL DEFAULT '{}', receipt jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
-- No candidate is stored in public chunks or embeddings. This relation also
-- exercises real vector and pg_search index materialization before publication.
DO $$ BEGIN
 IF to_regclass('public.embeddings') IS NOT NULL THEN
  IF to_regclass('public.embeddings_embedding_idx_1024') IS NULL THEN
   CREATE INDEX embeddings_embedding_idx_1024 ON embeddings
    USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops) WHERE dimension=1024;
   COMMENT ON INDEX embeddings_embedding_idx_1024 IS 'knowhub:migration3036:owned';
  END IF;
  CREATE TABLE feishu_legacy_index_candidates (
   id bigserial PRIMARY KEY, attempt_id varchar(36) NOT NULL REFERENCES feishu_legacy_attempts(id),
   chunk_id varchar(36) NOT NULL, content text NOT NULL, dimension integer NOT NULL CHECK(dimension>0),
   embedding halfvec NOT NULL, UNIQUE(attempt_id,chunk_id)
  );
  CREATE INDEX feishu_legacy_candidate_bm25 ON feishu_legacy_index_candidates
   USING bm25 (id,attempt_id,content) WITH (key_field='id',text_fields='{"content":{"tokenizer":{"type":"chinese_lindera"}}}');
 END IF;
END $$;

-- Checks and side effects share the resource row lock. Old binaries/tasks have
-- no valid publication capability and cannot mutate an enrolled stable KID.
CREATE FUNCTION knowhub_feishu_legacy_write_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE kid text; oldkid text; rid text; identity text; identities text[] := '{}';
 r feishu_legacy_resources; a feishu_legacy_attempts;
BEGIN
 IF TG_TABLE_NAME='knowledges' THEN
  IF TG_OP<>'DELETE' THEN kid:=NEW.id; END IF;
  IF TG_OP<>'INSERT' THEN oldkid:=OLD.id; END IF;
  IF TG_OP<>'DELETE' AND NEW.channel IN ('feishu','feishu_drive','lark','lark_drive')
   AND COALESCE(NEW.metadata->>'datasource_id','')<>'' AND COALESCE(NEW.metadata->>'external_id','')<>'' THEN
   identities:=array_append(identities,NEW.tenant_id::text||':'||NEW.knowledge_base_id||':'||
    (NEW.metadata->>'datasource_id')||':'||(NEW.metadata->>'external_id'));
  END IF;
  IF TG_OP<>'INSERT' AND OLD.channel IN ('feishu','feishu_drive','lark','lark_drive')
   AND COALESCE(OLD.metadata->>'datasource_id','')<>'' AND COALESCE(OLD.metadata->>'external_id','')<>'' THEN
   identities:=array_append(identities,OLD.tenant_id::text||':'||OLD.knowledge_base_id||':'||
    (OLD.metadata->>'datasource_id')||':'||(OLD.metadata->>'external_id'));
  END IF;
 ELSE
  IF TG_OP<>'DELETE' THEN kid:=NEW.knowledge_id; END IF;
  IF TG_OP<>'INSERT' THEN oldkid:=OLD.knowledge_id; END IF;
  SELECT COALESCE(array_agg(DISTINCT k.tenant_id::text||':'||k.knowledge_base_id||':'||
   (k.metadata->>'datasource_id')||':'||(k.metadata->>'external_id')),'{}') INTO identities
   FROM knowledges k WHERE k.id IN (kid,oldkid) AND k.channel IN ('feishu','feishu_drive','lark','lark_drive')
   AND COALESCE(k.metadata->>'datasource_id','')<>'' AND COALESCE(k.metadata->>'external_id','')<>'';
 END IF;
 -- Drain earlier uncommitted writes before enrolling a legacy identity.
 FOR identity IN SELECT DISTINCT unnest(identities) ORDER BY 1 LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(identity,0));
 END LOOP;
 IF cardinality(identities)>0 OR EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_id IN (kid,oldkid)) THEN
  FOR identity IN SELECT DISTINCT x FROM unnest(ARRAY[kid,oldkid]) x WHERE x IS NOT NULL ORDER BY 1 LOOP
   PERFORM pg_advisory_xact_lock(hashtextextended('feishu-kid:'||identity,0));
  END LOOP;
 END IF;
 FOR rid IN SELECT id FROM feishu_legacy_resources WHERE knowledge_id IN (kid,oldkid)
  OR tenant_id::text||':'||knowledge_base_id||':'||data_source_id||':'||external_id=ANY(identities) ORDER BY id LOOP
  SELECT * INTO r FROM feishu_legacy_resources WHERE id=rid FOR UPDATE;
  SELECT * INTO a FROM feishu_legacy_attempts WHERE id=r.current_attempt;
  IF a.id IS NULL OR a.generation<>r.generation OR a.status NOT IN ('ready','staging')
    OR a.lease_until<=clock_timestamp()
    OR current_setting('knowhub.feishu_attempt',true) IS DISTINCT FROM a.id
    OR current_setting('knowhub.feishu_nonce',true) IS DISTINCT FROM a.owner_nonce THEN
   RAISE EXCEPTION 'stale or unmanaged Feishu source write' USING ERRCODE='40001';
  END IF;
 END LOOP;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER feishu_legacy_knowledge_fence BEFORE INSERT OR UPDATE OR DELETE ON knowledges
 FOR EACH ROW EXECUTE FUNCTION knowhub_feishu_legacy_write_fence();
CREATE TRIGGER feishu_legacy_chunk_fence BEFORE INSERT OR UPDATE OR DELETE ON chunks
 FOR EACH ROW EXECUTE FUNCTION knowhub_feishu_legacy_write_fence();
DO $$ BEGIN IF to_regclass('public.embeddings') IS NOT NULL THEN
 CREATE TRIGGER feishu_legacy_embedding_fence BEFORE INSERT OR UPDATE OR DELETE ON embeddings
 FOR EACH ROW EXECUTE FUNCTION knowhub_feishu_legacy_write_fence();
END IF; END $$;
