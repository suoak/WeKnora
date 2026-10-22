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
INSERT INTO document_access_epochs SELECT id,1,lower(hex(randomblob(16))) FROM knowledges;
CREATE TRIGGER document_epoch_knowledges_insert AFTER INSERT ON knowledges BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (NEW.id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER document_epoch_knowledges_update AFTER UPDATE ON knowledges BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (OLD.id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (NEW.id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER document_epoch_knowledges_delete AFTER DELETE ON knowledges BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (OLD.id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER document_epoch_chunks_insert AFTER INSERT ON chunks BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (NEW.knowledge_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER document_epoch_chunks_update AFTER UPDATE ON chunks BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (OLD.knowledge_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (NEW.knowledge_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
CREATE TRIGGER document_epoch_chunks_delete AFTER DELETE ON chunks BEGIN
INSERT INTO document_access_epochs(knowledge_id,generation,incarnation) VALUES (OLD.knowledge_id,1,lower(hex(randomblob(16)))) ON CONFLICT(knowledge_id) DO UPDATE SET generation=generation+1;
END;
