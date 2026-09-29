CREATE EXTENSION IF NOT EXISTS unaccent;

-- unaccent(text) is STABLE (it looks up the dictionary by search_path); the pinned dictionary
-- makes this safe to mark IMMUTABLE. Changing unaccent.rules needs a rewrite of the column.
-- Returns the bucket char ('a'-'z' or '#') followed by the normalized key.
CREATE FUNCTION public.sort_key(title TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
BEGIN ATOMIC
    SELECT CASE WHEN k ~ '^[a-z]' THEN left(k, 1) ELSE '#' END || k
    FROM (
        SELECT coalesce(string_agg(
            CASE WHEN m[1] ~ '^[0-9]' AND length(m[1]) < 10 THEN lpad(m[1], 10, '0') ELSE m[1] END,
            '' ORDER BY n), '') AS k
        FROM regexp_matches(
            regexp_replace(lower(public.unaccent('public.unaccent'::regdictionary, title)),
                           '^[^[:alnum:]]+', ''),
            '[0-9]+|[^0-9]+', 'g') WITH ORDINALITY AS t(m, n)
    ) s;
END;

ALTER TABLE content_metadata
    ADD COLUMN sort_title TEXT COLLATE "C"
        GENERATED ALWAYS AS (public.sort_key(data->>'title')) STORED;

CREATE INDEX idx_content_roots_created ON content (library_id, created_at, id)
    WHERE parent_id IS NULL;
