SET LOCAL work_mem = '256MB';
SET LOCAL maintenance_work_mem = '256MB';
SET LOCAL max_parallel_maintenance_workers = 0; -- no dynamic shared memory: 64 MB /dev/shm is common

DROP INDEX content_search_idx; -- on content_metadata; frees the name
DROP INDEX idx_content_unique, idx_content_uri_unique, idx_content_parent, idx_content_file_uri,
    idx_content_roots, idx_content_roots_created;
ALTER TABLE content RENAME TO content_old;
ALTER INDEX content_pkey RENAME TO content_old_pkey;

-- The definition from 001, 003 and 011, plus the metadata. Existing constraints keep their names
-- (content_old still holds them, so they are named explicitly); the PK and FKs come after the load.
CREATE TABLE content (
    id TEXT CONSTRAINT content_id_not_null NOT NULL,
    created_at TIMESTAMPTZ CONSTRAINT content_created_at_not_null NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ CONSTRAINT content_updated_at_not_null NOT NULL DEFAULT NOW(),
    uri_part TEXT CONSTRAINT content_uri_part_not_null NOT NULL,
    uri TEXT CONSTRAINT content_uri_not_null NOT NULL,
    valid BOOLEAN CONSTRAINT content_valid_not_null NOT NULL DEFAULT TRUE,
    file_uri TEXT,
    file_mtime TIMESTAMPTZ,
    file_size INTEGER,
    cover_uri TEXT,
    type TEXT CONSTRAINT content_type_not_null NOT NULL,
    "order" INTEGER,
    order_parts REAL[] CONSTRAINT content_order_parts_not_null NOT NULL DEFAULT '{}',
    file_data JSONB CONSTRAINT content_file_data_not_null NOT NULL DEFAULT '{}',
    parent_id TEXT,
    library_id TEXT CONSTRAINT content_library_id_not_null NOT NULL,
    word_count INTEGER,
    page_count INTEGER,
    data JSONB NOT NULL DEFAULT '{}',
    data_raw JSONB NOT NULL DEFAULT '{}',
    data_version INTEGER NOT NULL DEFAULT 0,
    meta_updated_at TIMESTAMPTZ,
    release_date TEXT GENERATED ALWAYS AS (data->>'publication_date') STORED,
    rating NUMERIC GENERATED ALWAYS AS (
        CASE WHEN jsonb_typeof(data->'rating') = 'number' THEN (data->'rating')::numeric END) STORED,
    search_text TEXT GENERATED ALWAYS AS (
        coalesce(data->>'title', '') || E'\n' || coalesce((data->'alt_titles')::text, '')) STORED,
    sort_title TEXT COLLATE "C" GENERATED ALWAYS AS (public.sort_key(data->>'title')) STORED,
    CONSTRAINT content_type_check CHECK (type IN ('book', 'book_series', 'comic', 'comic_series'))
);

INSERT INTO content (id, created_at, updated_at, uri_part, uri, valid, file_uri, file_mtime, file_size,
    cover_uri, type, "order", order_parts, file_data, parent_id, library_id, word_count, page_count,
    data, data_raw, data_version, meta_updated_at)
SELECT c.id, c.created_at, c.updated_at, c.uri_part, c.uri, c.valid, c.file_uri, c.file_mtime, c.file_size,
    c.cover_uri, c.type, c."order", c.order_parts, c.file_data, c.parent_id, c.library_id, c.word_count,
    c.page_count, coalesce(m.data, '{}'), coalesce(m.data_raw, '{}'), coalesce(m.data_version, 0), m.updated_at
FROM content_old c LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri;

DROP TABLE content_old; -- its self-FK goes with it; nothing else references it
ALTER TABLE content
    ADD PRIMARY KEY (id),
    ADD FOREIGN KEY (parent_id) REFERENCES content (id),
    ADD FOREIGN KEY (library_id) REFERENCES libraries (id) ON DELETE CASCADE;

CREATE UNIQUE INDEX idx_content_unique ON content (uri_part, COALESCE(parent_id, ''), library_id);
CREATE UNIQUE INDEX idx_content_uri_unique ON content (library_id, uri);
CREATE INDEX idx_content_parent ON content (parent_id);
CREATE INDEX idx_content_file_uri ON content (file_uri COLLATE "C");
-- INCLUDE answers the user-status joins without heap visits.
CREATE INDEX idx_content_roots ON content (library_id, uri) INCLUDE (valid, id) WHERE parent_id IS NULL;
-- The grid's sorts: ascending NULLS FIRST, or these indexes backwards for descending NULLS LAST.
CREATE INDEX idx_content_roots_created ON content (library_id, valid, created_at, id)
    INCLUDE (type) WHERE parent_id IS NULL;
CREATE INDEX idx_content_roots_title ON content (library_id, valid, sort_title ASC NULLS FIRST, id)
    INCLUDE (type) WHERE parent_id IS NULL;
CREATE INDEX idx_content_roots_rating ON content (library_id, valid, rating ASC NULLS FIRST, id)
    INCLUDE (type) WHERE parent_id IS NULL;
CREATE INDEX idx_content_roots_release ON content (library_id, valid, release_date ASC NULLS FIRST, id)
    INCLUDE (type) WHERE parent_id IS NULL;
CREATE INDEX content_search_idx ON content
    USING bm25 (id, uri, library_id, (search_text::pdb.icu)) WITH (key_field = 'id');

-- Links attach to series content only; any other row was an orphan.
ALTER TABLE metadata_links ADD COLUMN content_id TEXT;
UPDATE metadata_links l SET content_id = c.id FROM content c
WHERE c.library_id = l.library_id AND c.uri = l.uri AND c.type IN ('comic_series', 'book_series');
DELETE FROM metadata_links WHERE content_id IS NULL;
ALTER TABLE metadata_links
    DROP CONSTRAINT metadata_links_pkey,
    DROP COLUMN uri,
    ALTER COLUMN content_id SET NOT NULL,
    ADD PRIMARY KEY (content_id, provider),
    ADD FOREIGN KEY (content_id) REFERENCES content (id) ON DELETE CASCADE;

DROP TABLE content_metadata;
ANALYZE content;
ANALYZE metadata_links;
