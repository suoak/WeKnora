CREATE TABLE document_access_epochs (
 knowledge_id VARCHAR(36) PRIMARY KEY,
 generation BIGINT NOT NULL CHECK (generation > 0),
 incarnation VARCHAR(32) NOT NULL CHECK (length(incarnation) = 32)
);
CREATE TABLE document_subject_grants (
 knowledge_id VARCHAR(36) NOT NULL REFERENCES knowledges(id) ON DELETE RESTRICT,
 user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 operation VARCHAR(16) NOT NULL CHECK (operation IN ('read','search','file')),
 state VARCHAR(16) NOT NULL DEFAULT 'revoked' CHECK (state IN ('approved','revoked')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 resource_version VARCHAR(64) NOT NULL DEFAULT '',
 updated_by VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 reason TEXT NOT NULL CHECK (length(reason) > 0),
 PRIMARY KEY (knowledge_id,user_id,operation),
 CHECK (state <> 'approved' OR length(resource_version) = 64)
);
INSERT INTO document_access_epochs SELECT id,1,md5(random()::text || clock_timestamp()::text || id) FROM knowledges;
-- StatementBegin keeps the migration runner from splitting PL/pgSQL bodies.
-- +migrate StatementBegin
CREATE FUNCTION knowhub_document_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE doc_id text;
BEGIN
 IF TG_OP <> 'INSERT' THEN
  IF TG_TABLE_NAME = 'knowledges' THEN doc_id := OLD.id; ELSE doc_id := OLD.knowledge_id; END IF;
  INSERT INTO document_access_epochs VALUES(doc_id,1,md5(random()::text || clock_timestamp()::text || doc_id))
   ON CONFLICT(knowledge_id) DO UPDATE SET generation=document_access_epochs.generation+1;
 END IF;
 IF TG_OP <> 'DELETE' THEN
  IF TG_TABLE_NAME = 'knowledges' THEN doc_id := NEW.id; ELSE doc_id := NEW.knowledge_id; END IF;
  INSERT INTO document_access_epochs VALUES(doc_id,1,md5(random()::text || clock_timestamp()::text || doc_id))
   ON CONFLICT(knowledge_id) DO UPDATE SET generation=document_access_epochs.generation+1;
 END IF;
 RETURN NULL;
END;
$$;
-- +migrate StatementEnd
CREATE TRIGGER document_epoch_knowledges AFTER INSERT OR UPDATE OR DELETE ON knowledges FOR EACH ROW EXECUTE FUNCTION knowhub_document_epoch();
CREATE TRIGGER document_epoch_chunks AFTER INSERT OR UPDATE OR DELETE ON chunks FOR EACH ROW EXECUTE FUNCTION knowhub_document_epoch();

CREATE FUNCTION knowhub_document_epoch_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE document_access_epochs SET generation=generation+1;
 RETURN NULL;
END;
$$;
CREATE TRIGGER document_epoch_knowledges_truncate AFTER TRUNCATE ON knowledges FOR EACH STATEMENT EXECUTE FUNCTION knowhub_document_epoch_truncate();
CREATE TRIGGER document_epoch_chunks_truncate AFTER TRUNCATE ON chunks FOR EACH STATEMENT EXECUTE FUNCTION knowhub_document_epoch_truncate();
