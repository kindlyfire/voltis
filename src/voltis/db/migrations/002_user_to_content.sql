CREATE TABLE user_to_content (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id TEXT REFERENCES libraries(id) ON DELETE SET NULL,
    uri TEXT NOT NULL,
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT,
    status_updated_at TIMESTAMP,
    notes TEXT,
    rating INTEGER,
    progress JSONB NOT NULL DEFAULT '{}',
    progress_updated_at TIMESTAMP,
    UNIQUE (user_id, library_id, uri)
);
