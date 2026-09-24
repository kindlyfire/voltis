ALTER TABLE users
    ALTER COLUMN password_hash DROP NOT NULL,
    ADD COLUMN email TEXT UNIQUE;

CREATE TABLE user_identities (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, issuer, subject)
);
CREATE INDEX idx_user_identities_user_id ON user_identities(user_id);

ALTER TABLE sessions
    ADD COLUMN method TEXT NOT NULL DEFAULT 'password',
    ADD COLUMN absolute_expires_at TIMESTAMPTZ;

CREATE TABLE auth_pending (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}',
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    session_token TEXT,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_auth_pending_expires_at ON auth_pending(expires_at);
