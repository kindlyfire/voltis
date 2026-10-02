package routes

import (
	"context"
	"encoding/json"
	"net/http"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// homePrefs reads the user's home preferences; malformed preferences mean the defaults.
func homePrefs(raw models.JSONB) (ignoreSeriesStatus bool) {
	var p struct {
		Home struct {
			IgnoreSeriesStatus bool `json:"ignoreSeriesStatus"`
		} `json:"home"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	return p.Home.IgnoreSeriesStatus
}

// recencyExpr is when the user last touched a user_to_content row: read it or set its status.
func recencyExpr(a string) string {
	return "GREATEST(" + a + ".last_read_at, " + a + ".status_updated_at)"
}

// ordKey orders a series' children: by number, unnumbered last, then by id.
func ordKey(a string) string { return `(COALESCE(` + a + `."order", 2147483647), ` + a + `.id)` }

// hasPosition says whether a user_to_content row holds a position to resume from.
func hasPosition(a string) string { return "(COALESCE(" + a + ".progress, '{}') - 'at_end' <> '{}')" }

// eligible says whether a child with that user_to_content row can be read next.
func eligible(a string) string {
	return "(" + a + ".status IS NULL OR " + a + ".status IN ('reading', 'plan_to_read'))"
}

// newGuard says whether n volumes added since the reference are few enough, out of a series'
// total valid ones, to be new volumes rather than a scan re-adding the series.
func newGuard(n, total string) string { return n + " <= GREATEST(3, " + total + " / 2.0)" }

// childJoin joins series s's valid children k with the user's rows ku.
const childJoin = `content k
	LEFT JOIN user_to_content ku ON ku.library_id = k.library_id AND ku.uri = k.uri AND ku.user_id = @user_id`

// kidArrays aggregates a series' valid children k, with the user's rows ku, into the arrays that
// kidRows unnests: resolving a series looks at its children several times, but reads them once.
func kidArrays(hasPos string) string {
	return `array_agg(k.id) AS id, array_agg(k."order") AS ord, array_agg(k.created_at) AS created_at,
	array_agg(ku.status) AS status, array_agg(` + recencyExpr("ku") + `) AS recency,
	array_agg(ku.last_read_at IS NOT NULL OR ku.status IN ('reading', 'completed', 'on_hold', 'dropped')) AS read,
	array_agg(COALESCE(` + hasPos + `, false)) AS has_pos`
}

// seriesKids reads series s's children as kids.
var seriesKids = `
	CROSS JOIN LATERAL (
		SELECT ` + kidArrays(hasPosition("ku")) + `
		FROM ` + childJoin + `
		WHERE k.parent_id = s.id AND k.valid
	) kids`

// kidRows are seriesKids' children as rows k.
const kidRows = `unnest(kids.id, kids.ord, kids.created_at, kids.status, kids.recency, kids.read, kids.has_pos)
	AS k(id, "order", created_at, status, recency, read, has_pos)`

// seriesLaterals resolve series s from its kids: anchor a (the child read or set last), target t,
// the counts g, and is_new and action in x.
var seriesLaterals = `
	LEFT JOIN LATERAL (
		SELECT k.id, k."order", k.created_at, k.status, k.recency
		FROM ` + kidRows + `
		WHERE k.read
		ORDER BY k.recency DESC NULLS LAST, ` + ordKey("k") + ` DESC
		LIMIT 1
	) a ON true
	LEFT JOIN LATERAL (
		SELECT k.id, k.created_at, k.has_pos
		FROM ` + kidRows + `
		WHERE ` + eligible("k") + `
			AND (a.id IS NULL OR CASE WHEN ` + eligible("a") + ` THEN k.id = a.id
				ELSE ` + ordKey("k") + ` > ` + ordKey("a") + ` END)
		ORDER BY ` + ordKey("k") + `
		LIMIT 1
	) t ON true
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS valid_n,
			COUNT(*) FILTER (WHERE k.status IN ('completed', 'dropped')) AS done_n,
			COUNT(*) FILTER (WHERE NOT ` + done("k") + ` AND k.created_at > a.recency) AS new_n,
			MAX(k.created_at) FILTER (WHERE NOT ` + done("k") + ` AND k.created_at > a.recency
				AND ` + ordKey("k") + ` > ` + ordKey("a") + `) AS new_added_at
		FROM ` + kidRows + `
	) g ON true
	CROSS JOIN LATERAL (
		SELECT t.id <> a.id AND t.created_at > a.recency AND ` + newGuard("g.new_n", "g.valid_n") + ` AS is_new,
			CASE WHEN t.has_pos THEN 'resume' WHEN a.id IS NULL OR t.id = a.id THEN 'start' ELSE 'next' END
				AS action
	) x`

func done(a string) string { return "COALESCE(" + a + ".status IN ('completed', 'dropped'), false)" }

// continueTargetsSQL lists the user's continue reading targets: standalone items being read, and
// each started series' next volume. Held (on hold, dropped) items and series only show with
// @ignore_series_status. The user's rows are read once, and the started series' children together;
// content is looked up by index from them (a hash join would scan it all).
var continueTargetsSQL = `
	WITH mine AS MATERIALIZED (
		SELECT c.id, c.parent_id, c.type, c.valid, utc.status, ` + hasPosition("utc") + ` AS has_pos,
			utc.last_read_at, utc.status_updated_at
		FROM user_to_content utc
		CROSS JOIN LATERAL (
			SELECT c.id, c.parent_id, c.type, c.valid FROM content c
			WHERE c.library_id = utc.library_id AND c.uri = utc.uri OFFSET 0
		) c
		WHERE utc.user_id = @user_id
	), cs AS MATERIALIZED (
		SELECT started.id FROM (
			SELECT parent_id AS id FROM mine
			WHERE parent_id IS NOT NULL
				AND (last_read_at IS NOT NULL OR status IN ('reading', 'completed', 'on_hold', 'dropped'))
			UNION
			SELECT id FROM mine
			WHERE type IN ('comic_series', 'book_series')
				AND (status = 'reading' OR @ignore_series_status AND status IN ('on_hold', 'dropped'))
		) started
		LEFT JOIN mine su ON su.id = started.id
		-- Series whose own status keeps them out aren't resolved at all.
		WHERE su.status IS NULL OR su.status = 'reading'
			OR @ignore_series_status AND su.status IN ('on_hold', 'dropped')
	), children AS MATERIALIZED (
		SELECT k.id, k.parent_id, k."order", k.created_at
		FROM cs
		CROSS JOIN LATERAL (SELECT k.* FROM content k WHERE k.parent_id = cs.id AND k.valid OFFSET 0) k
	), kids AS (
		SELECT k.parent_id, ` + kidArrays("ku.has_pos") + `
		FROM children k
		LEFT JOIN mine ku ON ku.id = k.id
		GROUP BY k.parent_id
	)
	SELECT c.id AS target_id, NULL::text AS series_id, NULL::text AS series_status,
		CASE WHEN c.has_pos THEN 'resume' ELSE 'start' END AS action, false AS is_new,
		` + recencyExpr("c") + ` AS recency, NULL::timestamptz AS new_added_at
	FROM mine c
	WHERE c.parent_id IS NULL AND c.valid AND c.type IN ('book', 'comic')
		AND (c.status = 'reading' OR @ignore_series_status AND c.status IN ('on_hold', 'dropped'))
	UNION ALL
	SELECT t.id, cs.id, su.status, x.action, COALESCE(x.is_new, false), COALESCE(a.recency, su.status_updated_at),
		CASE WHEN x.is_new THEN g.new_added_at END
	FROM cs
	LEFT JOIN mine su ON su.id = cs.id
	LEFT JOIN kids ON kids.parent_id = cs.id` +
	seriesLaterals + `
	WHERE t.id IS NOT NULL AND (su.status = 'reading' OR su.status IS NULL AND a.id IS NOT NULL
		OR @ignore_series_status AND su.status IN ('on_hold', 'dropped'))`

func continueOrder(dir string) string {
	return "ct.recency " + dir + " " + nullsOrder(dir) + ", ct.target_id " + dir
}

type readingPick struct {
	Item   contentListRow
	Series *contentListRow // nil for a standalone item
	Action string
	IsNew  bool
}

// continueReading lists the user's continue reading targets, most recent first.
func continueReading(ctx context.Context, pool *pgxpool.Pool, user *models.User, limit int) ([]readingPick, error) {
	rows, err := listContent(ctx, pool, user.ID, contentListQuery{
		Sort: "continue", SortOrder: "desc", Limit: &limit, IgnoreSeriesStatus: homePrefs(user.Preferences),
	})
	if err != nil {
		return nil, err
	}
	picks := make([]readingPick, len(rows))
	for i, r := range rows {
		picks[i] = readingPick{Item: r, Series: r.Continue.Series, Action: r.Continue.Action, IsNew: r.Continue.IsNew}
		picks[i].Item.Continue = nil
	}
	return picks, nil
}

type ContinueEntryDTO struct {
	Item   ContentDTO  `json:"item"`
	Series *ContentDTO `json:"series"`
	Action string      `json:"action"`
	IsNew  bool        `json:"is_new"`
}

type continueListQuery struct {
	Limit int `query:"limit" default:"10" validate:"min=1,max=50"`
}

func (cr *ContentRoutes) continueList(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	q, err := BindQuery[continueListQuery](c)
	if err != nil {
		return err
	}
	picks, err := continueReading(reqCtx(c), cr.pool, user, q.Limit)
	if err != nil {
		return err
	}
	entries := make([]ContinueEntryDTO, len(picks))
	for i, p := range picks {
		entries[i] = ContinueEntryDTO{Item: p.Item.dto(false, false), Action: p.Action, IsNew: p.IsNew}
		if p.Series != nil {
			dto := p.Series.dto(false, false)
			entries[i].Series = &dto
		}
	}
	return c.JSON(http.StatusOK, entries)
}

type ContinueTargetDTO struct {
	Target          *ContentDTO `json:"target"`
	SeriesID        *string     `json:"series_id"`
	Action          *string     `json:"action"`
	Reason          *string     `json:"reason"`
	IsNew           bool        `json:"is_new"`
	EarlierUnreadID *string     `json:"earlier_unread_id"`
}

// resolveContinue finds what reading content continues with: an item itself, or a series' next
// volume. Without a target, the reason says why.
func resolveContinue(ctx context.Context, q db.Querier, userID, contentID string) (ContinueTargetDTO, error) {
	t, err := loadReadingTarget(ctx, q, userID, contentID)
	if err != nil {
		return ContinueTargetDTO{}, err
	}
	var res ContinueTargetDTO
	var targetID string
	if !t.isSeries() {
		var m map[string]any
		_ = json.Unmarshal(t.Progress, &m)
		atEnd := m["at_end"] == true
		delete(m, "at_end")
		hasPos := len(m) > 0
		res.SeriesID = t.ParentID
		switch {
		case t.Status != nil && *t.Status == "completed" && (!hasPos || atEnd):
			res.Reason = new("completed")
			return res, nil
		case hasPos:
			res.Action = new("resume")
		default:
			res.Action = new("start")
		}
		targetID = contentID
	} else {
		r, err := db.SelectOne[struct {
			TargetID   *string `db:"target_id"`
			Action     *string `db:"action"`
			IsNew      *bool   `db:"is_new"`
			EarlierID  *string `db:"earlier_id"`
			HeldID     *string `db:"held_id"`
			HeldHasPos *bool   `db:"held_has_pos"`
			ValidN     int     `db:"valid_n"`
			DoneN      int     `db:"done_n"`
		}](ctx, q, `
			SELECT t.id AS target_id, x.action, x.is_new, e.id AS earlier_id, h.id AS held_id,
				h.has_pos AS held_has_pos, g.valid_n, g.done_n
			FROM content s`+seriesKids+seriesLaterals+`
			LEFT JOIN LATERAL (
				SELECT k.id FROM `+kidRows+`
				WHERE `+eligible("k")+` AND a.id IS NOT NULL AND `+ordKey("k")+` < `+ordKey("a")+`
				ORDER BY `+ordKey("k")+` LIMIT 1
			) e ON true
			LEFT JOIN LATERAL (
				SELECT k.id, k.has_pos FROM `+kidRows+`
				WHERE k.status = 'on_hold'
				ORDER BY (a.id IS NOT NULL AND `+ordKey("k")+` > `+ordKey("a")+`) DESC, `+ordKey("k")+`
				LIMIT 1
			) h ON true
			WHERE s.id = @id
		`, pgx.NamedArgs{"user_id": userID, "id": contentID})
		if err != nil {
			return res, err
		}
		res.SeriesID, res.EarlierUnreadID = &contentID, r.EarlierID
		switch {
		case r.TargetID != nil:
			res.Action, res.IsNew = r.Action, r.IsNew != nil && *r.IsNew
		case r.ValidN == 0:
			res.Reason = new("empty")
		case r.DoneN == r.ValidN:
			res.Reason = new("caught_up")
		case r.EarlierID != nil:
			res.Reason = new("earlier_unread")
		case r.HeldID != nil:
			r.TargetID, res.Reason, res.Action = r.HeldID, new("held"), new("start")
			if *r.HeldHasPos {
				res.Action = new("resume")
			}
		}
		if r.TargetID == nil {
			return res, nil
		}
		targetID = *r.TargetID
	}
	rows, err := selectContentRows(ctx, q, userID, []string{targetID})
	if err != nil {
		return res, err
	}
	// A scan may have deleted, invalidated or reparented a series' target since it was picked.
	row, ok := rows[targetID]
	if !ok || t.isSeries() && (!row.Valid || !fp.PtrEq(row.ParentID, &contentID)) {
		res.Action = nil
		return res, nil
	}
	dto := row.dto(false, false)
	res.Target = &dto
	return res, nil
}

func (cr *ContentRoutes) continueOne(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	res, err := resolveContinue(reqCtx(c), cr.pool, user.ID, c.Param("content_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}
