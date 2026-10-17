CREATE TABLE scrobble_accounts (
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,                 -- 'mangabaka'
    token       TEXT NOT NULL,                 -- PAT, never returned by the API
    remote_id   TEXT NOT NULL,                 -- account id; a change resets sent state
    remote_name TEXT,
    private_new BOOLEAN NOT NULL DEFAULT FALSE, -- create entries as private
    rejected_at TIMESTAMPTZ,                   -- set on 401/403: sending paused
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, provider)
);

-- One row per (user, series) once anything happened: pending, sent, held, failed.
CREATE TABLE scrobbles (
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider       TEXT NOT NULL,
    content_id     TEXT NOT NULL REFERENCES content(id) ON DELETE CASCADE,  -- the series
    held           BOOLEAN NOT NULL DEFAULT FALSE,
    pending_since  TIMESTAMPTZ,                -- first unsent change; NULL = nothing pending
    changed_at     TIMESTAMPTZ,                -- last change: the version a send acknowledges against
    due_at         TIMESTAMPTZ,                -- LEAST(changed_at + 5m, pending_since + 15m), or now
    force          BOOLEAN NOT NULL DEFAULT FALSE, -- Send anyway
    sent_at        TIMESTAMPTZ,
    error          TEXT,                       -- set = failed
    attempts       INTEGER NOT NULL DEFAULT 0,
    conflicts      INTEGER NOT NULL DEFAULT 0, -- consecutive entry conflicts; 3 = failed
    PRIMARY KEY (user_id, provider, content_id)
);
CREATE INDEX idx_scrobbles_due ON scrobbles (due_at) WHERE due_at IS NOT NULL;
-- The cascade from content deletes during scans.
CREATE INDEX idx_scrobbles_content ON scrobbles (content_id);
