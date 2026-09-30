SET LOCAL maintenance_work_mem = '256MB';
SET LOCAL max_parallel_maintenance_workers = 0; -- no dynamic shared memory: 64 MB /dev/shm is common

-- An old volume is never upgraded in place: the new image changes the glibc collation too.
DO $$ BEGIN
    IF string_to_array((SELECT extversion FROM pg_extension WHERE extname = 'pg_search'), '.')::int[]
       < '{0,25,10}' THEN
        RAISE EXCEPTION 'pg_search is older than 0.25.10: recreate the volume on the new image, or restore a dump into it';
    END IF;
END $$;

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

DROP INDEX content_search_idx;
ALTER TABLE content
    DROP COLUMN search_text,
    ADD COLUMN search_title TEXT GENERATED ALWAYS AS (
        coalesce(public.search_squash(data->>'title'), '')) STORED,
    ADD COLUMN search_title_len INT GENERATED ALWAYS AS (
        char_length(coalesce(public.search_squash(data->>'title'), ''))) STORED,
    ADD COLUMN search_alt_titles TEXT[] GENERATED ALWAYS AS (
        public.search_alt_titles(data)) STORED,
    ADD COLUMN is_root BOOLEAN GENERATED ALWAYS AS (parent_id IS NULL) STORED;

-- search_title_len and sort_title are fast fields, so a search's order pushes down into the scan;
-- is_root lets root-level searches drop item rows inside it.
CREATE INDEX content_search_idx ON content USING bm25 (
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
