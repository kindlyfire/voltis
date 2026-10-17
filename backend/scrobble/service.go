package scrobble

import (
	"context"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/notify"
	"voltis/models"
	"voltis/providers/mangabaka"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool     *pgxpool.Pool
	lib      *mangabaka.Library
	wake     chan struct{}
	onChange func([]string) // users whose scrobbles changed
	onAuth   func([]string) // users whose notifications changed
	clock    func() time.Time
}

func New(pool *pgxpool.Pool, lib *mangabaka.Library, onChange, onAuth func([]string)) *Service {
	return &Service{pool: pool, lib: lib, wake: make(chan struct{}, 1), onChange: onChange, onAuth: onAuth,
		clock: time.Now}
}

// Wake makes the worker look for due scrobbles now.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Changed is called after a commit that changed the users' scrobbles, if any.
func (s *Service) Changed(users []string) {
	if len(users) == 0 {
		return
	}
	s.Wake()
	s.onChange(users)
}

type Availability string

const (
	Available    Availability = "ok"
	NotConnected Availability = "not_connected"
	LibraryOff   Availability = "library_off"
	NotLinked    Availability = "not_linked"
)

// linkJoin joins a series s to its MangaBaka link and entry, whose canonical_id follows merges.
const linkJoin = `
	JOIN metadata_links ml ON ml.content_id = s.id AND ml.provider = 'mangabaka' AND ml.state = 'linked'
	JOIN provider_entries pe ON (pe.provider, pe.external_id) = (ml.provider, ml.external_id)`

// Eligible says whether the user can scrobble the series and, when so, its MangaBaka id.
func (s *Service) Eligible(ctx context.Context, q db.Querier, userID, seriesID string) (Availability, string, error) {
	row, err := db.SelectOne[struct {
		Connected bool    `db:"connected"`
		On        bool    `db:"switched_on"`
		Canonical *string `db:"canonical_id"`
	}](ctx, q, `
		SELECT EXISTS (SELECT 1 FROM scrobble_accounts a WHERE a.user_id = @user_id AND a.provider = 'mangabaka')
				AS connected,
			`+switchesOn+` AS switched_on, pe.canonical_id
		FROM content s
		JOIN libraries l ON l.id = s.library_id
		JOIN users u ON u.id = @user_id
		LEFT JOIN metadata_links ml ON ml.content_id = s.id AND ml.provider = 'mangabaka' AND ml.state = 'linked'
		LEFT JOIN provider_entries pe ON (pe.provider, pe.external_id) = (ml.provider, ml.external_id)
		WHERE s.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": seriesID})
	if err != nil {
		return "", "", err
	}
	switch {
	case !row.Connected:
		return NotConnected, "", nil
	case !row.On:
		return LibraryOff, "", nil
	case row.Canonical == nil:
		return NotLinked, "", nil
	}
	return Available, *row.Canonical, nil
}

// eligible maps the user's series that cond selects, among those they can scrobble, to their
// MangaBaka id. cond may use s, pe and the args.
func eligible(ctx context.Context, q db.Querier, userID, cond string, args pgx.NamedArgs) (map[string]string, error) {
	args["user_id"] = userID
	rows, err := db.Select[struct {
		ID        string `db:"id"`
		Canonical string `db:"canonical_id"`
	}](ctx, q, `
		SELECT s.id, pe.canonical_id
		FROM content s
		JOIN libraries l ON l.id = s.library_id
		JOIN users u ON u.id = @user_id`+linkJoin+`
		WHERE `+switchesOn+` AND (`+cond+`)
	`, args)
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Canonical
	}
	return out, err
}

// notHeld is an eligible condition that leaves out the series on hold, which are never sent.
const notHeld = `NOT EXISTS (SELECT 1 FROM scrobbles h WHERE h.user_id = @user_id AND h.provider = 'mangabaka'
	AND h.content_id = s.id AND h.held)`

// Linked maps the user's eligible series linked to the MangaBaka entry canonical to whether they are
// on hold. The worker sends the ones not on hold as one group.
func (s *Service) Linked(ctx context.Context, q db.Querier, userID, canonical string) (map[string]bool, error) {
	rows, err := db.Select[struct {
		ID   string `db:"id"`
		Held bool   `db:"held"`
	}](ctx, q, `
		SELECT s.id, COALESCE(h.held, false) AS held
		FROM content s
		JOIN libraries l ON l.id = s.library_id
		JOIN users u ON u.id = @user_id`+linkJoin+`
		LEFT JOIN scrobbles h ON h.user_id = @user_id AND h.provider = 'mangabaka' AND h.content_id = s.id
		WHERE `+switchesOn+` AND pe.canonical_id = @canonical
	`, pgx.NamedArgs{"user_id": userID, "canonical": canonical})
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Held
	}
	return out, err
}

// Unheld lists, sorted, the series of a Linked result that are not on hold.
func Unheld(linked map[string]bool) []string {
	var out []string
	for id, held := range linked {
		if !held {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// DropPending clears the pending and failed state of the scrobbles where selects, keeping holds
// and the record of what was sent. It returns the users whose rows it changed.
func (s *Service) DropPending(ctx context.Context, q db.Querier, where string, args ...any) ([]string, error) {
	users, err := db.SelectScalars[string](ctx, q, `
		WITH l AS (
			SELECT user_id, provider, content_id FROM scrobbles
			WHERE pending_since IS NOT NULL AND (`+where+`)
			ORDER BY user_id, provider, content_id FOR UPDATE
		)
		UPDATE scrobbles s SET pending_since = NULL, changed_at = NULL, due_at = NULL, error = NULL,
			attempts = 0, conflicts = 0, force = false
		FROM l WHERE (s.user_id, s.provider, s.content_id) = (l.user_id, l.provider, l.content_id)
		RETURNING s.user_id
	`, args...)
	return slices.Compact(slices.Sorted(slices.Values(users))), err
}

// ResetSent forgets what was last sent for the user, when the MangaBaka account changes or goes.
func (s *Service) ResetSent(ctx context.Context, q db.Querier, userID string) error {
	_, err := q.Exec(ctx, `
		WITH l AS (
			SELECT user_id, provider, content_id FROM scrobbles WHERE user_id = $1
			ORDER BY user_id, provider, content_id FOR UPDATE
		)
		UPDATE scrobbles s SET sent_at = NULL
		FROM l WHERE (s.user_id, s.provider, s.content_id) = (l.user_id, l.provider, l.content_id)
	`, userID)
	return err
}

// Reject pauses the user's scrobbling after MangaBaka refused tokenUsed, and tells them. A token
// replaced since the request was made stays as it is.
func (s *Service) Reject(ctx context.Context, userID, tokenUsed string, now time.Time) error {
	changed := false
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM users WHERE id = $1 FOR KEY SHARE", userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE scrobble_accounts SET rejected_at = $3
			WHERE user_id = $1 AND provider = 'mangabaka' AND token = $2 AND rejected_at IS NULL
		`, userID, tokenUsed, now)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		return notify.UpsertGrouped(ctx, tx, []notify.Grouped{{UserID: userID, Kind: models.NotificationScrobbleAuth,
			GroupKey: "scrobble_auth", Count: 1}}, false, now)
	})
	if err != nil || !changed {
		return err
	}
	s.onAuth([]string{userID})
	s.onChange([]string{userID})
	return nil
}
