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
