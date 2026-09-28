package linking

import (
	"context"
	"errors"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"

	"github.com/jackc/pgx/v5"
)

type State string

const (
	StateNone      State = "none" // no row: never attempted
	StateReview    State = "review"
	StateUnmatched State = "unmatched"
	StateLinked    State = "linked"
	StateIgnored   State = "ignored"
)

type Origin string

const (
	OriginAuto   Origin = "auto"
	OriginManual Origin = "manual"
)

// Link is a metadata_links row: where a series stands with one provider.
type Link struct {
	LibraryID  string      `db:"library_id"`
	URI        string      `db:"uri"`
	Provider   string      `db:"provider"`
	State      State       `db:"state"`
	ExternalID *string     `db:"external_id"`
	Origin     *Origin     `db:"origin"`
	Candidates []Candidate `db:"candidates"`
	Rejected   []string    `db:"rejected"`
	RetryAt    *time.Time  `db:"retry_at"`
	Attempts   int         `db:"attempts"`
	LastError  *string     `db:"last_error"`
	Rev        int64       `db:"rev"`
	UpdatedAt  time.Time   `db:"updated_at"`
}

var ErrLinked = errors.New("a linked series changes only by linking, rejecting or ignoring")

// LinkTo links an entry; a different entry linked before is rejected.
func (l *Link) LinkTo(id string, origin Origin) {
	if l.State == StateLinked && *l.ExternalID != id {
		l.Rejected = append(l.Rejected, *l.ExternalID)
	}
	l.Rejected = slices.DeleteFunc(l.Rejected, func(r string) bool { return r == id })
	l.settle(StateLinked, &id, &origin)
}

// Ignore is an admin's decision that the provider has no entry; a linked one is rejected.
func (l *Link) Ignore() {
	if l.State == StateLinked {
		l.Rejected = fp.Dedup(append(l.Rejected, *l.ExternalID))
	}
	l.settle(StateIgnored, nil, nil)
}

// Reject is an admin's decision that none of the entries fits, nor a linked one. Matching tries
// again on the schedule of finding nothing, leaving them out.
func (l *Link) Reject(ids []string, now time.Time) {
	if l.State == StateLinked {
		ids = append(ids, *l.ExternalID)
	}
	l.Rejected = fp.Dedup(append(l.Rejected, ids...))
	l.nothing(now)
}

// Match records the matcher's decision: an entry to link, candidates to review, or nothing, which
// is tried again in 30 days. It leaves ignored, as only an admin's Rematch runs it there.
func (l *Link) Match(d MatchDecision, now time.Time) error {
	switch {
	case l.State == StateLinked:
		return ErrLinked
	case d.Link != nil:
		l.LinkTo(d.Link.ID, OriginAuto)
	case len(d.Candidates) > 0:
		l.settle(StateReview, nil, nil)
		l.Candidates = d.Candidates
	default:
		l.nothing(now)
	}
	return nil
}

// nothing leaves the series unmatched, to try again in 30 days.
func (l *Link) nothing(now time.Time) {
	l.settle(StateUnmatched, nil, nil)
	l.RetryAt = new(now.Add(30 * 24 * time.Hour))
}

// MatchFailed records a failed attempt, tried again after 1h·2ⁿ, at most a week.
func (l *Link) MatchFailed(err error, now time.Time) error {
	if l.State == StateLinked {
		return ErrLinked
	}
	n := l.Attempts
	l.settle(StateUnmatched, nil, nil)
	l.Attempts, l.LastError = n+1, new(err.Error())
	l.RetryAt = new(now.Add(time.Hour * time.Duration(min(1<<min(n, 8), 168))))
	return nil
}

// revision is the rev clients see and send back: none while there is no row.
func (l Link) revision() *int64 {
	if l.State == StateNone {
		return nil
	}
	return &l.Rev
}

func (l *Link) settle(s State, id *string, origin *Origin) {
	l.State, l.ExternalID, l.Origin = s, id, origin
	l.Candidates, l.RetryAt, l.Attempts, l.LastError = nil, nil, 0, nil
}

// disposable reports whether the link records no admin decision: pending, rejecting nothing.
// It matches the negation of metadata.KeptLink.
func (l Link) disposable() bool {
	return l.pending() && len(l.Rejected) == 0
}

func (l Link) pending() bool { return l.State == StateReview || l.State == StateUnmatched }

// Expect guards a write against a link changed since it was read.
type Expect struct {
	Rev         *int64 // nil = expect no row
	Fingerprint string // background matching: the MatchQuery it decided on
}

func (e Expect) holds(l Link) bool {
	if e.Rev == nil {
		return l.State == StateNone
	}
	return l.State != StateNone && l.Rev == *e.Rev
}

func readLink(ctx context.Context, q db.Querier, libraryID, uri, provider string) (Link, error) {
	l, err := db.SelectOne[Link](ctx, q,
		"SELECT * FROM metadata_links WHERE library_id = $1 AND uri = $2 AND provider = $3", libraryID, uri, provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{LibraryID: libraryID, URI: uri, Provider: provider, State: StateNone}, nil
	}
	return l, err
}

// save writes a link at its next revision.
func save(ctx context.Context, tx pgx.Tx, l Link) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin, candidates, rejected,
			retry_at, attempts, last_error, rev, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::jsonb, '[]'), COALESCE($8::text[], '{}'), $9, $10, $11, $12, now())
		ON CONFLICT (library_id, uri, provider) DO UPDATE SET
			state = EXCLUDED.state, external_id = EXCLUDED.external_id, origin = EXCLUDED.origin,
			candidates = EXCLUDED.candidates, rejected = EXCLUDED.rejected, retry_at = EXCLUDED.retry_at,
			attempts = EXCLUDED.attempts, last_error = EXCLUDED.last_error, rev = EXCLUDED.rev,
			updated_at = EXCLUDED.updated_at
	`, l.LibraryID, l.URI, l.Provider, l.State, l.ExternalID, l.Origin, l.Candidates, l.Rejected,
		l.RetryAt, l.Attempts, l.LastError, l.Rev+1)
	return err
}
