-- Per-user keys for key-in-URL protocols such as OPDS. Stored raw so users can view them again.
CREATE TABLE app_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(trim(name)) > 0),
    key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX idx_app_keys_user_id ON app_keys(user_id);

-- Atom IDs must be globally unique.
INSERT INTO settings (key, value)
VALUES ('internal.installation_id', to_jsonb(gen_random_uuid()::text))
ON CONFLICT (key) DO NOTHING;
