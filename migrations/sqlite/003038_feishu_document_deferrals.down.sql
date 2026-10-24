-- Never discard an active policy or pending authorization retry on downgrade.
CREATE TEMP TABLE document_deferral_down_guard (allowed INTEGER CHECK (allowed=1));
INSERT INTO document_deferral_down_guard
 SELECT CASE WHEN EXISTS (
  SELECT 1 FROM data_sources
  WHERE json_array_length(document_deferrals) > 0
   OR json_extract(last_sync_cursor, '$.connector_cursor.feishu_pending_documents_v1') IS NOT NULL
 ) THEN 0 ELSE 1 END;
DROP TABLE document_deferral_down_guard;
ALTER TABLE data_sources DROP COLUMN document_deferrals;
