-- pg_search needs vector.
CREATE EXTENSION IF NOT EXISTS pg_search CASCADE;
-- An older paradedb image's template1 carries an older pg_search, which IF NOT EXISTS keeps.
DO $$ BEGIN
    IF string_to_array((SELECT extversion FROM pg_extension WHERE extname = 'pg_search'), '.')::int[]
       < '{0,25,10}' THEN
        RAISE EXCEPTION 'pg_search is older than 0.25.10: use the paradedb 0.25.10+ image with a fresh volume';
    END IF;
END $$;

CREATE EXTENSION IF NOT EXISTS unaccent;

-- unaccent(text) is STABLE (it looks up the dictionary by search_path); the pinned dictionary
-- makes this safe to mark IMMUTABLE. Changing unaccent.rules needs a rewrite of content.sort_title.
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

-- NFC-normalizes, collapses Unicode whitespace to one space and trims, so exact-title keys ignore
-- spacing and decomposed accents (ascii_folding keeps combining marks; \s misses U+0085 and
-- U+00A0). Applied to stored titles, alt titles and every query before tokenizing.
CREATE FUNCTION public.search_squash(s TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
BEGIN ATOMIC
    SELECT btrim(regexp_replace(normalize(s, NFC),
        '[\s\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+', ' ', 'g'));
END;

-- Alt titles, squashed, in order, without byte-identical duplicates or copies of the title.
-- No case or accent folding: ICU keeps e.g. Greek with and without tonos distinct.
CREATE FUNCTION public.search_alt_titles(d JSONB) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE
BEGIN ATOMIC
    SELECT coalesce(array_agg(v ORDER BY ord), '{}')
    FROM (
        SELECT public.search_squash(a.v) AS v, min(a.ord) AS ord
        FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(d->'alt_titles') = 'array'
            THEN d->'alt_titles' ELSE '[]' END) WITH ORDINALITY AS a(v, ord)
        GROUP BY 1
    ) x
    WHERE v <> '' AND v IS DISTINCT FROM public.search_squash(d->>'title');
END;

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    username TEXT NOT NULL UNIQUE,
    email TEXT,
    password_hash TEXT,
    permissions TEXT[] NOT NULL DEFAULT '{}',
    preferences JSONB NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method TEXT NOT NULL DEFAULT 'password',
    expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ
);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);

CREATE TABLE user_identities (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, issuer, subject)
);
CREATE INDEX idx_user_identities_user_id ON user_identities (user_id);

CREATE TABLE auth_pending (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    session_token TEXT,
    data JSONB NOT NULL DEFAULT '{}',
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_auth_pending_expires_at ON auth_pending (expires_at);

-- Per-user keys for key-in-URL protocols such as OPDS. Stored raw so users can view them again.
CREATE TABLE app_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(trim(name)) > 0),
    key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX idx_app_keys_user_id ON app_keys (user_id);

CREATE TABLE libraries (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    scanned_at TIMESTAMPTZ,
    sources JSONB NOT NULL DEFAULT '[]',
    settings JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    parent_id TEXT REFERENCES content(id),
    uri_part TEXT NOT NULL,
    uri TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('book', 'book_series', 'comic', 'comic_series')),
    valid BOOLEAN NOT NULL DEFAULT TRUE,
    "order" INTEGER,
    order_parts REAL[] NOT NULL DEFAULT '{}',
    file_uri TEXT,
    file_mtime TIMESTAMPTZ,
    file_size INTEGER,
    cover_uri TEXT,
    file_data JSONB NOT NULL DEFAULT '{}',
    word_count INTEGER,
    page_count INTEGER,
    data JSONB NOT NULL DEFAULT '{}',
    data_raw JSONB NOT NULL DEFAULT '{}',
    data_version INTEGER NOT NULL DEFAULT 0,
    meta_updated_at TIMESTAMPTZ,
    release_date TEXT GENERATED ALWAYS AS (data->>'publication_date') STORED,
    rating NUMERIC GENERATED ALWAYS AS (
        CASE WHEN jsonb_typeof(data->'rating') = 'number' THEN (data->'rating')::numeric END) STORED,
    sort_title TEXT COLLATE "C" GENERATED ALWAYS AS (public.sort_key(data->>'title')) STORED,
    search_title TEXT GENERATED ALWAYS AS (coalesce(public.search_squash(data->>'title'), '')) STORED,
    search_title_len INTEGER GENERATED ALWAYS AS (
        char_length(coalesce(public.search_squash(data->>'title'), ''))) STORED,
    search_alt_titles TEXT[] GENERATED ALWAYS AS (public.search_alt_titles(data)) STORED,
    is_root BOOLEAN GENERATED ALWAYS AS (parent_id IS NULL) STORED
);
CREATE UNIQUE INDEX idx_content_unique ON content (uri_part, COALESCE(parent_id, ''), library_id);
CREATE UNIQUE INDEX idx_content_uri_unique ON content (library_id, uri);
CREATE INDEX idx_content_parent ON content (parent_id);
-- Automatic matching (linking/automatch.go): leaves by source prefix.
CREATE INDEX idx_content_file_uri ON content (file_uri COLLATE "C");
-- Automatic matching (linking/automatch.go): a library's series in URI order. INCLUDE answers the
-- user-status joins without heap visits.
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
-- search_title_len and sort_title are fast fields, so a search's order pushes down into the scan;
-- is_root lets root-level searches drop item rows inside it.
CREATE INDEX idx_content_search ON content USING bm25 (
    id,
    (uri::pdb.literal),
    (library_id::pdb.literal),
    (search_title::pdb.icu('alias=title_words', 'ascii_folding=true')),
    (search_alt_titles::pdb.icu('alias=alt_words', 'ascii_folding=true')),
    (search_title::pdb.literal_normalized('alias=title_exact', 'ascii_folding=true')),
    (search_alt_titles::pdb.literal_normalized('alias=alt_exact', 'ascii_folding=true')),
    search_title_len,
    (sort_title::pdb.literal),
    is_root
) WITH (key_field = 'id');

CREATE TABLE user_to_content (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id TEXT REFERENCES libraries(id) ON DELETE SET NULL,
    uri TEXT NOT NULL,
    starred BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT,
    status_updated_at TIMESTAMPTZ,
    notes TEXT,
    rating INTEGER,
    progress JSONB NOT NULL DEFAULT '{}',
    progress_updated_at TIMESTAMPTZ,
    revision TEXT,
    last_read_at TIMESTAMPTZ,
    UNIQUE (user_id, library_id, uri)
);
CREATE INDEX idx_utc_user_last_read ON user_to_content (user_id, last_read_at)
    WHERE last_read_at IS NOT NULL;

CREATE TABLE custom_lists (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL CHECK (char_length(trim(name)) > 0),
    description TEXT,
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'private', 'unlisted')),
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_custom_lists_user_id ON custom_lists (user_id);
CREATE INDEX idx_custom_lists_visibility ON custom_lists (visibility);

CREATE TABLE custom_list_to_content (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    custom_list_id TEXT NOT NULL REFERENCES custom_lists(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    uri TEXT NOT NULL,
    notes TEXT,
    "order" INTEGER,
    UNIQUE (custom_list_id, library_id, uri)
);
CREATE INDEX idx_custom_list_to_content_order ON custom_list_to_content (custom_list_id, "order");

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    name TEXT NOT NULL,
    status INTEGER NOT NULL,
    output JSONB NOT NULL DEFAULT '{}',
    logs TEXT,
    input JSONB NOT NULL DEFAULT '{}',
    user_id TEXT,
    library_id TEXT
);
CREATE INDEX idx_tasks_status ON tasks (status);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE settings_version (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    version BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE provider_entries (
    provider     TEXT NOT NULL,
    external_id  TEXT NOT NULL,
    raw          JSONB NOT NULL,                  -- last snapshot that decoded
    merged_into  TEXT,                            -- tombstone: redirect, never un-merged
    canonical_id TEXT NOT NULL,                   -- where merged_into leads; external_id if not merged
    deleted      BOOLEAN NOT NULL DEFAULT FALSE,  -- confirmed upstream deletion; raw kept
    fetched_at   TIMESTAMPTZ NOT NULL,            -- observation time, captured before the request
    refresh_at   TIMESTAMPTZ NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    PRIMARY KEY (provider, external_id)
);
CREATE INDEX idx_provider_entries_refresh ON provider_entries (refresh_at) WHERE merged_into IS NULL;
CREATE INDEX idx_provider_entries_canonical ON provider_entries (provider, canonical_id);
CREATE INDEX idx_provider_entries_merged ON provider_entries (provider, merged_into) WHERE merged_into IS NOT NULL;
-- Provider health: failing refreshes and the latest snapshot.
CREATE INDEX idx_provider_entries_failing ON provider_entries (provider) WHERE attempts > 0 AND merged_into IS NULL AND fetched_at > '-infinity';
CREATE INDEX idx_provider_entries_fetched ON provider_entries (provider, fetched_at);

CREATE TABLE metadata_links (
    content_id  TEXT NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL,
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
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
    PRIMARY KEY (content_id, provider),
    FOREIGN KEY (provider, external_id) REFERENCES provider_entries (provider, external_id),
    CONSTRAINT metadata_links_linked_external_id_check CHECK ((state = 'linked') = (external_id IS NOT NULL)),
    CONSTRAINT metadata_links_linked_origin_check CHECK ((state = 'linked') = (origin IS NOT NULL))
);
CREATE INDEX idx_metadata_links_entry ON metadata_links (provider, external_id)
    WHERE external_id IS NOT NULL;
CREATE INDEX idx_metadata_links_rejected ON metadata_links USING gin (rejected);
CREATE INDEX idx_metadata_links_candidates ON metadata_links USING gin (candidates jsonb_path_ops);
CREATE INDEX idx_metadata_links_state ON metadata_links (state, updated_at);
CREATE INDEX idx_metadata_links_failed ON metadata_links (provider) WHERE last_error IS NOT NULL;
-- Automatic matching (linking/automatch.go): links in the order they fall due.
CREATE INDEX idx_metadata_links_retry ON metadata_links (library_id, provider, retry_at) WHERE retry_at IS NOT NULL;

-- The query functions have string bodies: BEGIN ATOMIC would record dependencies on paradedb.*
-- and pdb.*, which a pg_search upgrade script that recreates them would then fail on.

-- One token's clause: the best of exact and final-token prefix, on the title and, unless
-- title_only, the alt titles, and of a typo on the title only (alt typos cost most of a query at
-- scale).
CREATE FUNCTION public.title_token_query(tok TEXT, last BOOLEAN, title_only BOOLEAN)
RETURNS paradedb.searchqueryinput
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
AS $$
    SELECT paradedb.disjunction_max(ARRAY[
        paradedb.const_score(4, paradedb.term('title_words', tok)),
        CASE WHEN NOT title_only THEN paradedb.const_score(3, paradedb.term('alt_words', tok))
            ELSE paradedb.empty() END,
        CASE WHEN last THEN paradedb.const_score(2, paradedb.fuzzy_term('title_words', tok,
            distance => 0, prefix => true)) ELSE paradedb.empty() END,
        CASE WHEN last AND NOT title_only THEN paradedb.const_score(1.5, paradedb.fuzzy_term(
            'alt_words', tok, distance => 0, prefix => true)) ELSE paradedb.empty() END,
        CASE WHEN typo THEN paradedb.const_score(1, paradedb.fuzzy_term('title_words', tok,
            distance => 1, transposition_cost_one => true, prefix => last)) ELSE paradedb.empty() END
    ], tie_breaker => 0)
    -- Latin letters only, since ascii_folding leaves some (ɑ) and combining marks (İ becomes i and
    -- U+0307) unfolded; the marks don't count toward the length. Digits and other scripts get no
    -- typo.
    FROM (SELECT tok ~ '^[a-z''\u00df-\u00f6\u00f8-\u02af\u1e00-\u1eff\u0300-\u036f]+$'
        AND char_length(regexp_replace(tok, '[\u0300-\u036f]', '', 'g')) >= 4 AS typo) t
$$;

-- The AND of qs, scored as qs[1]. pg_search 0.25.10 and 0.25.11 intersections (boolean must,
-- minimum_should_match) drop matches when a clause is a union over prefix or fuzzy terms
-- (tantivy#3103); excluding each other clause's complement doesn't.
CREATE FUNCTION public.search_and(qs paradedb.searchqueryinput[])
RETURNS paradedb.searchqueryinput
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
AS $$
    SELECT CASE WHEN cardinality(qs) = 1 THEN qs[1] ELSE paradedb.boolean(must => qs[1:1],
        must_not => ARRAY(
            SELECT paradedb.boolean(must => ARRAY[paradedb.all()], must_not => ARRAY[q])
            FROM unnest(qs[2:]) q)) END
$$;

-- At least all but one of qs (two or more), by halves: a row missing at most one clause misses
-- none in one half. The tree stays O(n log n); leaving each clause out in turn is O(n²).
CREATE FUNCTION public.search_miss1(qs paradedb.searchqueryinput[])
RETURNS paradedb.searchqueryinput
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
AS $$
DECLARE
    h INT := cardinality(qs) / 2;
    l paradedb.searchqueryinput[] := qs[:h];
    r paradedb.searchqueryinput[] := qs[h + 1:];
BEGIN
    IF cardinality(qs) = 2 THEN RETURN paradedb.boolean(should => qs); END IF;
    RETURN paradedb.boolean(should => ARRAY[
        public.search_and(l || public.search_miss1(r)),
        -- A one-clause half may miss it entirely.
        CASE WHEN h = 1 THEN public.search_and(r)
            ELSE public.search_and(r || public.search_miss1(l)) END
    ]);
END
$$;

-- The search predicate of every surface; paradedb.empty() when the input has no tokens. Every
-- required token must match (one may miss from 4 on), and stopwords only add score. Tiers are
-- multiples of b, above any token sum: an exact whole title or alt 16b, a title starting with the
-- query 8b, an alt starting with it 4b, every required token in the primary title 2b, a complete
-- match b when a miss is allowed. Each tier exceeds all below it combined, and requires every
-- required token, so tiers never add rows.
CREATE FUNCTION public.title_query(search TEXT)
RETURNS paradedb.searchqueryinput
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
DECLARE
    -- At most 3 characters each, so an optional clause is exact-only.
    stop CONSTANT TEXT[] := '{a,an,and,at,by,for,in,of,on,or,the,to,de,ga,mo,ni,no,wa,wo}';
    squashed TEXT := public.search_squash(search);
    k TEXT; n INT; b INT;
    req paradedb.searchqueryinput[]; opt paradedb.searchqueryinput[];
    title_req paradedb.searchqueryinput[];
    every paradedb.searchqueryinput; matched paradedb.searchqueryinput;
    complete paradedb.searchqueryinput := paradedb.empty();
    all_req paradedb.searchqueryinput[] := '{}'; -- score-neutral guard for the whole-title tiers
BEGIN
    WITH raw AS (
        -- The index's own typmods: pg_search registers a new one by an insert, which fails
        -- when a parallel worker makes the first call.
        SELECT t, o
        FROM unnest(squashed::pdb.icu('alias=title_words', 'ascii_folding=true')::text[])
            WITH ORDINALITY AS u(t, o)
    ), toks AS (
        -- Deduped; prefix only if the token occurs solely at the end; stopwords optional unless
        -- every token is one.
        SELECT t, min(o) = (SELECT max(o) FROM raw) AS last,
            t = ANY(stop) AND NOT bool_and(t = ANY(stop)) OVER () AS optional
        FROM raw GROUP BY t
    )
    SELECT count(*),
        array_agg(public.title_token_query(t, last, false) ORDER BY t) FILTER (WHERE NOT optional),
        array_agg(public.title_token_query(t, false, false) ORDER BY t) FILTER (WHERE optional),
        array_agg(public.title_token_query(t, last, true) ORDER BY t) FILTER (WHERE NOT optional)
    INTO n, req, opt, title_req FROM toks;
    IF n = 0 THEN RETURN paradedb.empty(); END IF;

    k := (squashed::pdb.literal_normalized('alias=title_exact', 'ascii_folding=true')::text[])[1];
    b := 4 * n + 1;
    every := public.search_and(req);
    IF cardinality(req) >= 4 THEN
        matched := public.search_miss1(req);
        complete := paradedb.const_score(b, every);
        -- ICU segments a query and a title differently, so a title can start with the query
        -- without holding its tokens.
        all_req := ARRAY[paradedb.const_score(0, every)];
    ELSE
        matched := every;
    END IF;
    -- matched only filters; the token clauses score as should.
    RETURN paradedb.boolean(must => ARRAY[paradedb.const_score(0, matched)],
        should => req || coalesce(opt, '{}') || ARRAY[
        complete,
        paradedb.const_score(2 * b, public.search_and(title_req)),
        public.search_and(ARRAY[paradedb.disjunction_max(ARRAY[
            paradedb.const_score(16 * b, paradedb.disjunction_max(ARRAY[
                paradedb.term('title_exact', k), paradedb.term('alt_exact', k)])),
            paradedb.const_score(8 * b,
                paradedb.fuzzy_term('title_exact', k, distance => 0, prefix => true)),
            paradedb.const_score(4 * b,
                paradedb.fuzzy_term('alt_exact', k, distance => 0, prefix => true))
        ])] || all_req)
    ]);
END
$$;

INSERT INTO settings_version (id) VALUES (TRUE);

-- Atom IDs must be globally unique.
INSERT INTO settings (key, value) VALUES ('internal.installation_id', to_jsonb(gen_random_uuid()::text));
