DROP INDEX IF EXISTS content_search_idx;
DROP MATERIALIZED VIEW IF EXISTS content_metadata_merged;
DROP TABLE IF EXISTS content_metadata;

CREATE TABLE content_metadata (
    uri TEXT NOT NULL,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    data JSONB NOT NULL DEFAULT '{}'::JSONB,
    data_raw JSONB NOT NULL DEFAULT '{}'::JSONB,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    id TEXT GENERATED ALWAYS AS (uri || '::' || library_id) STORED,
    PRIMARY KEY (uri, library_id)
);

CREATE UNIQUE INDEX content_metadata_id_idx ON content_metadata (id);

CREATE INDEX content_search_idx ON content_metadata
USING bm25 (id, uri, library_id, (data::pdb.icu))
WITH (key_field='id');
