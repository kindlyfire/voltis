UPDATE libraries SET sources = '[]' WHERE jsonb_typeof(sources) IS DISTINCT FROM 'array';

-- auto_match becomes per provider. MangaBaka was the only provider when it was a bool.
UPDATE libraries SET settings = jsonb_set(settings, '{auto_match}',
    CASE WHEN settings->'auto_match' = 'true' THEN '{"mangabaka": true}'::jsonb ELSE '{}'::jsonb END)
WHERE jsonb_typeof(settings->'auto_match') = 'boolean';

-- A pending scan whose input no longer decodes would stay pending forever; scans don't read it.
UPDATE tasks SET input = input #- '{settings,auto_match}'
WHERE name = 'scan_library' AND status = 0 AND jsonb_typeof(input->'settings'->'auto_match') = 'boolean';

-- Automatic matching (linking/automatch.go): leaves by source prefix, a library's series in URI
-- order, and links in the order they fall due.
CREATE INDEX idx_content_file_uri ON content (file_uri COLLATE "C");
CREATE INDEX idx_content_roots ON content (library_id, uri) WHERE parent_id IS NULL;
CREATE INDEX idx_metadata_links_retry ON metadata_links (library_id, provider, retry_at) WHERE retry_at IS NOT NULL;
