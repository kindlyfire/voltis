package scrobble

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"voltis/db"
	"voltis/metadata"

	"github.com/jackc/pgx/v5"
)

const (
	quiet   = 5 * time.Minute  // a series is sent once its changes stop for this long
	maxWait = 15 * time.Minute // or at the latest this long after its first pending change
)

// Touches collects the content whose reading state a request changed, so that Flush queues it at
// the end of the transaction, and the users whose scrobbles then changed.
// It belongs to one request, so it isn't safe for concurrent use.
type Touches struct {
	pending map[string]map[string]bool // by user, content ids
	users   []string
}

type touchesKey struct{}

func WithTouches(ctx context.Context) (context.Context, *Touches) {
	t := &Touches{pending: map[string]map[string]bool{}}
	return context.WithValue(ctx, touchesKey{}, t), t
}

// Users are the users whose scrobbles a Flush changed, for Service.Changed after commit.
func (t *Touches) Users() []string {
	return slices.Clone(t.users)
}

var errNoTouches = errors.New("scrobble: no collector in the context; wrap it with WithTouches")

// Touch records that the user's completion or series status changed on the content, items or
// series. It runs no SQL: Flush queues the series.
func Touch(ctx context.Context, userID string, contentIDs ...string) error {
	t, ok := ctx.Value(touchesKey{}).(*Touches)
	if !ok {
		return errNoTouches
	}
	if t.pending[userID] == nil {
		t.pending[userID] = map[string]bool{}
	}
	for _, id := range contentIDs {
		t.pending[userID][id] = true
	}
	return nil
}

// Flush queues the touched series, user by user in order, as the last statement of the
// transaction that touched them.
func Flush(ctx context.Context, tx pgx.Tx, now time.Time) error {
	t, ok := ctx.Value(touchesKey{}).(*Touches)
	if !ok {
		return errNoTouches
	}
	for _, user := range slices.Sorted(maps.Keys(t.pending)) {
		queued, err := Queue(ctx, tx, user, slices.Sorted(maps.Keys(t.pending[user])), now, quiet)
		if err != nil {
			return err
		}
		if len(queued) > 0 && !slices.Contains(t.users, user) {
			t.users = append(t.users, user)
		}
	}
	clear(t.pending)
	return nil
}

// switchesOn holds for a series s (with its library l and user u) unless the admin turned
// scrobbling off for the library or the user did. It compares text, never casts, so a malformed
// preference can't fail a reading write.
const switchesOn = `(l.settings->>'scrobbling') IS DISTINCT FROM 'false'
	AND (u.preferences->'libraries'->s.library_id->>'scrobble') IS DISTINCT FROM 'false'`

// Queue marks the series of the content (items map to their series) pending for the user, due
// after delay, or 15 minutes after the first pending change at the latest. Series that aren't
// linked, are held, or are in a library with scrobbling off are left alone, as is everything
// when the user isn't connected. It returns the queued series.
//
// Callers outside the reading writes take lockUserData for the series' libraries first.
func Queue(ctx context.Context, tx pgx.Tx, userID string, contentIDs []string, now time.Time,
	delay time.Duration) ([]string, error) {
	// The account lock makes a concurrent disconnect wait, so its DropPending sees these rows.
	connected, err := LockAccount(ctx, tx, userID)
	if err != nil || !connected {
		return nil, err
	}
	args := pgx.NamedArgs{"user_id": userID, "ids": contentIDs, "series_types": metadata.SeriesTypes,
		"now": now, "delay": delay, "max_wait": maxWait}
	// A failed, idle or backed-off row starts a fresh window. Rows lock in id order, so two
	// transactions can't deadlock.
	return db.SelectScalars[string](ctx, tx, `
		INSERT INTO scrobbles (user_id, provider, content_id, pending_since, changed_at, due_at)
		SELECT @user_id, 'mangabaka', s.id, @now::timestamptz, @now::timestamptz,
			@now::timestamptz + @delay::interval
		FROM content s
		JOIN libraries l ON l.id = s.library_id
		JOIN users u ON u.id = @user_id
		JOIN metadata_links ml ON ml.content_id = s.id AND ml.provider = 'mangabaka' AND ml.state = 'linked'
		WHERE s.id IN (SELECT COALESCE(parent_id, id) FROM content
				WHERE id = ANY(@ids) AND (parent_id IS NOT NULL OR type = ANY(@series_types)))
			AND `+switchesOn+`
		ORDER BY s.id
		ON CONFLICT (user_id, provider, content_id) DO UPDATE SET
			pending_since = CASE WHEN scrobbles.error IS NOT NULL OR scrobbles.due_at IS NULL OR scrobbles.attempts > 0
				THEN EXCLUDED.pending_since ELSE COALESCE(scrobbles.pending_since, EXCLUDED.pending_since) END,
			changed_at = EXCLUDED.changed_at,
			due_at = LEAST(EXCLUDED.due_at,
				CASE WHEN scrobbles.error IS NOT NULL OR scrobbles.due_at IS NULL OR scrobbles.attempts > 0
					THEN EXCLUDED.pending_since ELSE COALESCE(scrobbles.pending_since, EXCLUDED.pending_since) END
				+ @max_wait::interval),
			error = NULL, attempts = 0, conflicts = 0
		WHERE NOT scrobbles.held
		RETURNING content_id
	`, args)
}

// LockAccount locks the user's account row against a disconnect or reconnect, after the users
// row: a user delete locks users and then cascades, and an insert's foreign key check would
// otherwise take the users lock after the account's. It reports whether the user is connected.
func LockAccount(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	if _, err := tx.Exec(ctx, "SELECT 1 FROM users WHERE id = $1 FOR KEY SHARE", userID); err != nil {
		return false, err
	}
	n, err := db.SelectScalars[int](ctx, tx,
		"SELECT 1 FROM scrobble_accounts WHERE user_id = $1 AND provider = 'mangabaka' FOR KEY SHARE", userID)
	return len(n) > 0, err
}
