package linking

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"voltis/db"
	"voltis/metadata"

	"github.com/jackc/pgx/v5"
)

// undos keeps the links as admins' decisions found them, for a minute, for Undo.
type undos struct {
	mu  sync.Mutex
	m   map[linkKey]undo
	now func() time.Time
}

type linkKey struct{ LibraryID, URI, Provider string }

// undo is a link before a decision, and the revision the decision saved.
type undo struct {
	prior Link
	rev   int64
	at    time.Time
}

var ErrNoUndo = errors.New("can no longer undo: it expired or changed since")

const undoWindow = time.Minute

// put keeps the undos of committed decisions, dropping expired ones.
func (u *undos) put(decided map[linkKey]undo) {
	if len(decided) == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.now()
	maps.DeleteFunc(u.m, func(_ linkKey, d undo) bool { return now.Sub(d.at) > undoWindow })
	for k, d := range decided {
		// Decisions commit in rev order but may be published out of it.
		if old, ok := u.m[k]; ok && old.rev > d.rev {
			continue
		}
		d.at = now
		u.m[k] = d
	}
}

func (u *undos) get(k linkKey, rev int64) (undo, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	d, ok := u.m[k]
	return d, ok && d.rev == rev && u.now().Sub(d.at) <= undoWindow
}

// remove drops an undo once carried out, unless a new decision replaced it: revs restart after
// deleting a row, so the rev alone does not tell.
func (u *undos) remove(k linkKey, d undo) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.m[k].rev == d.rev && u.m[k].at.Equal(d.at) {
		delete(u.m, k)
	}
}

// decide writes an admin's decision, keeping the link as it found it for Undo. It returns the
// revision it saved.
func (s *Service) decide(ctx context.Context, o *op, t metadata.Target, provider string, expect Expect,
	change func(*Link) error) (int64, error) {
	var d undo
	err := s.write(ctx, o, t, provider, expect, func(l *Link) error {
		prior := *l
		prior.Rejected, prior.Candidates = slices.Clone(l.Rejected), slices.Clone(l.Candidates)
		if err := change(l); err != nil {
			return err
		}
		d = undo{prior: prior, rev: l.Rev + 1} // save writes the next revision
		return nil
	})
	if err != nil {
		return 0, err
	}
	o.decided[linkKey{t.LibraryID, t.URI, provider}] = d
	return d.rev, nil
}

// Undo restores a link as the decision that saved rev found it, unless the link changed since or
// the undo expired. Entries the decision published stay, for the collector.
func (s *Service) Undo(ctx context.Context, contentID, provider string, rev int64) error {
	if _, _, err := s.target(ctx, contentID, provider); err != nil {
		return err
	}
	var k linkKey
	var d undo
	err := s.run(ctx, func(o *op) error {
		// A linked row it restores must not miss entries published meanwhile.
		if err := db.LockProvider(ctx, o.tx, provider); err != nil {
			return err
		}
		t, err := s.store.Lock(ctx, o.tx, contentID)
		if err != nil {
			return err
		}
		k = linkKey{t.LibraryID, t.URI, provider}
		var ok bool
		if d, ok = s.undos.get(k, rev); !ok {
			return ErrNoUndo
		}
		// MatchNow updates rows without the metadata lock.
		l, err := db.SelectOne[Link](ctx, o.tx, "SELECT * FROM metadata_links WHERE library_id = $1 AND uri = $2 AND provider = $3 FOR UPDATE",
			t.LibraryID, t.URI, provider)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && l.Rev != rev {
			return ErrNoUndo
		}
		if err != nil {
			return err
		}
		o.wrote(t.LibraryID, t.URI)
		if d.prior.State == StateNone {
			_, err := o.tx.Exec(ctx, "DELETE FROM metadata_links WHERE library_id = $1 AND uri = $2 AND provider = $3",
				t.LibraryID, t.URI, provider)
			return err
		}
		p := d.prior
		// A retry made due since the decision stays due; the decision's own, not yet due, goes.
		if p.pending() && l.pending() && l.RetryAt != nil && !l.RetryAt.After(time.Now()) &&
			(p.RetryAt == nil || l.RetryAt.Before(*p.RetryAt)) {
			p.RetryAt = l.RetryAt
		}
		p.Rev = l.Rev
		return save(ctx, o.tx, p)
	})
	if err == nil {
		s.undos.remove(k, d)
	}
	return err
}
