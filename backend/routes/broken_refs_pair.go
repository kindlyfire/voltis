package routes

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"voltis/scanner/keys"
)

// groupOf is a broken ref's group: the URI up to its second "/", so a series' children group
// under it. It must agree with the grp column of brokenRefsSQL.
func groupOf(uri string) string {
	if i := strings.IndexByte(uri, '/'); i >= 0 {
		if j := strings.IndexByte(uri[i+1:], '/'); j >= 0 {
			return uri[:i+1+j]
		}
	}
	return uri
}

// numKey is a child's number by kind, so a chapter never pairs with a volume of the same number.
type numKey struct {
	kind byte // 'c' chapter, 'v' volume, 'y' year, 'b' book volume
	n    float64
}

var yearPart = regexp.MustCompile(`^y(\d+)$`)

func partKey(prefix, part string) (numKey, bool) {
	switch prefix {
	case "comic":
		// The chapter wins, so v2_ch12 pairs with ch12.
		if n := keys.ParseChapter(part); n != nil {
			return numKey{'c', *n}, true
		}
		if n := keys.ParseVolume(part); n != nil {
			return numKey{'v', *n}, true
		}
		if m := yearPart.FindStringSubmatch(part); m != nil {
			n, _ := strconv.ParseFloat(m[1], 64)
			return numKey{'y', n}, true
		}
	case "book":
		if keys.IsBookSpecial(part) {
			return numKey{}, false
		}
		if _, n, ok := keys.ParseBookFileVolume(part); ok {
			return numKey{'b', n}, true
		}
	}
	return numKey{}, false
}

// partKeys memoises partKey: suggestions pair many targets against the same parts.
type partKeys map[[2]string]cachedKey

type cachedKey struct {
	k  numKey
	ok bool
}

func (pk partKeys) get(prefix, part string) (numKey, bool) {
	key := [2]string{prefix, part}
	c, seen := pk[key]
	if !seen {
		c.k, c.ok = partKey(prefix, part)
		pk[key] = c
	}
	return c.k, c.ok
}

type refPair struct {
	From, To, By string // By: root, exact, number or manual
}

type pairing struct {
	Pairs                  []refPair
	Leftovers, FreeTargets []string
}

// pairRefs pairs a group's distinct broken URIs (srcs) with the target to and its child parts.
// Overrides come first (nil unpairs), then the root, exact parts, and numbers held by exactly
// one source and one free target. Pairs and leftovers follow srcs' order.
func pairRefs(group string, srcs []string, to string, toParts []string, overrides map[string]*string, pk partKeys) (pairing, error) {
	byFrom := map[string]refPair{}
	taken := map[string]bool{}
	add := func(from, dst, by string) {
		byFrom[from] = refPair{from, dst, by}
		taken[dst] = true
	}
	// Paired, or overridden: a nil override leaves the source broken.
	handled := func(src string) bool {
		_, paired := byFrom[src]
		_, overridden := overrides[src]
		return paired || overridden
	}

	for _, src := range slices.Sorted(maps.Keys(overrides)) {
		if !slices.Contains(srcs, src) {
			return pairing{}, fmt.Errorf("%s isn't broken", src)
		}
		dst := overrides[src]
		if dst == nil {
			continue
		}
		if taken[*dst] {
			return pairing{}, fmt.Errorf("%s is the destination of two items", *dst)
		}
		add(src, *dst, "manual")
	}

	if slices.Contains(srcs, group) && !handled(group) && !taken[to] {
		add(group, to, "root")
	}

	prefix, _, _ := strings.Cut(group, "/")
	part := func(src string) (string, bool) { return strings.CutPrefix(src, group+"/") }
	parts := map[string]bool{}
	for _, p := range toParts {
		parts[p] = true
	}
	for _, src := range srcs {
		if p, ok := part(src); ok && !handled(src) && parts[p] && !taken[to+"/"+p] {
			add(src, to+"/"+p, "exact")
		}
	}

	srcKeys := map[numKey][]string{}
	for _, src := range srcs {
		if p, ok := part(src); ok && !handled(src) {
			if k, ok := pk.get(prefix, p); ok {
				srcKeys[k] = append(srcKeys[k], src)
			}
		}
	}
	dstKeys := map[numKey][]string{}
	for _, p := range toParts {
		if dst := to + "/" + p; !taken[dst] {
			if k, ok := pk.get(prefix, p); ok {
				dstKeys[k] = append(dstKeys[k], dst)
			}
		}
	}
	for k, s := range srcKeys {
		if d := dstKeys[k]; len(s) == 1 && len(d) == 1 {
			add(s[0], d[0], "number")
		}
	}

	res := pairing{Pairs: []refPair{}, Leftovers: []string{}, FreeTargets: []string{}}
	for _, src := range srcs {
		if p, ok := byFrom[src]; ok {
			res.Pairs = append(res.Pairs, p)
		} else {
			res.Leftovers = append(res.Leftovers, src)
		}
	}
	for _, p := range toParts {
		if dst := to + "/" + p; !taken[dst] {
			res.FreeTargets = append(res.FreeTargets, dst)
		}
	}
	return res, nil
}

type refSuggestion struct {
	URI     string `json:"uri"`
	Matched int    `json:"matched"`
	Total   int    `json:"total"`
	Same    bool   `json:"same"`
}

type targetRow struct {
	Root     string  `db:"root"`
	RootPart string  `db:"root_part"`
	Part     *string `db:"part"`
}

// targetIndex holds a library's top-level URIs and their child parts, for suggestions.
type targetIndex struct {
	keys      partKeys
	rootParts map[string]string   // root → its uri_part
	parts     map[string][]string // root → child parts
	byPart    map[string][]string // child part → roots
	byKey     map[numKey][]string // child number → roots
}

func newTargetIndex(rows []targetRow) *targetIndex {
	ix := &targetIndex{keys: partKeys{}, rootParts: map[string]string{}, parts: map[string][]string{},
		byPart: map[string][]string{}, byKey: map[numKey][]string{}}
	for _, r := range rows {
		ix.rootParts[r.Root] = r.RootPart
		if r.Part == nil {
			continue
		}
		ix.parts[r.Root] = append(ix.parts[r.Root], *r.Part)
		ix.byPart[*r.Part] = append(ix.byPart[*r.Part], r.Root)
		prefix, _, _ := strings.Cut(r.Root, "/")
		if k, ok := ix.keys.get(prefix, *r.Part); ok {
			ix.byKey[k] = append(ix.byKey[k], r.Root)
		}
	}
	return ix
}

// suggest ranks the roots a series group's children would pair with: up to three, those sharing a
// word of the group's name first, then by matched children and name prefix, after the group's
// own root when it still exists.
func (ix *targetIndex) suggest(group string, srcs []string) []refSuggestion {
	prefix, name, _ := strings.Cut(group, "/")
	candidates := map[string]bool{}
	total := 0
	for _, src := range srcs {
		p, ok := strings.CutPrefix(src, group+"/")
		if !ok {
			continue
		}
		total++
		roots := ix.byPart[p]
		if k, ok := ix.keys.get(prefix, p); ok {
			roots = append(slices.Clip(roots), ix.byKey[k]...)
		}
		for _, r := range roots {
			if r != group && strings.HasPrefix(r, prefix+"/") {
				candidates[r] = true
			}
		}
	}
	out := []refSuggestion{}
	if total == 0 {
		return out
	}
	matched := func(root string) int {
		p, _ := pairRefs(group, srcs, root, ix.parts[root], nil, ix.keys)
		n := 0
		for _, pr := range p.Pairs {
			if pr.By != "root" {
				n++
			}
		}
		return n
	}

	for r := range candidates {
		if m := matched(r); m > 0 {
			out = append(out, refSuggestion{URI: r, Matched: m, Total: total})
		}
	}
	words := nameWords(name)
	// Generic parts (v1, ch1) pair with any series, so a shared word outranks a higher count.
	sharesWord := map[string]int{}
	for _, s := range out {
		for w := range nameWords(ix.rootParts[s.URI]) {
			if words[w] {
				sharesWord[s.URI] = 1
				break
			}
		}
	}
	name = strings.ToLower(name)
	slices.SortFunc(out, func(a, b refSuggestion) int {
		return cmp.Or(cmp.Compare(sharesWord[b.URI], sharesWord[a.URI]),
			cmp.Compare(b.Matched, a.Matched),
			cmp.Compare(sharedPrefix(name, strings.ToLower(ix.rootParts[b.URI])),
				sharedPrefix(name, strings.ToLower(ix.rootParts[a.URI]))),
			strings.Compare(a.URI, b.URI))
	})
	out = out[:min(len(out), 3)]
	if _, ok := ix.rootParts[group]; ok {
		out = append([]refSuggestion{{URI: group, Matched: matched(group), Total: total, Same: true}}, out...)
	}
	return out
}

var yearWord = regexp.MustCompile(`^\d{4}$`)

// nameWords is a series name's case-folded words, without years and words under four letters (the, and, vol).
func nameWords(name string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if utf8.RuneCountInString(w) >= 4 && !yearWord.MatchString(w) {
			words[w] = true
		}
	}
	return words
}

// sharedPrefix is the number of leading runes a and b share.
func sharedPrefix(a, b string) int {
	n := 0
	for a != "" && b != "" {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			break
		}
		n++
		a, b = a[sa:], b[sb:]
	}
	return n
}
