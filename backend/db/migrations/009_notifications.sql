-- A per-user feed. An item is unread when alerted_at is set and differs from seen_at.
CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,              -- 'scan' | 'new_content' | 'metadata_review'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),   -- feed order
    alerted_at TIMESTAMPTZ,          -- last time it should count as unread; null: never
    seen_at TIMESTAMPTZ,             -- the alerted_at the user last saw
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    content_id TEXT REFERENCES content(id) ON DELETE CASCADE,
    group_key TEXT,                  -- 'new_content:<series id>', 'metadata_review'
    data JSONB NOT NULL DEFAULT '{}' -- what the item renders: counts, outcome, titles
);
-- Scanned backwards for ORDER BY updated_at DESC, id DESC.
CREATE INDEX idx_notifications_feed ON notifications (user_id, updated_at, id);
CREATE UNIQUE INDEX idx_notifications_group ON notifications (user_id, group_key) WHERE group_key IS NOT NULL;
CREATE INDEX idx_notifications_task ON notifications (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_notifications_content ON notifications (content_id) WHERE content_id IS NOT NULL;
CREATE INDEX idx_notifications_sweep ON notifications (updated_at);

-- Reader lookup at scan commit.
CREATE INDEX idx_utc_library_uri ON user_to_content (library_id, uri);
