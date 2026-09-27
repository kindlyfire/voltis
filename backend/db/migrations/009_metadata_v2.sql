CREATE TABLE provider_entries (
    provider    TEXT NOT NULL,
    external_id TEXT NOT NULL,
    raw         JSONB NOT NULL,                  -- last snapshot that decoded
    merged_into TEXT,                            -- tombstone: redirect, never un-merged
    canonical_id TEXT NOT NULL,                  -- where merged_into leads; external_id if not merged
    deleted     BOOLEAN NOT NULL DEFAULT FALSE,  -- confirmed upstream deletion; raw kept
    fetched_at  TIMESTAMPTZ NOT NULL,            -- observation time, captured before the request
    refresh_at  TIMESTAMPTZ NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT,
    PRIMARY KEY (provider, external_id)
);
CREATE INDEX idx_provider_entries_refresh ON provider_entries (refresh_at) WHERE merged_into IS NULL;
CREATE INDEX idx_provider_entries_canonical ON provider_entries (provider, canonical_id);
CREATE INDEX idx_provider_entries_merged ON provider_entries (provider, merged_into) WHERE merged_into IS NOT NULL;
-- Provider health: failing refreshes and the latest snapshot.
CREATE INDEX idx_provider_entries_failing ON provider_entries (provider) WHERE attempts > 0 AND merged_into IS NULL AND fetched_at > '-infinity';
CREATE INDEX idx_provider_entries_fetched ON provider_entries (provider, fetched_at);

CREATE TABLE metadata_links (
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    uri         TEXT NOT NULL,
    provider    TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('review', 'unmatched', 'linked', 'ignored')),
    external_id TEXT,
    origin      TEXT CHECK (origin IN ('auto', 'manual')),
    candidates  JSONB NOT NULL DEFAULT '[]',     -- review only
    rejected    TEXT[] NOT NULL DEFAULT '{}',
    retry_at    TIMESTAMPTZ,                     -- due for automatic matching; NULL = not due
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT,                            -- set = last attempt failed
    rev         BIGINT NOT NULL DEFAULT 1,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (library_id, uri, provider),
    FOREIGN KEY (provider, external_id) REFERENCES provider_entries (provider, external_id),
    CHECK ((state = 'linked') = (external_id IS NOT NULL)),
    CHECK ((state = 'linked') = (origin IS NOT NULL))
);
CREATE INDEX idx_metadata_links_entry ON metadata_links (provider, external_id)
    WHERE external_id IS NOT NULL;
CREATE INDEX idx_metadata_links_rejected ON metadata_links USING gin (rejected);
CREATE INDEX idx_metadata_links_candidates ON metadata_links USING gin (candidates jsonb_path_ops);
CREATE INDEX idx_metadata_links_state ON metadata_links (state, updated_at);
CREATE INDEX idx_metadata_links_failed ON metadata_links (provider) WHERE last_error IS NOT NULL;

-- Metadata v1 is dropped, not converted. No stored mtime matches a file's, so the next scan of
-- each library reads every file again, which rebuilds the file and series layers.
TRUNCATE content_metadata;
UPDATE content SET file_mtime = NULL WHERE type IN ('comic', 'book');

ALTER TABLE content_metadata
    ADD COLUMN data_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN release_date TEXT GENERATED ALWAYS AS (data->>'publication_date') STORED,
    ADD COLUMN rating NUMERIC GENERATED ALWAYS AS (
        CASE WHEN jsonb_typeof(data->'rating') = 'number' THEN (data->'rating')::numeric END) STORED,
    ADD COLUMN search_text TEXT GENERATED ALWAYS AS (
        coalesce(data->>'title', '') || E'\n' || coalesce((data->'alt_titles')::text, '')) STORED;

DROP INDEX content_search_idx;
CREATE INDEX content_search_idx ON content_metadata
    USING bm25 (id, uri, library_id, (search_text::pdb.icu)) WITH (key_field = 'id');

DROP AGGREGATE jsonb_merge_agg(JSONB);
DROP FUNCTION jsonb_merge_pair(JSONB, JSONB);
