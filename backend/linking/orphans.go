package linking

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"voltis/db"
	"voltis/metadata"

	"github.com/jackc/pgx/v5"
)

// FixOrphans deletes orphaned metadata and links, or moves them onto content, all or nothing. A
// link moves only onto content its provider can describe that has no link for it, or a
// disposable one; otherwise a disposable link is dropped, and any other fails the request.
func (s *Service) FixOrphans(ctx context.Context, libraryID string, del []string, move map[string]string) error {
	return s.run(ctx, func(o *op) error {
		if err := db.LockMetadata(ctx, o.tx, libraryID); err != nil {
			return err
		}
		srcs := slices.Concat(del, slices.Collect(maps.Keys(move)))
		dsts := slices.Collect(maps.Values(move))
		types, orphans, moves, err := prepareRepair(ctx, o.tx, libraryID, srcs, dsts, move)
		if err != nil {
			return err
		}
		if err := s.store.FixOrphans(ctx, o.tx, libraryID, orphans, moves); err != nil {
			return err
		}
		links, err := db.Select[Link](ctx, o.tx,
			"SELECT * FROM metadata_links WHERE library_id = $1 AND uri = ANY($2) ORDER BY uri, provider",
			libraryID, slices.Concat(srcs, dsts))
		if err != nil {
			return err
		}

		type key struct{ uri, provider string }
		at := map[key]Link{}
		for _, l := range links {
			at[key{l.URI, l.Provider}] = l
		}
		now := time.Now().UTC()
		var moved []Link
		for _, l := range links {
			dst, ok := move[l.URI]
			if !ok {
				continue
			}
			p, known := s.reg.Get(l.Provider)
			d, taken := at[key{dst, l.Provider}]
			var err error
			switch {
			case !known || len(p.Kinds(types[dst])) == 0:
				err = &metadata.ValidationError{Field: dst, Msg: fmt.Sprintf("%s cannot describe a %s", l.Provider, types[dst])}
			case taken && !d.disposable():
				err = fmt.Errorf("%w: %s has a %s link", metadata.ErrOccupied, dst, l.Provider)
			}
			if err != nil {
				if l.disposable() {
					continue // it has nothing to keep
				}
				return err
			}
			// The revision moves past both rows', so no stale expect_rev matches it.
			l.URI, l.Rev = dst, max(l.Rev, d.Rev)
			if l.State == StateReview || l.State == StateUnmatched {
				l.RetryAt = &now // matching reads another series now
			}
			at[key{dst, l.Provider}] = l
			moved = append(moved, l)
		}
		if _, err := o.tx.Exec(ctx, "DELETE FROM metadata_links WHERE library_id = $1 AND uri = ANY($2)", libraryID, srcs); err != nil {
			return err
		}
		for _, l := range moved {
			if err := save(ctx, o.tx, l); err != nil {
				return err
			}
		}
		o.changed[libraryID] = true
		for _, dst := range dsts {
			o.touch(libraryID, dst)
		}
		return nil
	})
}

// prepareRepair checks a repair against the library's content: sources must not be series, and
// destinations must have content. It returns the content types by URI, and the metadata to delete
// and the overrides to move: those of the sources without content, as a leaf keeps its metadata
// and only its links are orphans.
func prepareRepair(ctx context.Context, tx pgx.Tx, libraryID string, srcs, dsts []string, move map[string]string) (
	types map[string]string, orphans []string, moves map[string]string, err error) {
	type content struct {
		URI  string `db:"uri"`
		Type string `db:"type"`
	}
	rows, err := db.Select[content](ctx, tx, "SELECT uri, type FROM content WHERE library_id = $1 AND uri = ANY($2)",
		libraryID, slices.Concat(srcs, dsts))
	if err != nil {
		return nil, nil, nil, err
	}
	types = map[string]string{}
	for _, r := range rows {
		types[r.URI] = r.Type
	}
	for _, uri := range srcs {
		t, live := types[uri]
		if slices.Contains(metadata.SeriesTypes, t) {
			return nil, nil, nil, &metadata.ValidationError{Field: uri, Msg: "has content"}
		}
		if !live {
			orphans = append(orphans, uri)
		}
	}
	moves = map[string]string{}
	for src, dst := range move {
		if _, live := types[dst]; !live {
			return nil, nil, nil, &metadata.ValidationError{Field: dst, Msg: "no content"}
		}
		if slices.Contains(orphans, src) {
			moves[src] = dst
		}
	}
	return types, orphans, moves, nil
}
