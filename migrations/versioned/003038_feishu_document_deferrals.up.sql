ALTER TABLE data_sources ADD COLUMN IF NOT EXISTS document_deferrals JSONB NOT NULL DEFAULT '[]'::jsonb;
