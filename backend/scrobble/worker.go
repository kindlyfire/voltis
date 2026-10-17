package scrobble

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/providers/mangabaka"

	"github.com/jackc/pgx/v5"
)

const (
	maxSleep  = 5 * time.Minute // a safety net for a wake that never came
	roundRows = 50
	// conflictLimit consecutive entry conflicts fail a scrobble.
	conflictLimit   = 3
	conflictMessage = "MangaBaka entry changed while sending"
	// backoff is the delay after a transient failure: doubling per attempt, at most 64 minutes.
	backoff = `@now::timestamptz + interval '1 minute' * 2 ^ least(attempts, 6)`
)

// Run sends due scrobbles until ctx ends.
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		next, err := s.step(ctx, s.clock())
		wait := maxSleep
		if !next.IsZero() {
			wait = min(time.Until(next), maxSleep)
		}
		if err != nil && ctx.Err() == nil {
			slog.Error("[scrobble] round failed", "err", err)
			wait = time.Minute
		}
		select {
		case <-ctx.Done():
		case <-s.wake:
		case <-time.After(wait):
		}
	}
}

// due is a pending scrobble with the account that sends it.
type due struct {
	UserID    string     `db:"user_id"`
	ContentID string     `db:"content_id"`
	ChangedAt *time.Time `db:"changed_at"` // the version a write-back applies to
	Force     bool       `db:"force"`
	Token     string     `db:"token"`
	Private   bool       `db:"private_new"`
}

// group is the series of a user linked to one MangaBaka entry, which are sent together.
type group struct {
	id      string
	pending []due
	series  []string // every member, the pending ones included
}

// step sends a round of due scrobbles, and returns when the next one falls due (zero for none).
func (s *Service) step(ctx context.Context, now time.Time) (time.Time, error) {
	rows, err := db.Select[due](ctx, s.pool, `
		SELECT s.user_id, s.content_id, s.changed_at, s.force, a.token, a.private_new
		FROM scrobbles s JOIN scrobble_accounts a USING (user_id, provider)
		WHERE s.due_at <= $1 AND NOT s.held AND s.error IS NULL AND a.rejected_at IS NULL
		ORDER BY s.due_at LIMIT $2
	`, now, roundRows)
	if err != nil {
		return time.Time{}, err
	}
	byUser := map[string][]due{}
	for _, r := range rows {
		byUser[r.UserID] = append(byUser[r.UserID], r)
	}
	users := slices.Sorted(maps.Keys(byUser))
	if len(users) > 0 {
		defer s.onChange(users)
	}
	for _, user := range users {
		if err := s.sendUser(ctx, user, byUser[user]); err != nil {
			return time.Time{}, err
		}
	}
	next, err := db.SelectScalar[*time.Time](ctx, s.pool, `
		SELECT min(s.due_at) FROM scrobbles s JOIN scrobble_accounts a USING (user_id, provider)
		WHERE s.due_at IS NOT NULL AND NOT s.held AND s.error IS NULL AND a.rejected_at IS NULL
	`)
	if err != nil || next == nil {
		return time.Time{}, err
	}
	return *next, nil
}

// errStop ends a user's round: the token was refused, or replaced since the round started.
var errStop = errors.New("scrobble: stop the user's round")

func (s *Service) sendUser(ctx context.Context, user string, rows []due) error {
	tok, private := rows[0].Token, rows[0].Private
	canonical, err := eligible(ctx, s.pool, user, "s.id = ANY(@ids)",
		pgx.NamedArgs{"ids": fp.Map(rows, func(r due) string { return r.ContentID })})
	if err != nil {
		return err
	}
	groups := map[string]*group{}
	var drop []due
	for _, r := range rows {
		c, ok := canonical[r.ContentID]
		if !ok {
			drop = append(drop, r)
			continue
		}
		if groups[c] == nil {
			groups[c] = &group{id: c}
		}
		groups[c].pending = append(groups[c].pending, r)
	}
	if err := s.drop(ctx, user, tok, drop); err != nil || len(groups) == 0 {
		return err
	}
	ids := slices.Sorted(maps.Keys(groups))
	// Every other series linked to the same entry is sent with it, pending or not.
	members, err := eligible(ctx, s.pool, user, "pe.canonical_id = ANY(@canonical) AND "+notHeld,
		pgx.NamedArgs{"canonical": ids})
	if err != nil {
		return err
	}
	for _, g := range groups {
		g.series = fp.Map(g.pending, func(r due) string { return r.ContentID })
	}
	for id, c := range members {
		if !slices.Contains(groups[c].series, id) {
			groups[c].series = append(groups[c].series, id)
		}
	}

	// A round has at most roundRows rows, so ids fit in one batch.
	entries, err := s.lib.Entries(ctx, tok, ids)
	if err != nil {
		var covered []due
		for _, g := range groups {
			covered = append(covered, g.pending...)
		}
		return ignoreStop(s.failed(ctx, user, tok, covered, err, http.MethodGet))
	}
	for _, id := range ids {
		var remote *mangabaka.Entry
		if e, ok := entries[id]; ok {
			remote = &e
		}
		if err := s.sendGroup(ctx, user, tok, private, groups[id], remote); err != nil {
			return ignoreStop(err)
		}
	}
	return nil
}

func ignoreStop(err error) error {
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}

// errNotSent aborts a write that a recheck stopped.
var errNotSent = errors.New("scrobble: not sent")

func (s *Service) sendGroup(ctx context.Context, user, tok string, private bool, g *group,
	remote *mangabaka.Entry) error {
	for len(g.pending) > 0 {
		values, err := Compute(ctx, s.pool, user, g.series)
		if err != nil {
			return err
		}
		ours := Merge(slices.Collect(maps.Values(values))...)
		msg := OutOfRange(ours)
		force := slices.ContainsFunc(g.pending, func(r due) bool { return r.Force })
		w, create, ok := Decide(ours, remote, force, private)
		if msg != "" || !ok {
			// Only for the current group: the number may be a peer's that left it, and an ack
			// applies to the entry read, not after a relink.
			if regroup, err := s.recheck(ctx, user, tok, g); err != nil {
				return err
			} else if regroup {
				continue
			}
			if msg != "" {
				return s.update(ctx, user, tok, g.pending, "error = @msg, due_at = NULL", pgx.NamedArgs{"msg": msg})
			}
			return s.ack(ctx, user, tok, g.pending, false)
		}

		write, method := s.lib.Update, http.MethodPatch
		if create {
			write, method = s.lib.Create, http.MethodPost
		}
		var regroup bool
		var checkErr error
		err = write(ctx, tok, g.id, w, func() error {
			regroup, checkErr = s.recheck(ctx, user, tok, g)
			if checkErr != nil || regroup {
				return errNotSent
			}
			return nil
		})
		switch {
		case checkErr != nil:
			return checkErr
		case regroup:
			continue
		case err == nil:
			return s.ack(ctx, user, tok, g.pending, true)
		}
		return s.failed(ctx, user, tok, g.pending, err, method)
	}
	return nil
}

// recheck runs right before a write or an ack without one: it stops the round when the account changed, and brings the
// group up to date with the series that were held, released, switched off, unlinked or relinked since. Pending rows
// that are no longer eligible are dropped; relinked ones stay pending for the next round. It
// reports whether the group changed.
func (s *Service) recheck(ctx context.Context, user, tok string, g *group) (bool, error) {
	rows, err := db.Select[struct {
		ID        string  `db:"id"`
		Canonical *string `db:"canonical_id"` // nil: no longer eligible
		Held      bool    `db:"held"`
		Live      bool    `db:"live"`
	}](ctx, s.pool, `
		SELECT r.id, e.canonical_id, COALESCE(h.held, false) AS held,
			EXISTS (SELECT 1 FROM scrobble_accounts WHERE user_id = @user_id AND provider = 'mangabaka'
				AND token = @token AND rejected_at IS NULL) AS live
		FROM unnest(@ids::text[]) r(id)
		LEFT JOIN (
			SELECT s.id, pe.canonical_id
			FROM content s
			JOIN libraries l ON l.id = s.library_id
			JOIN users u ON u.id = @user_id`+linkJoin+`
			WHERE s.id = ANY(@ids) AND `+switchesOn+`
		) e ON e.id = r.id
		LEFT JOIN scrobbles h ON h.user_id = @user_id AND h.provider = 'mangabaka' AND h.content_id = r.id
	`, pgx.NamedArgs{"user_id": user, "token": tok, "ids": fp.Map(g.pending, func(r due) string { return r.ContentID })})
	if err != nil {
		return false, err
	}
	if len(rows) > 0 && !rows[0].Live {
		return false, errStop
	}
	canonical, held := map[string]string{}, []string{}
	for _, r := range rows {
		if r.Canonical != nil {
			canonical[r.ID] = *r.Canonical
		}
		if r.Held {
			held = append(held, r.ID)
		}
	}
	stays := func(id string) bool { return canonical[id] == g.id && !slices.Contains(held, id) }
	var drop, pending []due
	for _, r := range g.pending {
		switch _, ok := canonical[r.ContentID]; {
		case stays(r.ContentID):
			pending = append(pending, r)
		case !ok && !slices.Contains(held, r.ContentID):
			drop = append(drop, r)
		}
	}
	// The members are read again, not just filtered: a held series may have been released.
	linked, err := s.Linked(ctx, s.pool, user, g.id)
	if err != nil {
		return false, err
	}
	series := Unheld(linked)
	slices.Sort(g.series)
	if len(pending) == len(g.pending) && slices.Equal(series, g.series) {
		return false, nil
	}
	g.pending, g.series = pending, series
	return true, s.drop(ctx, user, tok, drop)
}

// failed records a failed request for the rows it was made for. The backoff runs from the
// failure, not the round's start: a round can wait long on the rate limiter.
func (s *Service) failed(ctx context.Context, user, tok string, rows []due, err error, method string) error {
	now := s.clock()
	args := pgx.NamedArgs{"now": now}
	switch mangabaka.Classify(err, method) {
	case mangabaka.Rejected:
		if err := s.Reject(ctx, user, tok, now); err != nil {
			return err
		}
		return errStop
	case mangabaka.Transient:
		slog.Warn("[scrobble] request failed, retrying later", "user", user, "err", err)
		return s.update(ctx, user, tok, rows, "attempts = attempts + 1, conflicts = 0, due_at = "+backoff, args)
	case mangabaka.Conflict:
		args["msg"], args["max_conflicts"] = conflictMessage, conflictLimit-1
		return s.update(ctx, user, tok, rows, `attempts = attempts + 1, conflicts = conflicts + 1,
			error = CASE WHEN conflicts >= @max_conflicts THEN @msg END,
			due_at = CASE WHEN conflicts < @max_conflicts THEN `+backoff+` END`, args)
	}
	msg := err.Error()
	if e, ok := errors.AsType[*mangabaka.Error](err); ok {
		msg = e.Message
	}
	args["msg"] = msg
	return s.update(ctx, user, tok, rows, "error = @msg, due_at = NULL", args)
}

// ack marks the rows sent, with sent_at the time after the write when one was made. A row changed while the request
// was in flight only records the time, and stays due.
func (s *Service) ack(ctx context.Context, user, tok string, rows []due, wrote bool) error {
	const done = "pending_since = NULL, due_at = NULL, changed_at = NULL, force = false, attempts = 0, conflicts = 0"
	if !wrote {
		return s.update(ctx, user, tok, rows, done, nil)
	}
	return s.writeBack(ctx, user, tok, rows, func(tx pgx.Tx, args pgx.NamedArgs) error {
		args["now"] = s.clock()
		// Changed rows first: the full ack below clears the version they're told apart by.
		if _, err := tx.Exec(ctx, `UPDATE scrobbles s SET sent_at = @now
			FROM unnest(@ids::text[], @seen::timestamptz[]) r(id, seen)
			WHERE s.user_id = @user_id AND s.provider = 'mangabaka' AND s.content_id = r.id
				AND s.changed_at IS DISTINCT FROM r.seen`, args); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE scrobbles s SET sent_at = @now, `+done+`
			FROM unnest(@ids::text[], @seen::timestamptz[]) r(id, seen)
			WHERE s.user_id = @user_id AND s.provider = 'mangabaka' AND s.content_id = r.id AND s.changed_at = r.seen`, args)
		return err
	})
}

// drop clears the pending rows of series that can't be sent, unless they changed since.
func (s *Service) drop(ctx context.Context, user, tok string, rows []due) error {
	if len(rows) == 0 {
		return nil
	}
	return s.writeBack(ctx, user, tok, rows, func(tx pgx.Tx, args pgx.NamedArgs) error {
		_, err := s.DropPending(ctx, tx, `user_id = $1 AND provider = 'mangabaka'
			AND (content_id, changed_at) IN (SELECT * FROM unnest($2::text[], $3::timestamptz[]))`,
			user, args["ids"], args["seen"])
		return err
	})
}

// update sets set on the rows still at the version seen.
func (s *Service) update(ctx context.Context, user, tok string, rows []due, set string, extra pgx.NamedArgs) error {
	return s.writeBack(ctx, user, tok, rows, func(tx pgx.Tx, args pgx.NamedArgs) error {
		maps.Copy(args, extra)
		_, err := tx.Exec(ctx, `UPDATE scrobbles s SET `+set+`
			FROM unnest(@ids::text[], @seen::timestamptz[]) r(id, seen)
			WHERE s.user_id = @user_id AND s.provider = 'mangabaka' AND s.content_id = r.id AND s.changed_at = r.seen`, args)
		return err
	})
}

// writeBack runs fn on the rows in a transaction, only while the account still has the token the
// rows were sent with. FOR SHARE waits out a reconnect or disconnect, or makes them wait: a write
// back of the old token never lands on the new account's rows. The rows lock in key order.
// fn gets @user_id, @ids and @seen.
func (s *Service) writeBack(ctx context.Context, user, tok string, rows []due,
	fn func(tx pgx.Tx, args pgx.NamedArgs) error) error {
	args := pgx.NamedArgs{"user_id": user, "ids": fp.Map(rows, func(r due) string { return r.ContentID }),
		"seen": fp.Map(rows, func(r due) *time.Time { return r.ChangedAt })}
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		ok, err := db.SelectScalars[int](ctx, tx, `SELECT 1 FROM scrobble_accounts
			WHERE user_id = $1 AND provider = 'mangabaka' AND token = $2 FOR SHARE`, user, tok)
		if err != nil || len(ok) == 0 {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM scrobbles WHERE user_id = @user_id AND provider = 'mangabaka'
			AND content_id = ANY(@ids) ORDER BY content_id FOR UPDATE`, args); err != nil {
			return err
		}
		return fn(tx, args)
	})
}
