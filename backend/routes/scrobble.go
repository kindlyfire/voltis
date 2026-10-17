package routes

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/fp"
	"voltis/lib/notify"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/mangabaka"
	"voltis/scrobble"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

const releaseDelay = 5 * time.Minute

type ScrobbleRoutes struct {
	pool *pgxpool.Pool
	svc  *scrobble.Service
	lib  *mangabaka.Library
	hub  *WebSocketHub
}

// Register mounts the account under /users/me/scrobbling, the queue under /scrobbles, and a
// series' scrobbling under /content.
func (sr *ScrobbleRoutes) Register(account, queue, content *echo.Group) {
	account.GET("", sr.account)
	account.PUT("/mangabaka", sr.connect)
	account.PATCH("/mangabaka", sr.patchAccount)
	account.DELETE("/mangabaka", sr.disconnect)
	account.POST("/mangabaka/sync-all", sr.syncAll)
	queue.GET("", sr.queue)
	queue.GET("/recent", sr.recent)
	queue.POST("/send", sr.sendQueue)
	content.GET("/:content_id/scrobble", sr.series)
	content.PUT("/:content_id/scrobble", sr.hold)
	content.POST("/:content_id/scrobble/send", sr.sendSeries)
}

type scrobbleAccountDTO struct {
	Connected  bool    `json:"connected"`
	RemoteName *string `json:"remote_name"`
	Rejected   bool    `json:"rejected"`
	PrivateNew bool    `json:"private_new"`
}

func (sr *ScrobbleRoutes) accountResponse(c echo.Context, userID string) error {
	var a scrobbleAccountDTO
	err := sr.pool.QueryRow(reqCtx(c), `
		SELECT true, remote_name, rejected_at IS NOT NULL, private_new FROM scrobble_accounts
		WHERE user_id = $1 AND provider = 'mangabaka'
	`, userID).Scan(&a.Connected, &a.RemoteName, &a.Rejected, &a.PrivateNew)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return c.JSON(http.StatusOK, map[string]scrobbleAccountDTO{"mangabaka": a})
}

func (sr *ScrobbleRoutes) account(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	return sr.accountResponse(c, user.ID)
}

// connect checks the token with MangaBaka and stores it. Pending scrobbles, kept while a token
// was rejected, are sent again; a different account forgets what was sent before.
func (sr *ScrobbleRoutes) connect(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Enter a token.")
	}
	ctx := reqCtx(c)
	profile, err := sr.lib.Profile(providers.Interactive(ctx), token)
	if err != nil {
		if mangabaka.Classify(err, http.MethodGet) == mangabaka.Transient {
			return echo.NewHTTPError(http.StatusBadGateway, "Couldn't reach MangaBaka")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "MangaBaka rejected this token.")
	}

	cleared := false
	err = db.WithTx(ctx, sr.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM users WHERE id = $1 FOR KEY SHARE", user.ID); err != nil {
			return err
		}
		// FOR UPDATE waits out a worker write-back of the old token, and blocks new ones until commit.
		prev, err := db.SelectScalars[string](ctx, tx, `SELECT remote_id FROM scrobble_accounts
			WHERE user_id = $1 AND provider = 'mangabaka' FOR UPDATE`, user.ID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scrobble_accounts (user_id, provider, token, remote_id, remote_name)
			VALUES ($1, 'mangabaka', $2, $3, $4)
			ON CONFLICT (user_id, provider) DO UPDATE SET token = EXCLUDED.token, remote_id = EXCLUDED.remote_id,
				remote_name = EXCLUDED.remote_name, rejected_at = NULL
		`, user.ID, token, profile.ID, profile.Name); err != nil {
			return err
		}
		if len(prev) > 0 && prev[0] != profile.ID {
			if err := sr.svc.ResetSent(ctx, tx, user.ID); err != nil {
				return err
			}
		}
		cleared, err = notify.ClearScrobbleAuth(ctx, tx, user.ID)
		return err
	})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	if cleared {
		sr.hub.NotificationsChanged([]string{user.ID})
	}
	return sr.accountResponse(c, user.ID)
}

func (sr *ScrobbleRoutes) patchAccount(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req struct {
		PrivateNew bool `json:"private_new"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	tag, err := sr.pool.Exec(reqCtx(c), `UPDATE scrobble_accounts SET private_new = $2
		WHERE user_id = $1 AND provider = 'mangabaka'`, user.ID, req.PrivateNew)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "MangaBaka isn't connected")
	}
	return sr.accountResponse(c, user.ID)
}

// disconnect deletes the token and drops pending scrobbles. Holds stay.
func (sr *ScrobbleRoutes) disconnect(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	cleared := false
	err = db.WithTx(ctx, sr.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM scrobble_accounts WHERE user_id = $1 AND provider = 'mangabaka'",
			user.ID); err != nil {
			return err
		}
		if _, err := sr.svc.DropPending(ctx, tx, "user_id = $1", user.ID); err != nil {
			return err
		}
		// A later connect can't tell whether it's the same account.
		if err := sr.svc.ResetSent(ctx, tx, user.ID); err != nil {
			return err
		}
		cleared, err = notify.ClearScrobbleAuth(ctx, tx, user.ID)
		return err
	})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	if cleared {
		sr.hub.NotificationsChanged([]string{user.ID})
	}
	return okResponse(c)
}

// syncAll queues every linked series the user has progress in, due now.
func (sr *ScrobbleRoutes) syncAll(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	var queued []string
	err = db.WithTx(ctx, sr.pool, func(tx pgx.Tx) error {
		series, err := db.SelectScalars[string](ctx, tx, `
			SELECT c.id FROM content c`+utcJoin+`
			WHERE c.type = ANY(@series_types)
				AND EXISTS (SELECT 1 FROM metadata_links ml
					WHERE ml.content_id = c.id AND ml.provider = 'mangabaka' AND ml.state = 'linked')
				AND (utc.status IS NOT NULL OR EXISTS (
					SELECT 1 FROM content k JOIN user_to_content ku
						ON ku.user_id = @user_id AND ku.library_id = k.library_id AND ku.uri = k.uri
					WHERE k.parent_id = c.id AND ku.status = 'completed'))
		`, pgx.NamedArgs{"user_id": user.ID, "series_types": metadata.SeriesTypes})
		if err != nil {
			return err
		}
		if err := lockUserData(ctx, tx, user.ID, series...); err != nil {
			return err
		}
		connected, err := scrobble.LockAccount(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		if !connected {
			return echo.NewHTTPError(http.StatusConflict, "MangaBaka isn't connected")
		}
		queued, err = scrobble.Queue(ctx, tx, user.ID, series, time.Now().UTC(), 0)
		return err
	})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	return c.JSON(http.StatusOK, map[string]int{"count": len(queued)})
}

type scrobbleRowDTO struct {
	ContentID string          `json:"content_id" db:"content_id"`
	Title     string          `json:"title"      db:"title"`
	State     string          `json:"state"      db:"state"`
	DueAt     *time.Time      `json:"due_at"     db:"due_at"`
	Values    scrobble.Values `json:"values"     db:"-"`
	Error     *string         `json:"error"      db:"error"`
}

// scrobbleState is a row's state for the client, or NULL for a row with nothing to show.
const scrobbleState = `CASE WHEN s.held THEN 'held' WHEN s.error IS NOT NULL THEN 'failed'
	WHEN s.pending_since IS NOT NULL THEN 'pending' END`

// queue lists the user's failed, pending and held series.
func (sr *ScrobbleRoutes) queue(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	rows, err := db.Select[scrobbleRowDTO](ctx, sr.pool, `
		SELECT s.content_id, COALESCE(NULLIF(c.data->>'title', ''), c.uri_part) AS title, `+scrobbleState+` AS state,
			s.due_at, s.error
		FROM scrobbles s JOIN content c ON c.id = s.content_id
		WHERE s.user_id = $1 AND s.provider = 'mangabaka'
			AND (s.pending_since IS NOT NULL OR s.error IS NOT NULL OR s.held)
		ORDER BY CASE `+scrobbleState+` WHEN 'failed' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END,
			s.due_at, title, s.content_id
	`, user.ID)
	if err != nil {
		return err
	}
	values, err := scrobble.Compute(ctx, sr.pool, user.ID, fp.Map(rows, func(r scrobbleRowDTO) string { return r.ContentID }))
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Values = values[rows[i].ContentID]
	}
	if rows == nil {
		rows = []scrobbleRowDTO{}
	}
	return c.JSON(http.StatusOK, rows)
}

type recentScrobbleDTO struct {
	ContentID    string    `json:"content_id"    db:"content_id"`
	Title        string    `json:"title"         db:"title"`
	SentAt       time.Time `json:"sent_at"       db:"sent_at"`
	CoverVersion *string   `json:"cover_version" db:"-"`
}

// recent lists the series last sent to MangaBaka, newest first.
func (sr *ScrobbleRoutes) recent(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	rows, err := db.Select[struct {
		recentScrobbleDTO
		Cover *metadata.CoverRef `db:"cover"`
		Local bool               `db:"local"`
		Mtime *time.Time         `db:"file_mtime"`
	}](reqCtx(c), sr.pool, `
		SELECT s.content_id, COALESCE(NULLIF(c.data->>'title', ''), c.uri_part) AS title, s.sent_at,
			c.data->'cover' AS cover, c.cover_uri IS NOT NULL AS local, c.file_mtime
		FROM scrobbles s JOIN content c ON c.id = s.content_id
		WHERE s.user_id = $1 AND s.provider = 'mangabaka' AND s.sent_at IS NOT NULL
		ORDER BY s.sent_at DESC, s.content_id LIMIT 20
	`, user.ID)
	if err != nil {
		return err
	}
	out := make([]recentScrobbleDTO, len(rows))
	for i, r := range rows {
		out[i] = r.recentScrobbleDTO
		out[i].CoverVersion = covers.Version(r.Cover, r.Local, r.Mtime)
	}
	return c.JSON(http.StatusOK, out)
}

// sendQueue makes pending scrobbles due now: all of them, or with content_ids those series, failed
// ones included. It never queues anything new.
func (sr *ScrobbleRoutes) sendQueue(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req struct {
		ContentIDs []string `json:"content_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	where, set := "pending_since IS NOT NULL AND error IS NULL", "attempts = 0, conflicts = 0"
	if req.ContentIDs != nil {
		where, set = "content_id = ANY(@ids) AND (pending_since IS NOT NULL OR error IS NOT NULL)",
			"error = NULL, attempts = 0, conflicts = 0"
	}
	_, err = sr.pool.Exec(reqCtx(c), `
		WITH l AS (
			SELECT user_id, provider, content_id FROM scrobbles
			WHERE user_id = @user_id AND provider = 'mangabaka' AND NOT held AND `+where+`
			ORDER BY user_id, provider, content_id FOR UPDATE
		)
		UPDATE scrobbles s SET due_at = @now, `+set+`
		FROM l WHERE (s.user_id, s.provider, s.content_id) = (l.user_id, l.provider, l.content_id)
	`, pgx.NamedArgs{"user_id": user.ID, "ids": req.ContentIDs, "now": time.Now().UTC()})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	return okResponse(c)
}

type contentScrobbleDTO struct {
	Available   scrobble.Availability `json:"available"`
	Held        bool                  `json:"held"`
	State       *string               `json:"state,omitempty"`
	DueAt       *time.Time            `json:"due_at,omitempty"`
	SentAt      *time.Time            `json:"sent_at,omitempty"`
	Error       *string               `json:"error,omitempty"`
	Ours        scrobble.Values       `json:"ours"`   // what is sent for the MangaBaka entry, with the linked series
	Ahead       []string              `json:"ahead"`  // fields MangaBaka keeps over ours: chapter, volume, state
	Linked      []linkedSeriesDTO     `json:"linked"` // the user's other series linked to the entry
	Remote      *scrobble.Values      `json:"remote"` // null: no entry on MangaBaka, or it couldn't be read
	RemoteError *string               `json:"remote_error,omitempty"`
}

type linkedSeriesDTO struct {
	ContentID string `json:"content_id" db:"content_id"`
	Title     string `json:"title"      db:"title"`
	Held      bool   `json:"held"       db:"-"`
}

// series is a series' scrobbling, with the user's live MangaBaka entry when it can be scrobbled.
func (sr *ScrobbleRoutes) series(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	id := c.Param("content_id")
	if err := requireSeries(ctx, sr.pool, id); err != nil {
		return err
	}
	var res contentScrobbleDTO
	var canonical string
	if res.Available, canonical, err = sr.svc.Eligible(ctx, sr.pool, user.ID, id); err != nil {
		return err
	}
	err = sr.pool.QueryRow(ctx, `SELECT s.held, `+scrobbleState+`, s.due_at, s.sent_at, s.error FROM scrobbles s
		WHERE s.user_id = $1 AND s.provider = 'mangabaka' AND s.content_id = $2`, user.ID, id).
		Scan(&res.Held, &res.State, &res.DueAt, &res.SentAt, &res.Error)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	res.Ahead, res.Linked = []string{}, []linkedSeriesDTO{}
	group := []string{id} // the series sent together; just this one when it is on hold or can't be scrobbled
	if res.Available == scrobble.Available {
		linked, err := sr.svc.Linked(ctx, sr.pool, user.ID, canonical)
		if err != nil {
			return err
		}
		if res.Linked, err = sr.linkedSeries(ctx, id, linked); err != nil {
			return err
		}
		if !linked[id] {
			group = scrobble.Unheld(linked)
		}
	}
	values, err := scrobble.Compute(ctx, sr.pool, user.ID, group)
	if err != nil {
		return err
	}
	res.Ours = scrobble.Merge(slices.Collect(maps.Values(values))...)
	if res.Available != scrobble.Available {
		return c.JSON(http.StatusOK, res)
	}

	token, err := db.SelectScalar[string](ctx, sr.pool,
		"SELECT token FROM scrobble_accounts WHERE user_id = $1 AND provider = 'mangabaka'", user.ID)
	if err != nil {
		return err
	}
	entry, err := sr.lib.Entry(providers.Interactive(ctx), token, canonical)
	switch {
	case err == nil:
		if entry != nil {
			res.Remote = &scrobble.Values{Chapter: entry.Chapter, Volume: entry.Volume, State: &entry.State}
			res.Ahead = scrobble.Ahead(res.Ours, entry)
		}
	case mangabaka.Classify(err, http.MethodGet) == mangabaka.Rejected:
		if err := sr.svc.Reject(ctx, user.ID, token, time.Now().UTC()); err != nil {
			return err
		}
		res.RemoteError = new("MangaBaka rejected your token.")
	case mangabaka.Classify(err, http.MethodGet) == mangabaka.Transient:
		res.RemoteError = new("Couldn't reach MangaBaka")
	default: // a refusal, which only an *Error is
		e, _ := errors.AsType[*mangabaka.Error](err)
		res.RemoteError = &e.Message
	}
	return c.JSON(http.StatusOK, res)
}

// linkedSeries lists the series of linked other than id, by title.
func (sr *ScrobbleRoutes) linkedSeries(ctx context.Context, id string, linked map[string]bool) ([]linkedSeriesDTO, error) {
	others := slices.Collect(maps.Keys(linked))
	others = slices.DeleteFunc(others, func(o string) bool { return o == id })
	rows, err := db.Select[linkedSeriesDTO](ctx, sr.pool, `
		SELECT id AS content_id, COALESCE(NULLIF(data->>'title', ''), uri_part) AS title
		FROM content WHERE id = ANY($1) ORDER BY title, id`, others)
	if rows == nil {
		rows = []linkedSeriesDTO{}
	}
	for i := range rows {
		rows[i].Held = linked[rows[i].ContentID]
	}
	return rows, err
}

// hold stops or resumes scrobbling a series. Holding drops what's pending; releasing queues the
// current state.
func (sr *ScrobbleRoutes) hold(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req struct {
		Held *bool `json:"held"`
	}
	if err := c.Bind(&req); err != nil || req.Held == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "held is required")
	}
	ctx := reqCtx(c)
	id := c.Param("content_id")
	err = db.WithTx(ctx, sr.pool, func(tx pgx.Tx) error {
		if err := requireSeries(ctx, tx, id); err != nil {
			return err
		}
		if err := lockUserData(ctx, tx, user.ID, id); err != nil {
			return err
		}
		if _, err := scrobble.LockAccount(ctx, tx, user.ID); err != nil {
			return err
		}
		if *req.Held {
			_, err := tx.Exec(ctx, `
				INSERT INTO scrobbles (user_id, provider, content_id, held) VALUES ($1, 'mangabaka', $2, true)
				ON CONFLICT (user_id, provider, content_id) DO UPDATE SET held = true, pending_since = NULL,
					changed_at = NULL, due_at = NULL, error = NULL, attempts = 0, conflicts = 0, force = false
			`, user.ID, id)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE scrobbles SET held = false
			WHERE user_id = $1 AND provider = 'mangabaka' AND content_id = $2`, user.ID, id); err != nil {
			return err
		}
		// Queue leaves out a series that can't be scrobbled.
		_, err := scrobble.Queue(ctx, tx, user.ID, []string{id}, time.Now().UTC(), releaseDelay)
		return err
	})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	return okResponse(c)
}

// sendSeries makes a series due now; with force (Send anyway) it may lower MangaBaka.
func (sr *ScrobbleRoutes) sendSeries(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req struct {
		Force bool `json:"force"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	ctx := reqCtx(c)
	id := c.Param("content_id")
	unavailable := echo.NewHTTPError(http.StatusConflict, "This series can't be scrobbled")
	err = db.WithTx(ctx, sr.pool, func(tx pgx.Tx) error {
		if err := requireSeries(ctx, tx, id); err != nil {
			return err
		}
		if err := lockUserData(ctx, tx, user.ID, id); err != nil {
			return err
		}
		connected, err := scrobble.LockAccount(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		if !connected {
			return unavailable
		}
		available, _, err := sr.svc.Eligible(ctx, tx, user.ID, id)
		if err != nil {
			return err
		}
		if available != scrobble.Available {
			return unavailable
		}
		// A new version: an in-flight ack of the old one can't clear force.
		tag, err := tx.Exec(ctx, `
			INSERT INTO scrobbles (user_id, provider, content_id, pending_since, changed_at, due_at, force)
			VALUES (@user_id, 'mangabaka', @id, @now::timestamptz, @now::timestamptz, @now::timestamptz, @force)
			ON CONFLICT (user_id, provider, content_id) DO UPDATE SET
				pending_since = COALESCE(scrobbles.pending_since, @now), changed_at = @now, due_at = @now,
				error = NULL, attempts = 0, conflicts = 0, force = @force
			WHERE NOT scrobbles.held
		`, pgx.NamedArgs{"user_id": user.ID, "id": id, "now": time.Now().UTC(), "force": req.Force})
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return echo.NewHTTPError(http.StatusConflict, "This series is on hold")
		}
		return nil
	})
	if err != nil {
		return err
	}
	sr.svc.Changed([]string{user.ID})
	return okResponse(c)
}

// requireSeries answers 404 for missing content and 400 for content that isn't a series.
func requireSeries(ctx context.Context, q db.Querier, id string) error {
	typ, err := db.SelectScalar[string](ctx, q, "SELECT type FROM content WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}
	if !slices.Contains(metadata.SeriesTypes, typ) {
		return echo.NewHTTPError(http.StatusBadRequest, "Not a series")
	}
	return nil
}
