CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    status INTEGER NOT NULL,
    output JSONB NOT NULL DEFAULT '{}',
    logs TEXT,
    input JSONB NOT NULL DEFAULT '{}',
    user_id TEXT,
    library_id TEXT
);
CREATE INDEX idx_tasks_status ON tasks(status);
