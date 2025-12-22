CREATE TABLE users (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    permissions TEXT[] NOT NULL DEFAULT '{}'
);

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE libraries (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    scanned_at TIMESTAMP,
    sources JSONB NOT NULL DEFAULT '[]'::JSONB
);

CREATE TABLE content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    uri_part TEXT NOT NULL,
    title TEXT NOT NULL,
    valid BOOLEAN NOT NULL DEFAULT TRUE,
    file_uri TEXT,
    file_mtime TIMESTAMP,
    file_size INTEGER,
    cover_uri TEXT,
    type TEXT NOT NULL CHECK (type IN ('book', 'book_series', 'comic', 'comic_series')),
    "order" INTEGER,
    order_parts REAL[] NOT NULL DEFAULT '{}'::REAL[],
    meta JSONB NOT NULL DEFAULT '{}'::JSONB,
    parent_id TEXT REFERENCES content(id),
    library_id TEXT NOT NULL REFERENCES libraries(id)
);

CREATE UNIQUE INDEX idx_content_unique ON content(uri_part, COALESCE(parent_id, ''), library_id);