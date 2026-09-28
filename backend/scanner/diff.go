package scanner

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"voltis/db"
)

type Fingerprint struct {
	ID       string
	Path     string     `db:"file_uri"`
	Mtime    *time.Time `db:"file_mtime"`
	Size     *int       `db:"file_size"`
	Valid    bool
	URIPart  string
	ParentID *string
}

func loadFingerprints(ctx context.Context, q db.Querier, libraryID string) ([]Fingerprint, error) {
	return db.Select[Fingerprint](ctx, q, `
		SELECT id, file_uri, file_mtime, file_size, valid, uri_part, parent_id
		FROM content
		WHERE library_id = $1 AND type IN ('comic', 'book') AND file_uri IS NOT NULL
	`, libraryID)
}

func loadSeries(ctx context.Context, q db.Querier, libraryID string) ([]SeriesRef, error) {
	return db.Select[SeriesRef](ctx, q, `
		SELECT id, uri, uri_part, type, file_uri
		FROM content
		WHERE library_id = $1 AND type IN ('comic_series', 'book_series')
	`, libraryID)
}

func changed(f FSFile, fp Fingerprint, exists, force bool) bool {
	return !exists || force || !fp.Valid ||
		f.Size != int64(deref(fp.Size)) ||
		!f.Mtime.Truncate(time.Millisecond).Equal(deref(fp.Mtime).Truncate(time.Millisecond))
}

type node struct {
	edges map[string]*node
	leaf  string
}

type coverage struct{ root *node }

func pathParts(path string) []string {
	if path == string(filepath.Separator) {
		return []string{""}
	}
	return strings.Split(path, string(filepath.Separator))
}

func newCoverage(fps map[string]Fingerprint) *coverage {
	c := &coverage{root: &node{edges: map[string]*node{}}}
	for path, fp := range fps {
		n := c.root
		for _, part := range pathParts(path) {
			next, ok := n.edges[part]
			if !ok {
				next = &node{edges: map[string]*node{}}
				n.edges[part] = next
			}
			n = next
		}
		n.leaf = fp.ID
	}
	return c
}

func (n *node) leaves(out []string) []string {
	if n.leaf != "" {
		out = append(out, n.leaf)
	}
	for _, child := range n.edges {
		out = child.leaves(out)
	}
	return out
}

func (c *coverage) listed(dir string, names []string) []string {
	if !filepath.IsAbs(dir) {
		return nil
	}
	n := c.root
	for _, part := range pathParts(dir) {
		next, ok := n.edges[part]
		if !ok {
			return nil
		}
		n = next
	}

	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}

	var out []string
	for name, child := range n.edges {
		if present[name] {
			continue
		}
		out = child.leaves(out)
		delete(n.edges, name)
	}
	slices.Sort(out)
	return out
}

func deref[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return v
}
