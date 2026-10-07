-- The last finished pass per kind, not a history. Counts is JSONB because it's only read back
-- whole, so a new counter needs no migration.
CREATE TABLE metadata_passes (
    kind       TEXT PRIMARY KEY CHECK (kind IN ('match', 'refresh')),
    started_at TIMESTAMPTZ NOT NULL,
    ended_at   TIMESTAMPTZ NOT NULL,
    counts     JSONB NOT NULL  -- match: linked, review, unmatched, failed, skipped
);                              -- refresh: refreshed, failed
