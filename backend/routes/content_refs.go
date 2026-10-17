package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/scrobble"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ContentRefRoutes struct {
	pool      *pgxpool.Pool
	scrobbles *scrobble.Service
}

func (cr *ContentRefRoutes) Register(g *echo.Group) {
	g.GET("/refs/:library_id", cr.listLibraryURIs)
	g.GET("/broken-refs", cr.brokenRefsSummary)
	g.GET("/broken-refs/:library_id", cr.listBrokenRefs)
	g.POST("/broken-refs/:library_id/preview", cr.previewBrokenRefs)
	g.POST("/broken-refs/:library_id", cr.saveBrokenRefs)
}

type libraryURIsResponse struct {
	ContentURIs []string `json:"content_uris"`
}

func (cr *ContentRefRoutes) listLibraryURIs(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	contentURIs, err := db.SelectScalars[string](reqCtx(c), cr.pool,
		"SELECT uri FROM content WHERE library_id = $1", c.Param("library_id"))
	if err != nil {
		return err
	}
	if contentURIs == nil {
		contentURIs = []string{}
	}
	return c.JSON(http.StatusOK, libraryURIsResponse{ContentURIs: contentURIs})
}

// brokenRefsSQL selects the user's rows and the entries of the user's lists whose URI has no
// content (@user_id), with their group as grp (groupOf; the non-capturing group makes substring
// return the whole match). A library filter outside it reaches both sides.
const brokenRefsSQL = `
	SELECT substring(b.uri FROM '^[^/]*(?:/[^/]*)?') AS grp, b.* FROM (
		SELECT u.library_id, u.uri, u.id AS utc_id, NULL::text AS entry_id FROM user_to_content u
		WHERE u.user_id = @user_id
			AND NOT EXISTS (SELECT 1 FROM content c WHERE c.library_id = u.library_id AND c.uri = u.uri)
		UNION ALL
		SELECT e.library_id, e.uri, NULL, e.id FROM custom_list_to_content e
		JOIN custom_lists l ON l.id = e.custom_list_id AND l.user_id = @user_id
		WHERE NOT EXISTS (SELECT 1 FROM content c WHERE c.library_id = e.library_id AND c.uri = e.uri)
	) b`

// brokenInLibrarySQL is brokenRefsSQL as a CTE "grouped", for @library_id.
const brokenInLibrarySQL = "WITH grouped AS (SELECT * FROM (" + brokenRefsSQL + ") g WHERE g.library_id = @library_id) "

type brokenRefsSummaryItem struct {
	LibraryID *string `json:"library_id" db:"library_id"`
	Count     int     `json:"count"      db:"count"`
}

// brokenRefsSummary counts groups per library. Rows whose library was deleted count under a null
// library_id.
func (cr *ContentRefRoutes) brokenRefsSummary(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	items, err := db.Select[brokenRefsSummaryItem](reqCtx(c), cr.pool,
		"SELECT library_id, COUNT(DISTINCT grp) AS count FROM ("+brokenRefsSQL+") g GROUP BY library_id",
		pgx.NamedArgs{"user_id": user.ID})
	if err != nil {
		return err
	}
	if items == nil {
		items = []brokenRefsSummaryItem{}
	}
	return c.JSON(http.StatusOK, items)
}

type brokenUserToContentDTO struct {
	ID                string            `json:"id"`
	URI               string            `json:"uri"`
	LibraryID         *string           `json:"library_id"`
	Starred           bool              `json:"starred"`
	Status            *string           `json:"status"`
	StatusUpdatedAt   *time.Time        `json:"status_updated_at"`
	Notes             *string           `json:"notes"`
	Rating            *int              `json:"rating"`
	Progress          json.RawMessage   `json:"progress"`
	ProgressUpdatedAt *time.Time        `json:"progress_updated_at"`
	LastReadAt        *time.Time        `json:"last_read_at"`
	ReadingSeq        models.ReadingSeq `json:"reading_seq"`
}

func brokenUTCToDTO(u models.UserToContent) brokenUserToContentDTO {
	progress := json.RawMessage(u.Progress)
	if progress == nil {
		progress = json.RawMessage("{}")
	}
	return brokenUserToContentDTO{
		ID:                u.ID,
		URI:               u.URI,
		LibraryID:         u.LibraryID,
		Starred:           u.Starred,
		Status:            u.Status,
		StatusUpdatedAt:   u.StatusUpdatedAt,
		Notes:             u.Notes,
		Rating:            u.Rating,
		Progress:          progress,
		ProgressUpdatedAt: u.ProgressUpdatedAt,
		LastReadAt:        u.LastReadAt,
		ReadingSeq:        u.ReadingSeq,
	}
}

type brokenRefsQuery struct {
	Search string `query:"search"`
	Limit  *int   `query:"limit"  validate:"omitempty,min=1"`
	Offset int    `query:"offset" validate:"min=0"`
}

type brokenListEntryDTO struct {
	ID       string  `json:"id"        db:"id"`
	ListID   string  `json:"list_id"   db:"list_id"`
	ListName string  `json:"list_name" db:"list_name"`
	URI      string  `json:"uri"       db:"uri"`
	Notes    *string `json:"notes"     db:"notes"`
}

type brokenRefGroupDTO struct {
	URI         string                   `json:"uri"`
	Items       []brokenUserToContentDTO `json:"items"`
	ListEntries []brokenListEntryDTO     `json:"list_entries"`
	Suggestions []refSuggestion          `json:"suggestions"`
}

// listBrokenRefs pages the library's broken groups. A search matching any URI in a group returns
// the whole group.
func (cr *ContentRefRoutes) listBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	q, err := BindQuery[brokenRefsQuery](c)
	if err != nil {
		return err
	}

	args := pgx.NamedArgs{"user_id": user.ID, "library_id": libraryID}
	groupsQuery := brokenInLibrarySQL + "SELECT grp FROM grouped GROUP BY grp"
	if q.Search != "" {
		args["search"] = "%" + q.Search + "%"
		groupsQuery += " HAVING bool_or(grp ILIKE @search OR uri ILIKE @search)"
	}

	total, err := db.SelectScalar[int](ctx, cr.pool, "SELECT COUNT(*) FROM ("+groupsQuery+") g", args)
	if err != nil {
		return err
	}

	groupsQuery += " ORDER BY grp"
	if q.Limit != nil {
		groupsQuery += fmt.Sprintf(" LIMIT %d", *q.Limit)
	}
	if q.Offset > 0 {
		groupsQuery += fmt.Sprintf(" OFFSET %d", q.Offset)
	}
	groups, err := db.SelectScalars[string](ctx, cr.pool, groupsQuery, args)
	if err != nil {
		return err
	}

	args["groups"] = groups
	items, err := db.Select[models.UserToContent](ctx, cr.pool, brokenInLibrarySQL+`
		SELECT u.* FROM user_to_content u
		WHERE u.id IN (SELECT utc_id FROM grouped WHERE grp = ANY(@groups))
		ORDER BY u.uri`, args)
	if err != nil {
		return err
	}
	entries, err := db.Select[brokenListEntryDTO](ctx, cr.pool, brokenInLibrarySQL+`
		SELECT e.id, e.custom_list_id AS list_id, l.name AS list_name, e.uri, e.notes
		FROM custom_list_to_content e JOIN custom_lists l ON l.id = e.custom_list_id
		WHERE e.id IN (SELECT entry_id FROM grouped WHERE grp = ANY(@groups))
		ORDER BY l.name, e.uri`, args)
	if err != nil {
		return err
	}

	dtos := make([]brokenRefGroupDTO, len(groups))
	byURI := map[string]*brokenRefGroupDTO{}
	for i, g := range groups {
		dtos[i] = brokenRefGroupDTO{URI: g, Items: []brokenUserToContentDTO{},
			ListEntries: []brokenListEntryDTO{}, Suggestions: []refSuggestion{}}
		byURI[g] = &dtos[i]
	}
	for _, it := range items {
		g := byURI[groupOf(it.URI)]
		g.Items = append(g.Items, brokenUTCToDTO(it))
	}
	for _, e := range entries {
		g := byURI[groupOf(e.URI)]
		g.ListEntries = append(g.ListEntries, e)
	}
	// Each group's distinct broken URIs, sorted; a series group has some besides its own.
	srcs := make([][]string, len(dtos))
	series := false
	for i, d := range dtos {
		for _, it := range d.Items {
			srcs[i] = append(srcs[i], it.URI)
		}
		for _, e := range d.ListEntries {
			srcs[i] = append(srcs[i], e.URI)
		}
		slices.Sort(srcs[i])
		srcs[i] = slices.Compact(srcs[i])
		series = series || slices.ContainsFunc(srcs[i], func(u string) bool { return u != d.URI })
	}

	if series {
		rows, err := db.Select[targetRow](ctx, cr.pool, `
			SELECT p.uri AS root, p.uri_part AS root_part, c.uri_part AS part
			FROM content p LEFT JOIN content c ON c.parent_id = p.id
			WHERE p.library_id = $1 AND p.parent_id IS NULL`, libraryID)
		if err != nil {
			return err
		}
		ix := newTargetIndex(rows)
		for i := range dtos {
			dtos[i].Suggestions = ix.suggest(dtos[i].URI, srcs[i])
		}
	}

	return c.JSON(http.StatusOK, PaginatedResponse[brokenRefGroupDTO]{Data: dtos, Total: total})
}

type brokenRefsPreviewRequest struct {
	From      string             `json:"from"      validate:"required"`
	To        string             `json:"to"        validate:"required"`
	Overrides map[string]*string `json:"overrides"`
}

type brokenRefPairDTO struct {
	From       string                  `json:"from"`
	To         string                  `json:"to"`
	By         string                  `json:"by"`
	Existing   *brokenUserToContentDTO `json:"existing"`
	ListMerges int                     `json:"list_merges"`
}

type brokenRefsPreviewResponse struct {
	Pairs       []brokenRefPairDTO `json:"pairs"`
	Leftovers   []string           `json:"leftovers"`
	FreeTargets []string           `json:"free_targets"`
}

// previewBrokenRefs pairs a group's broken URIs with a target. It doesn't lock: the save checks
// the pairs again. Stale overrides are dropped, and a group with nothing broken left previews
// empty, so a refetch after a failed save succeeds.
func (cr *ContentRefRoutes) previewBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	var req brokenRefsPreviewRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	type srcRow struct {
		URI    string `db:"uri"`
		HasRow bool   `db:"has_row"`
	}
	srcRows, err := db.Select[srcRow](ctx, cr.pool, brokenInLibrarySQL+`
		SELECT uri, bool_or(utc_id IS NOT NULL) AS has_row FROM grouped WHERE grp = @group
		GROUP BY uri ORDER BY uri`,
		pgx.NamedArgs{"user_id": user.ID, "library_id": libraryID, "group": req.From})
	if err != nil {
		return err
	}
	res := brokenRefsPreviewResponse{Pairs: []brokenRefPairDTO{}, Leftovers: []string{}, FreeTargets: []string{}}
	if len(srcRows) == 0 {
		return c.JSON(http.StatusOK, res)
	}
	srcs := make([]string, len(srcRows))
	hasRow := map[string]bool{}
	series := false
	for i, r := range srcRows {
		srcs[i] = r.URI
		hasRow[r.URI] = r.HasRow
		series = series || r.URI != req.From
	}

	top, err := db.SelectScalar[bool](ctx, cr.pool,
		"SELECT parent_id IS NULL FROM content WHERE library_id = $1 AND uri = $2", libraryID, req.To)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusBadRequest, "No content at "+req.To)
	}
	if err != nil {
		return err
	}
	if series && !top {
		return echo.NewHTTPError(http.StatusBadRequest, "A series moves to a top-level item")
	}
	// A one-item group moves onto the item itself, never into its children.
	var toParts []string
	if series {
		toParts, err = db.SelectScalars[string](ctx, cr.pool, `
			SELECT c.uri_part FROM content p JOIN content c ON c.parent_id = p.id
			WHERE p.library_id = $1 AND p.uri = $2 ORDER BY c."order" NULLS LAST, c.uri_part`,
			libraryID, req.To)
		if err != nil {
			return err
		}
	}

	var dsts []string
	for src, dst := range req.Overrides {
		if _, ok := hasRow[src]; ok && dst != nil {
			dsts = append(dsts, *dst)
		}
	}
	live, err := db.SelectScalars[string](ctx, cr.pool,
		"SELECT uri FROM content WHERE library_id = $1 AND uri = ANY($2)", libraryID, dsts)
	if err != nil {
		return err
	}
	overrides := map[string]*string{}
	for src, dst := range req.Overrides {
		if _, ok := hasRow[src]; ok && (dst == nil || slices.Contains(live, *dst)) {
			overrides[src] = dst
		}
	}

	p, err := pairRefs(req.From, srcs, req.To, toParts, overrides, partKeys{})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	res.Leftovers, res.FreeTargets = p.Leftovers, p.FreeTargets

	from, to := make([]string, len(p.Pairs)), make([]string, len(p.Pairs))
	for i, pr := range p.Pairs {
		from[i], to[i] = pr.From, pr.To
	}
	existing, err := db.Select[models.UserToContent](ctx, cr.pool,
		"SELECT * FROM user_to_content WHERE user_id = $1 AND library_id = $2 AND uri = ANY($3)",
		user.ID, libraryID, to)
	if err != nil {
		return err
	}
	existingAt := map[string]models.UserToContent{}
	for _, u := range existing {
		existingAt[u.URI] = u
	}
	type mergeRow struct {
		Src   string `db:"src"`
		Count int    `db:"count"`
	}
	merges, err := db.Select[mergeRow](ctx, cr.pool, `
		SELECT r.src, COUNT(*) AS count FROM unnest($1::text[], $2::text[]) r(src, dst)
		JOIN custom_list_to_content m ON m.library_id = $3 AND m.uri = r.src
		JOIN custom_lists l ON l.id = m.custom_list_id AND l.user_id = $4
		JOIN custom_list_to_content d ON d.custom_list_id = m.custom_list_id
			AND d.library_id = m.library_id AND d.uri = r.dst
		GROUP BY r.src`, from, to, libraryID, user.ID)
	if err != nil {
		return err
	}
	mergesAt := map[string]int{}
	for _, m := range merges {
		mergesAt[m.Src] = m.Count
	}

	for _, pr := range p.Pairs {
		dto := brokenRefPairDTO{From: pr.From, To: pr.To, By: pr.By, ListMerges: mergesAt[pr.From]}
		if u, ok := existingAt[pr.To]; ok && hasRow[pr.From] {
			e := brokenUTCToDTO(u)
			dto.Existing = &e
		}
		res.Pairs = append(res.Pairs, dto)
	}
	return c.JSON(http.StatusOK, res)
}

type brokenRefsMove struct {
	Keep  string            `json:"keep"  validate:"omitempty,oneof=newer source target"`
	Pairs map[string]string `json:"pairs" validate:"required,min=1,dive,keys,required,endkeys,required"`
}

type brokenRefsSaveRequest struct {
	Moves  []brokenRefsMove `json:"moves" validate:"dive"`
	Delete struct {
		Refs        []string `json:"refs"`
		ListEntries []string `json:"list_entries"`
	} `json:"delete"`
}

type brokenRefsSaveResponse struct {
	Moved   int   `json:"moved"`
	Merged  int64 `json:"merged"`
	Deleted int64 `json:"deleted"`
}

const brokenRefsChanged = "Broken references changed. Check the preview and save again."

// saveBrokenRefs applies the pairs a preview showed and deletes broken rows and entries. It never
// pairs on its own: when a source or deleted id isn't broken any more, a destination is gone, or
// two pairs share one, it answers 409 and changes nothing.
func (cr *ContentRefRoutes) saveBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	var req brokenRefsSaveRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}
	refs, entries := fp.Dedup(req.Delete.Refs), fp.Dedup(req.Delete.ListEntries)
	if len(req.Moves) == 0 && len(refs) == 0 && len(entries) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "Nothing to save")
	}
	conflict := echo.NewHTTPError(http.StatusConflict, brokenRefsChanged)

	srcs, dsts, keeps := []string{}, []string{}, []string{}
	for _, m := range req.Moves {
		keep := m.Keep
		if keep == "newer" {
			keep = "" // MergeUserToContentSQL's more recently touched
		}
		for src, dst := range m.Pairs {
			srcs, dsts, keeps = append(srcs, src), append(dsts, dst), append(keeps, keep)
		}
	}
	if len(fp.Dedup(srcs)) != len(srcs) || len(fp.Dedup(dsts)) != len(dsts) {
		return conflict
	}

	ctx, touches := scrobble.WithTouches(ctx)
	var res brokenRefsSaveResponse
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		// Scans move refs under the metadata lock, and every list entry insert takes this lock
		// too, so the checks below hold until commit.
		if err := db.LockUserData(ctx, tx, user.ID, libraryID); err != nil {
			return err
		}
		rev, now := db.ServerRevision(), time.Now().UTC()
		args := pgx.NamedArgs{"user_id": user.ID, "library_id": libraryID, "srcs": srcs, "dsts": dsts,
			"refs": refs, "entries": entries, "now": now, "rev": rev}

		// Entry updates and deletes don't take the user lock: lock the affected lists, then their
		// entries, before checking them.
		const affected = `
			FROM custom_list_to_content e
			JOIN custom_lists l ON l.id = e.custom_list_id AND l.user_id = @user_id
			WHERE e.library_id = @library_id AND (e.uri = ANY(@srcs) OR e.id = ANY(@entries))`
		lists, err := db.SelectScalars[string](ctx, tx, "SELECT DISTINCT e.custom_list_id"+affected, args)
		if err != nil {
			return err
		}
		if err := lockLists(ctx, tx, lists...); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT e.id"+affected+" FOR UPDATE OF e", args); err != nil {
			return err
		}

		stale, err := db.SelectScalar[bool](ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM unnest(@srcs::text[]) s
			WHERE EXISTS (SELECT 1 FROM content c WHERE c.library_id = @library_id AND c.uri = s)
				OR NOT (EXISTS (SELECT 1 FROM user_to_content u
						WHERE u.user_id = @user_id AND u.library_id = @library_id AND u.uri = s)
					OR EXISTS (SELECT 1 FROM custom_list_to_content e
						JOIN custom_lists l ON l.id = e.custom_list_id AND l.user_id = @user_id
						WHERE e.library_id = @library_id AND e.uri = s)))`, args)
		if err != nil {
			return err
		}
		live, err := db.SelectScalars[string](ctx, tx,
			"SELECT id FROM content WHERE library_id = @library_id AND uri = ANY(@dsts)", args)
		if err != nil {
			return err
		}
		if stale || len(live) != len(dsts) {
			return conflict
		}

		// The user filter matters: the merge otherwise takes every user's rows at the sources.
		rowsMerged, err := tx.Exec(ctx, db.MergeUserToContentSQL, libraryID, srcs, dsts, keeps, rev, user.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE user_to_content u SET uri = r.dst, revision = @rev
			FROM unnest(@srcs::text[], @dsts::text[]) r(src, dst)
			WHERE u.user_id = @user_id AND u.library_id = @library_id AND u.uri = r.src`, args); err != nil {
			return err
		}
		// A merge can change a destination's completion.
		if err := scrobble.Touch(ctx, user.ID, live...); err != nil {
			return err
		}

		// An entry moving into a list that already has the destination goes; the existing one keeps
		// its position and takes the moved notes when it has none.
		entriesMerged, err := tx.Exec(ctx, `
			WITH r AS (SELECT * FROM unnest(@srcs::text[], @dsts::text[]) r(src, dst)),
			clash AS (
				SELECT m.id AS moved_id, d.id AS dst_id, m.notes AS moved_notes
				FROM custom_list_to_content m
				JOIN custom_lists l ON l.id = m.custom_list_id AND l.user_id = @user_id
				JOIN r ON m.uri = r.src
				JOIN custom_list_to_content d ON d.custom_list_id = m.custom_list_id
					AND d.library_id = m.library_id AND d.uri = r.dst
				WHERE m.library_id = @library_id
			), filled AS (
				UPDATE custom_list_to_content d
				SET notes = COALESCE(NULLIF(d.notes, ''), c.moved_notes), updated_at = @now
				FROM clash c WHERE d.id = c.dst_id
			)
			DELETE FROM custom_list_to_content WHERE id IN (SELECT moved_id FROM clash)`, args)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE custom_list_to_content m SET uri = r.dst, updated_at = @now
			FROM unnest(@srcs::text[], @dsts::text[]) r(src, dst), custom_lists l
			WHERE m.library_id = @library_id AND m.uri = r.src
				AND l.id = m.custom_list_id AND l.user_id = @user_id`, args); err != nil {
			return err
		}

		refsDeleted, err := tx.Exec(ctx, `
			DELETE FROM user_to_content u
			WHERE u.id = ANY(@refs) AND u.user_id = @user_id AND u.library_id = @library_id
				AND NOT EXISTS (SELECT 1 FROM content c WHERE c.library_id = u.library_id AND c.uri = u.uri)`, args)
		if err != nil {
			return err
		}
		entriesDeleted, err := tx.Exec(ctx, `
			DELETE FROM custom_list_to_content e USING custom_lists l
			WHERE e.id = ANY(@entries) AND l.id = e.custom_list_id AND l.user_id = @user_id
				AND e.library_id = @library_id
				AND NOT EXISTS (SELECT 1 FROM content c WHERE c.library_id = e.library_id AND c.uri = e.uri)`, args)
		if err != nil {
			return err
		}
		// A row repaired since, moved by this request, or back to life.
		if refsDeleted.RowsAffected() != int64(len(refs)) || entriesDeleted.RowsAffected() != int64(len(entries)) {
			return conflict
		}

		// Clients refetch a list when its updated_at changes.
		args["lists"] = lists
		if _, err := tx.Exec(ctx, "UPDATE custom_lists SET updated_at = @now WHERE id = ANY(@lists)", args); err != nil {
			return err
		}
		res = brokenRefsSaveResponse{Moved: len(srcs),
			Merged:  rowsMerged.RowsAffected() + entriesMerged.RowsAffected(),
			Deleted: refsDeleted.RowsAffected() + entriesDeleted.RowsAffected()}
		return scrobble.Flush(ctx, tx, now)
	})
	if err != nil {
		return err
	}
	cr.scrobbles.Changed(touches.Users())

	return c.JSON(http.StatusOK, res)
}
