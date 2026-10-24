ALTER TABLE data_sources ADD COLUMN document_deferrals TEXT NOT NULL DEFAULT '[]'
 CHECK (json_valid(document_deferrals) AND json_type(document_deferrals) = 'array');
