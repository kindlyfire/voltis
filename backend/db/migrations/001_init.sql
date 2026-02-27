CREATE TABLE users (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    permissions TEXT[] NOT NULL DEFAULT '{}',
    preferences JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE libraries (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    scanned_at TIMESTAMP,
    sources JSONB NOT NULL DEFAULT '[]',
    settings JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    uri_part TEXT NOT NULL,
    uri TEXT NOT NULL,
    valid BOOLEAN NOT NULL DEFAULT TRUE,
    file_uri TEXT,
    file_mtime TIMESTAMP,
    file_size INTEGER,
    cover_uri TEXT,
    type TEXT NOT NULL CHECK (type IN ('book', 'book_series', 'comic', 'comic_series')),
    "order" INTEGER,
    order_parts REAL[] NOT NULL DEFAULT '{}',
    file_data JSONB NOT NULL DEFAULT '{}',
    parent_id TEXT REFERENCES content(id),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_content_unique ON content(uri_part, COALESCE(parent_id, ''), library_id);
CREATE UNIQUE INDEX idx_content_uri_unique ON content(uri, library_id);

CREATE TABLE user_to_content (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id TEXT REFERENCES libraries(id) ON DELETE SET NULL,
    uri TEXT NOT NULL,
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT,
    status_updated_at TIMESTAMP,
    notes TEXT,
    rating INTEGER,
    progress JSONB NOT NULL DEFAULT '{}',
    progress_updated_at TIMESTAMP,
    UNIQUE (user_id, library_id, uri)
);

CREATE TABLE custom_lists (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL CHECK (char_length(trim(name)) > 0),
    description TEXT,
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'private', 'unlisted')),
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_custom_lists_user_id ON custom_lists(user_id);
CREATE INDEX idx_custom_lists_visibility ON custom_lists(visibility);

CREATE TABLE custom_list_to_content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    custom_list_id TEXT NOT NULL REFERENCES custom_lists(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    uri TEXT NOT NULL,
    notes TEXT,
    "order" INTEGER,
    UNIQUE (custom_list_id, library_id, uri)
);
CREATE INDEX idx_custom_list_to_content_list_id ON custom_list_to_content(custom_list_id);
CREATE INDEX idx_custom_list_to_content_order ON custom_list_to_content(custom_list_id, "order");

-- JSONB merge aggregate for metadata
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
    data JSONB NOT NULL DEFAULT '{}',
    data_raw JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    id TEXT GENERATED ALWAYS AS (uri || '::' || library_id) STORED,
    PRIMARY KEY (uri, library_id)
);
CREATE UNIQUE INDEX content_metadata_id_idx ON content_metadata (id);

CREATE INDEX content_search_idx ON content_metadata
USING bm25 (id, uri, library_id, (data::pdb.icu))
WITH (key_field='id');

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    status INTEGER NOT NULL,
    input JSONB NOT NULL DEFAULT '{}',
    output JSONB NOT NULL DEFAULT '{}',
    logs TEXT,
    user_id TEXT,
    library_id TEXT
);
