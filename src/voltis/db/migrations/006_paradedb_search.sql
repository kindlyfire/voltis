CREATE EXTENSION IF NOT EXISTS pg_search;

DROP VIEW content_metadata_merged;

CREATE MATERIALIZED VIEW content_metadata_merged AS
SELECT
    uri || '::' || library_id AS id,
    uri,
    library_id,
    jsonb_merge_agg(data ORDER BY provider ASC) AS data
FROM content_metadata
GROUP BY uri, library_id;

CREATE UNIQUE INDEX content_metadata_merged_id_idx ON content_metadata_merged (id);

CREATE INDEX content_search_idx ON content_metadata_merged
USING bm25 (id, uri, library_id, (data::pdb.icu))
WITH (key_field='id');
