CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE settings_version (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    version BIGINT NOT NULL DEFAULT 0
);
INSERT INTO settings_version (id) VALUES (TRUE);

INSERT INTO settings (key, value)
SELECT 'internal.bootstrap_completed', 'true'::jsonb
WHERE EXISTS (SELECT 1 FROM users WHERE permissions @> ARRAY['ADMIN']);
