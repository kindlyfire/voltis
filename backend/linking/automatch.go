package linking

import (
	"fmt"
	"slices"
	"strings"

	"voltis/models"
)

// autoMatch is which series of a library a provider matches automatically: those with a leaf
// whose most specific source resolves it on. Leaves under no source, and series without leaves,
// take the library value.
type autoMatch struct {
	Library bool
	Scopes  []scope // every source with a prefix, longest first; empty when all resolve like Library
}

type scope struct {
	Prefix string
	On     bool
}

func autoMatchOf(lib models.Library, provider string) autoMatch {
	settings := models.ParseLibrarySettings(lib.Settings)
	a := autoMatch{Library: settings.AutoMatch[provider]}
	for _, src := range models.ParseLibrarySources(lib.Sources) {
		if p, ok := src.Prefix(); ok {
			a.Scopes = append(a.Scopes, scope{p, settings.Resolve(src.Settings).AutoMatch[provider]})
		}
	}
	if !slices.ContainsFunc(a.Scopes, func(s scope) bool { return s.On != a.Library }) {
		return autoMatch{Library: a.Library}
	}
	// Ties keep source order: only duplicates stored before the upsert rejected them tie.
	slices.SortStableFunc(a.Scopes, func(x, y scope) int { return len(y.Prefix) - len(x.Prefix) })
	return a
}

func (a autoMatch) any() bool {
	return a.Library || slices.ContainsFunc(a.Scopes, func(s scope) bool { return s.On })
}

// at is the value for a leaf at path.
func (a autoMatch) at(path string) bool {
	for _, s := range a.Scopes {
		if strings.HasPrefix(path, s.Prefix) {
			return s.On
		}
	}
	return a.Library
}

// AutoMatchGrew reports whether saving library before as after may have made some series match
// automatically with the provider.
func AutoMatchGrew(before, after models.Library, provider string) bool {
	return grew(autoMatchOf(before, provider), autoMatchOf(after, provider))
}

// grew reports whether some leaf may match with after but not before. It probes each prefix of
// either, and a path under none: a leaf resolves like the longest probe containing it, so no
// growth is missed, though a probe without leaves may report some that isn't.
func grew(before, after autoMatch) bool {
	probes := []string{""}
	for _, s := range slices.Concat(before.Scopes, after.Scopes) {
		probes = append(probes, s.Prefix)
	}
	return slices.ContainsFunc(probes, func(p string) bool { return !before.at(p) && after.at(p) })
}

// sql is the condition that series c is covered, with its arguments numbered from $n. There is
// none on the fast path, where every series takes the library value; callers skip the query when
// that is off. A leaf resolves through a CASE on the prefixes, longest first, so the first match
// is the most specific. Only leaves under a prefix that resolves unlike the library change a
// series' value: with the library off, a series needs such a leaf, found by prefix range through
// idx_content_file_uri; with it on, one that resolves off, unless another resolves on. Both are
// semi or anti joins, which the planner orders after the cheaper conditions, and hashes when the
// prefixes hold few leaves.
func (a autoMatch) sql(n int) (string, []any) {
	if len(a.Scopes) == 0 {
		return "", nil
	}
	args := []any{a.Library}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", n+len(args)-1)
	}
	var cases, ranges []string
	for _, s := range a.Scopes {
		p := arg(s.Prefix)
		cases = append(cases, fmt.Sprintf("WHEN starts_with(%%[1]s.file_uri, %s) THEN %s::boolean", p, arg(s.On)))
		if s.On != a.Library {
			// Prefixes end in "/", so the next string past them ends in "0".
			ranges = append(ranges, fmt.Sprintf(`k.file_uri COLLATE "C" >= %s AND k.file_uri COLLATE "C" < %s`, p,
				arg(s.Prefix[:len(s.Prefix)-1]+"0")))
		}
	}
	resolved := "CASE " + strings.Join(cases, " ") + fmt.Sprintf(" ELSE $%d::boolean END", n)
	differing := "SELECT 1 FROM content k WHERE k.parent_id = c.id AND (" + strings.Join(ranges, " OR ") + ") AND "
	if !a.Library {
		return " AND EXISTS (" + differing + fmt.Sprintf(resolved, "k") + ")", args
	}
	return " AND NOT EXISTS (" + differing + "NOT " + fmt.Sprintf(resolved, "k") +
		" AND NOT EXISTS (SELECT 1 FROM content k2 WHERE k2.parent_id = k.parent_id AND " + fmt.Sprintf(resolved, "k2") + "))", args
}
