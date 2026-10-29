-- Stop/drain derived-write workers before rollback. Original source/content guards remain.
CREATE OR REPLACE FUNCTION knowhub_feishu_legacy_write_fence() RETURNS trigger LANGUAGE plpgsql AS $$
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

DROP TABLE feishu_derived_write_grants;
