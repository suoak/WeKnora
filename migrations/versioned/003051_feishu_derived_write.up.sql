-- Forward-only opt-in: old workers still use the original publication fence.
-- Grants are consumed within their issuing transaction; successful commits
-- leave no reusable permission or nonce. No production source state is changed.
CREATE TABLE feishu_derived_write_grants (
 transaction_id bigint NOT NULL, knowledge_id varchar(36) NOT NULL,
 resource_id varchar(64) NOT NULL REFERENCES feishu_legacy_resources(id),
 generation bigint NOT NULL, attempt_id varchar(36) NOT NULL,
 epoch bigint NOT NULL, incarnation varchar(32) NOT NULL,
 row_digest varchar(32) NOT NULL, patch jsonb NOT NULL,
 PRIMARY KEY(transaction_id,knowledge_id),
 CHECK(jsonb_typeof(patch)='object')
);
-- Permit folder-only classification while retaining source publication fences.
CREATE OR REPLACE FUNCTION knowhub_feishu_legacy_write_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE kid text; oldkid text; rid text; identity text; identities text[] := '{}';
 r feishu_legacy_resources; a feishu_legacy_attempts; g feishu_derived_write_grants;
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
 -- Derived grants never authorize INSERT/DELETE, chunks, embeddings, source
 -- identity or content fields. An old worker without a ticket keeps the exact
 -- original publication fence below.
 IF TG_TABLE_NAME='knowledges' AND TG_OP='UPDATE' THEN
  SELECT * INTO g FROM feishu_derived_write_grants
   WHERE transaction_id=txid_current() AND knowledge_id=OLD.id;
  IF FOUND THEN
   SELECT * INTO r FROM feishu_legacy_resources WHERE id=g.resource_id FOR UPDATE;
   SELECT * INTO a FROM feishu_legacy_attempts WHERE id=r.current_attempt;
   IF (SELECT count(*) FROM feishu_legacy_resources q WHERE q.knowledge_id=OLD.id
      OR q.tenant_id::text||':'||q.knowledge_base_id||':'||q.data_source_id||':'||q.external_id=ANY(identities))<>1
    OR r.id IS NULL OR a.id IS NULL OR r.knowledge_id IS DISTINCT FROM OLD.id OR r.state<>'active'
    OR r.tenant_id<>OLD.tenant_id OR r.knowledge_base_id<>OLD.knowledge_base_id
    OR r.generation<>g.generation OR r.current_attempt<>g.attempt_id
    OR r.active_attempt<>g.attempt_id OR a.status<>'published'
    OR a.resource_id<>r.id OR a.generation<>r.generation
    OR NOT EXISTS(SELECT 1 FROM data_sources d JOIN tenants t ON t.id=d.tenant_id
      JOIN knowledge_bases k ON k.id=d.knowledge_base_id AND k.tenant_id=d.tenant_id
      WHERE d.id=r.data_source_id AND d.tenant_id=r.tenant_id AND d.knowledge_base_id=r.knowledge_base_id
      AND d.deleted_at IS NULL AND d.legacy_access_state='active' AND d.status IN ('active','error')
      AND d.type IN ('feishu','feishu_drive','lark','lark_drive')
      AND t.status='active' AND t.deleted_at IS NULL AND k.deleted_at IS NULL)
    OR OLD.deleted_at IS NOT NULL OR OLD.parse_status<>'completed'
    OR NOT EXISTS(SELECT 1 FROM document_access_epochs e WHERE e.knowledge_id=OLD.id
      AND e.generation=g.epoch AND e.incarnation=g.incarnation)
    OR md5(to_jsonb(OLD)::text)<>g.row_digest
    OR EXISTS(SELECT 1 FROM jsonb_object_keys(g.patch) key
      WHERE key NOT IN ('description','profile','summary_status','metadata','updated_at'))
    OR (to_jsonb(NEW)-ARRAY['description','profile','summary_status','metadata','updated_at'])
      IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['description','profile','summary_status','metadata','updated_at'])
    OR (to_jsonb(NEW)-'updated_at') IS DISTINCT FROM
       ((to_jsonb(OLD)||g.patch)-'updated_at')
    OR (COALESCE(NEW.metadata,'{}'::jsonb)-'derived') IS DISTINCT FROM
       (COALESCE(OLD.metadata,'{}'::jsonb)-'derived') THEN
     RAISE EXCEPTION 'Feishu derived write rejected: version or authority changed' USING ERRCODE='P0001';
   END IF;
   DELETE FROM feishu_derived_write_grants WHERE transaction_id=g.transaction_id AND knowledge_id=g.knowledge_id;
   RETURN NEW;
  END IF;
 END IF;

 FOR rid IN SELECT id FROM feishu_legacy_resources WHERE knowledge_id IN (kid,oldkid)
  OR tenant_id::text||':'||knowledge_base_id||':'||data_source_id||':'||external_id=ANY(identities) ORDER BY id LOOP
  SELECT * INTO r FROM feishu_legacy_resources WHERE id=rid FOR UPDATE;
  -- Local deletion has its own durable capability. It allows removal only;
  -- no old publisher can restore content or write a new version with it.
  IF r.state='deleted' AND r.quarantine_snapshot->>'deleted_by_user'='true' THEN
   IF TG_OP='DELETE' THEN
    CONTINUE;
   ELSIF TG_OP='UPDATE' AND TG_TABLE_NAME='knowledges'
     AND (to_jsonb(NEW)->>'parse_status'='deleting' OR to_jsonb(NEW)->>'deleted_at' IS NOT NULL)
     AND (to_jsonb(NEW)-ARRAY['parse_status','updated_at','deleted_at']) =
         (to_jsonb(OLD)-ARRAY['parse_status','updated_at','deleted_at']) THEN
    CONTINUE;
   ELSIF TG_OP='UPDATE' AND TG_TABLE_NAME='chunks' AND to_jsonb(NEW)->>'deleted_at' IS NOT NULL
     AND (to_jsonb(NEW)-ARRAY['updated_at','deleted_at']) =
         (to_jsonb(OLD)-ARRAY['updated_at','deleted_at']) THEN
    CONTINUE;
   ELSE
    RAISE EXCEPTION 'deleted Feishu document cannot be republished' USING ERRCODE='40001';
   END IF;
  END IF;
  -- Classification does not publish source content. Compare the complete row
  -- so no identity, content, permission, transfer or lifecycle field can change.
  IF TG_OP='UPDATE' AND TG_TABLE_NAME='knowledges' AND r.state<>'deleted'
    AND to_jsonb(OLD)->>'deleted_at' IS NULL
    AND to_jsonb(OLD)->>'parse_status' IS DISTINCT FROM 'deleting'
    AND (to_jsonb(NEW)-ARRAY['folder_path','updated_at']) =
        (to_jsonb(OLD)-ARRAY['folder_path','updated_at']) THEN
   CONTINUE;
  END IF;
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
