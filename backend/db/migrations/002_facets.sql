SET LOCAL work_mem = '256MB';
SET LOCAL maintenance_work_mem = '256MB';

-- Identity of a name. In order: NFKC; guillemets and curly quotes → spaces (unaccent would turn
-- them into kept symbols or apostrophes); lower, unaccent, lower (unaccent emits uppercase:
-- Œ → OE); apostrophes deleted; NFC (unaccent can leave a composable pair that would refold
-- differently); runs of punctuation, whitespace and control characters → one space. '.', '-' and
-- '_' count as punctuation (J.M. = J. M., U.S.A. ≠ USA); # + $ < = > ^ ` | ~ are kept.
-- IMMUTABLE despite STABLE unaccent(text): the dictionary is pinned, as in public.sort_key.
-- Changing this, unaccent.rules or the Postgres Unicode version requires a rebuild (TRUNCATE
-- content_facets, then the backfill INSERT below).
CREATE FUNCTION public.facet_key(s TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
BEGIN ATOMIC
    SELECT btrim(regexp_replace(normalize(
        translate(lower(public.unaccent('public.unaccent'::regdictionary,
            lower(translate(normalize(s, NFKC), '«»‹›‘‚‛“”„‟', '           ')))), '''’', ''), NFC),
        '[]!"%&()*,./:;?@[\\_{}«·»‐-―‘-‟…⁄\s\u0001-\u001f\u007f-\u009f\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff-]+', ' ', 'g'));
END;

-- The name as written, for display: NFC, runs of whitespace and control characters to one space,
-- trimmed.
CREATE FUNCTION public.facet_label(s TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
BEGIN ATOMIC
    SELECT btrim(regexp_replace(normalize(s, NFC), '[\s\u0001-\u001f\u007f-\u009f\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+', ' ', 'g'));
END;

-- The string elements of a JSON array; nothing for any other shape.
CREATE FUNCTION public.facet_strings(a JSONB) RETURNS SETOF TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT v #>> '{}' FROM jsonb_array_elements(CASE WHEN jsonb_typeof(a) = 'array' THEN a ELSE '[]' END) v
    WHERE jsonb_typeof(v) = 'string'
$$;

-- A row's data as content_facets rows: one per (kind, key), with its distinct roles and labels.
-- Only JSON strings count: other element types, staff entries without a string name, and staff
-- roles that are neither a string nor missing/null are skipped, as are names that fold to '' and
-- keys or labels over 1000 bytes (btree index rows max out near 2704 bytes; folding can lengthen
-- the key, so both are checked). Person rows also cap role at 200 bytes. Genres: key =
-- facet_key(slug), label = the slug. $$ body so it inlines; OFFSET 0 computes facet_key and
-- facet_label once per element. array_agg(DISTINCT) sorts, so equal sets compare equal.
CREATE FUNCTION public.facet_rows(d JSONB)
RETURNS TABLE (kind TEXT, key TEXT, roles TEXT[], labels TEXT[])
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT e.kind, e.key,
        coalesce(array_agg(DISTINCT e.role) FILTER (WHERE e.role <> ''), '{}'),
        array_agg(DISTINCT e.label)
    FROM (
        SELECT 'genre', n.key, '', g
        FROM public.facet_strings(d->'genres') g,
            LATERAL (SELECT public.facet_key(g) AS key OFFSET 0) n
        WHERE n.key <> '' AND octet_length(n.key) <= 1000 AND octet_length(g) <= 1000
        UNION ALL
        SELECT 'person', n.key, n.role, n.label
        FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d->'staff') = 'array' THEN d->'staff' ELSE '[]' END) s,
            LATERAL (SELECT public.facet_key(s->>'name') AS key, public.facet_label(s->>'name') AS label,
                coalesce(s->>'role', '') AS role OFFSET 0) n
        WHERE jsonb_typeof(s) = 'object' AND jsonb_typeof(s->'name') = 'string'
          AND coalesce(jsonb_typeof(s->'role'), 'null') IN ('string', 'null')
          AND n.key <> '' AND octet_length(n.key) <= 1000 AND octet_length(n.label) <= 1000
          AND octet_length(n.role) <= 200
        UNION ALL
        SELECT f.kind, n.key, '', n.label
        FROM (VALUES ('publisher', 'publishers'), ('tag', 'tags')) f(kind, field),
            public.facet_strings(d->f.field) v,
            LATERAL (SELECT public.facet_key(v) AS key, public.facet_label(v) AS label OFFSET 0) n
        WHERE n.key <> '' AND octet_length(n.key) <= 1000 AND octet_length(n.label) <= 1000
    ) e(kind, key, role, label)
    GROUP BY e.kind, e.key
$$;

-- A name's key when facet_rows would keep it, else NULL: a non-empty key within the cap, and
-- the label within the cap (genres: the raw slug, which is their label).
CREATE FUNCTION public.facet_link(s TEXT, genre BOOLEAN) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT k FROM (SELECT public.facet_key(s) AS k OFFSET 0) x
    WHERE k <> '' AND octet_length(k) <= 1000
      AND octet_length(CASE WHEN genre THEN s ELSE public.facet_label(s) END) <= 1000
$$;

-- facet_link over each element of a JSON array; NULL for non-strings, [] for a non-array.
CREATE FUNCTION public.facet_link_list(a JSONB, genre BOOLEAN) RETURNS JSONB
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT coalesce(jsonb_agg(CASE WHEN jsonb_typeof(v) = 'string'
        THEN public.facet_link(v #>> '{}', genre) END ORDER BY i), '[]')
    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(a) = 'array' THEN a ELSE '[]' END) WITH ORDINALITY x(v, i)
$$;

-- Keys aligned index for index with data's staff, genres, tags and publishers, for the content
-- page's links. NULL where facet_rows skips the element; [] for a missing or malformed field.
CREATE FUNCTION public.facet_links(d JSONB) RETURNS JSONB
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT jsonb_build_object(
        'staff', (SELECT coalesce(jsonb_agg(CASE
                WHEN jsonb_typeof(s) = 'object' AND jsonb_typeof(s->'name') = 'string'
                 AND coalesce(jsonb_typeof(s->'role'), 'null') IN ('string', 'null')
                 AND octet_length(coalesce(s->>'role', '')) <= 200
                THEN public.facet_link(s->>'name', false) END ORDER BY i), '[]')
            FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d->'staff') = 'array' THEN d->'staff' ELSE '[]' END)
                WITH ORDINALITY x(s, i)),
        'genres', public.facet_link_list(d->'genres', true),
        'tags', public.facet_link_list(d->'tags', false),
        'publishers', public.facet_link_list(d->'publishers', false))
$$;

-- One row per valid top-level entry and value, kept in sync with content by the triggers below.
CREATE TABLE content_facets (
    content_id TEXT NOT NULL,
    library_id TEXT NOT NULL,      -- copy of content.library_id; never updated by any writer
    kind TEXT NOT NULL CHECK (kind IN ('genre', 'tag', 'person', 'publisher')),
    key TEXT NOT NULL,
    roles TEXT[] NOT NULL,         -- people only, else '{}'
    labels TEXT[] NOT NULL         -- distinct facet_label spellings on the root (genres: the slug)
);

-- Backfill; also the rebuild statement.
INSERT INTO content_facets (content_id, library_id, kind, key, roles, labels)
SELECT c.id, c.library_id, r.kind, r.key, r.roles, r.labels
FROM content c, public.facet_rows(c.data) r
WHERE c.parent_id IS NULL AND c.valid;

-- No primary key: facets_sync replaces a root's rows wholesale, and facet_rows yields one row per
-- (kind, key), so (content_id, kind, key) is unique by construction.
ALTER TABLE content_facets ADD FOREIGN KEY (content_id) REFERENCES content (id) ON DELETE CASCADE;
-- The sync's delete and the FK cascade.
CREATE INDEX idx_content_facets_content ON content_facets (content_id);
-- Counts, name sort and totals (per library by filtering library_id inside each key), the entry
-- count and the grid filter; the display-name sample reads it in order.
CREATE INDEX idx_content_facets_value ON content_facets (kind, key, library_id, content_id);

-- Replaces the content_facets rows of the given roots with those of their current data. A row that
-- is no longer a valid root gets none. The roots' content rows are locked by the calling statement,
-- so concurrent writers of one root serialize on them.
CREATE FUNCTION public.facets_sync(ids TEXT[]) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM content_facets WHERE content_id = ANY(ids);
    INSERT INTO content_facets (content_id, library_id, kind, key, roles, labels)
    SELECT c.id, c.library_id, r.kind, r.key, r.roles, r.labels
    FROM content c, public.facet_rows(c.data) r
    WHERE c.id = ANY(ids) AND c.parent_id IS NULL AND c.valid;
END $$;

CREATE FUNCTION public.facets_content_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ids TEXT[];
BEGIN
    SELECT array_agg(id) INTO ids FROM new_rows WHERE parent_id IS NULL AND valid;
    IF ids IS NOT NULL THEN PERFORM public.facets_sync(ids); END IF;
    RETURN NULL;
END $$;

-- Only the four fields facet_rows reads, parent_id and valid count as changes, so title and
-- recompute updates leave the facet rows alone. EXECUTE plans the old/new join per call: a cached
-- plan from a 1-row upsert is a nested loop that makes a later bulk UPDATE on the same connection
-- quadratic.
CREATE FUNCTION public.facets_content_update() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ids TEXT[];
BEGIN
    EXECUTE $q$
        SELECT array_agg(n.id) FROM old_rows o JOIN new_rows n ON n.id = o.id
        WHERE ((o.parent_id IS NULL AND o.valid) OR (n.parent_id IS NULL AND n.valid))
          AND ((o.data->'genres', o.data->'tags', o.data->'staff', o.data->'publishers')
                 IS DISTINCT FROM (n.data->'genres', n.data->'tags', n.data->'staff', n.data->'publishers')
               OR o.parent_id IS DISTINCT FROM n.parent_id OR o.valid IS DISTINCT FROM n.valid)
    $q$ INTO ids;
    IF ids IS NOT NULL THEN PERFORM public.facets_sync(ids); END IF;
    RETURN NULL;
END $$;

CREATE TRIGGER facets_content_insert AFTER INSERT ON content
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.facets_content_insert();
CREATE TRIGGER facets_content_update AFTER UPDATE ON content
    REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.facets_content_update();

ANALYZE content_facets;
