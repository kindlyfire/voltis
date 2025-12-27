CREATE TABLE user_to_content (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_id TEXT NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    status TEXT,
    notes TEXT,
    rating INTEGER,
    progress JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (user_id, content_id)
);