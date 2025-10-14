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

CREATE TABLE content (
    id TEXT PRIMARY KEY,
    content_id TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('book', 'book_series', 'comic', 'comic_series')),
    title TEXT NOT NULL,
    "order" INTEGER,
    order_parts REAL[],
    parent_id TEXT REFERENCES content(id)
);