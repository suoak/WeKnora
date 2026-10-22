DROP TRIGGER IF EXISTS document_epoch_knowledges_truncate ON knowledges;
DROP TRIGGER IF EXISTS document_epoch_chunks_truncate ON chunks;
DROP FUNCTION IF EXISTS knowhub_document_epoch_truncate();
DROP TRIGGER IF EXISTS document_epoch_chunks ON chunks;
DROP TRIGGER IF EXISTS document_epoch_knowledges ON knowledges;
DROP FUNCTION IF EXISTS knowhub_document_epoch();
DROP TABLE IF EXISTS document_subject_grants;
DROP TABLE IF EXISTS document_access_epochs;
