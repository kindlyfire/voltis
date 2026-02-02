ALTER TABLE content_metadata ADD COLUMN remote_id TEXT;
ALTER TABLE libraries ADD COLUMN settings JSONB NOT NULL DEFAULT '{}'::JSONB;
