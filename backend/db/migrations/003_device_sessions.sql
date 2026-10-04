-- The volatile default backfills a distinct id per existing row.
ALTER TABLE sessions
    ADD COLUMN id TEXT NOT NULL DEFAULT ('s_' || replace(gen_random_uuid()::text, '-', '')),
    ADD COLUMN client_name TEXT CHECK (client_name IS NULL OR char_length(btrim(client_name)) BETWEEN 1 AND 64),
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN last_used_at TIMESTAMPTZ;
ALTER TABLE sessions ADD CONSTRAINT sessions_id_key UNIQUE (id);
