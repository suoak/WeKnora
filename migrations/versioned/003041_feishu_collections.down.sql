-- +migrate StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM feishu_collections) OR EXISTS(SELECT 1 FROM feishu_collection_pages) THEN
  RAISE EXCEPTION 'Refusing to discard Feishu collection discovery and provenance state';
 END IF;
END $$;
-- +migrate StatementEnd
DROP TABLE feishu_collection_pages;
DROP TABLE feishu_collections;
