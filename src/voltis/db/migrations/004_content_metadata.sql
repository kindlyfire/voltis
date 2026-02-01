-- Aggregate for merging JSONB objects by priority. Later values (higher
-- provider number) overwrite earlier ones.
CREATE FUNCTION jsonb_merge_pair(a JSONB, b JSONB) RETURNS JSONB AS $$
    SELECT COALESCE(a, '{}'::jsonb) || COALESCE(b, '{}'::jsonb);
$$ LANGUAGE SQL IMMUTABLE;

CREATE AGGREGATE jsonb_merge_agg(JSONB) (
    SFUNC = jsonb_merge_pair,
    STYPE = JSONB,
    INITCOND = '{}'
);

CREATE TABLE content_metadata (
    uri TEXT NOT NULL,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    provider INTEGER NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::JSONB,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (uri, library_id, provider)
);

CREATE VIEW content_metadata_merged AS
SELECT uri, library_id, jsonb_merge_agg(data ORDER BY provider ASC) AS data
FROM content_metadata
GROUP BY uri, library_id;

ALTER TABLE content ADD COLUMN file_data JSONB NOT NULL DEFAULT '{}'::JSONB;
ALTER TABLE content DROP COLUMN meta;