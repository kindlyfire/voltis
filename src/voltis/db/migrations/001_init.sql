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
    user_id TEXT NOT NULL REFERENCES users(id)
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE data_sources (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    path TEXT NOT NULL,
    type TEXT NOT NULL,
    scanned_at TIMESTAMP
);

CREATE TABLE content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    uri_part TEXT NOT NULL,
    title TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('book', 'book_series', 'comic', 'comic_series')),
    "order" INTEGER,
    order_parts REAL[],
    parent_id TEXT REFERENCES content(id),
    datasource_id TEXT NOT NULL REFERENCES data_sources(id)
);

CREATE UNIQUE INDEX idx_content_unique ON content(uri_part, COALESCE(parent_id, ''), datasource_id);