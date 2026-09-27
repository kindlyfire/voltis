package metadata

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"voltis/lib/fp"

	"golang.org/x/text/runes"
	"golang.org/x/text/unicode/norm"
)

// Normalize cleans a file or provider layer. Blank strings and empty lists are never display
// data, so they become Absent, as do values that fail to parse.
func (f Fields) Normalize() Fields { return f.normalize(Absent) }

// normalize cleans every Value, turning those left blank or invalid into `empty`.
func (f Fields) normalize(empty Presence) Fields {
	for _, o := range []*Opt[string]{&f.Title, &f.Description, &f.Series, &f.Number, &f.Volume, &f.Imprint,
		&f.Format, &f.Web, &f.Notes, &f.ScanInformation, &f.BlackAndWhite, &f.SeriesGroup, &f.AlternateSeries,
		&f.AlternateNumber, &f.Manga} {
		*o = str(*o, empty, strings.TrimSpace)
	}
	f.Language = str(f.Language, empty, func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
	})
	f.PublicationDate = str(f.PublicationDate, empty, ParsePartialDate)
	for _, o := range []*Opt[[]string]{&f.AltTitles, &f.Publishers, &f.Tags} {
		*o = list(*o, empty, func(vs []string) []string {
			return dedupeBy(fp.Map(vs, strings.TrimSpace), strings.ToLower)
		})
	}
	f.Genres = list(f.Genres, empty, func(vs []string) []string {
		var slugs []string
		for _, v := range vs {
			slugs = append(slugs, GenreSlug(v)...)
		}
		return fp.Dedup(slugs)
	})
	f.Staff = list(f.Staff, empty, func(vs []Staff) []Staff {
		vs = fp.Map(vs, func(s Staff) Staff { return Staff{Name: strings.TrimSpace(s.Name), Role: snake(s.Role)} })
		return dedupeBy(vs, func(s Staff) string {
			if s.Name == "" {
				return ""
			}
			return strings.ToLower(s.Name) + "\x00" + s.Role
		})
	})
	f.Links = list(f.Links, empty, func(vs []Link) []Link {
		vs = fp.Map(vs, func(l Link) Link { return Link{Label: strings.TrimSpace(l.Label), URL: strings.TrimSpace(l.URL)} })
		return dedupeBy(vs, func(l Link) string { return l.URL })
	})
	f.ContentRating = oneOf(f.ContentRating, empty, ContentRatings)
	f.Status = oneOf(f.Status, empty, Statuses)
	f.Kind = oneOf(f.Kind, empty, Kinds)
	if f.Rating.P == Value && !(f.Rating.V >= 0 && f.Rating.V <= 100) {
		f.Rating = Opt[float64]{P: empty}
	}
	if f.Cover.P == Value && f.Cover.V.URL == "" {
		f.Cover = Opt[CoverRef]{P: empty}
	}
	return f
}

func str(o Opt[string], empty Presence, fn func(string) string) Opt[string] {
	if o.P != Value {
		return o
	}
	if v := fn(o.V); v != "" {
		return Val(v)
	}
	return Opt[string]{P: empty}
}

func list[T any](o Opt[[]T], empty Presence, fn func([]T) []T) Opt[[]T] {
	if o.P != Value {
		return o
	}
	if v := fn(o.V); len(v) > 0 {
		return Val(v)
	}
	return Opt[[]T]{P: empty}
}

func oneOf[T comparable](o Opt[T], empty Presence, valid []T) Opt[T] {
	if o.P == Value && !slices.Contains(valid, o.V) {
		return Opt[T]{P: empty}
	}
	return o
}

// dedupeBy keeps the first item per key, dropping items with a blank key.
func dedupeBy[T any](vs []T, key func(T) string) []T {
	seen := map[string]bool{}
	var out []T
	for _, v := range vs {
		if k := key(v); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

var separators = regexp.MustCompile(`[\s/-]+`)

// GenreSlug splits a genre list on commas and slugs each, so "Sci-Fi, Slice of Life" gives
// sci_fi and slice_of_life.
func GenreSlug(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if slug := snake(part); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

func snake(s string) string {
	return strings.Trim(separators.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "_"), "_")
}

// ParsePartialDate reads YYYY, YYYY-M, YYYY-M-D, and timestamps as canonical YYYY[-MM[-DD]], or
// "" when s is none of those.
func ParsePartialDate(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "T")
	s, _, _ = strings.Cut(s, " ")
	parts := strings.Split(s, "-")
	if len(parts) > 3 || len(parts[0]) != 4 {
		return ""
	}
	n := []int{0, 1, 1}
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 1 || strings.HasPrefix(p, "+") || (i > 0 && len(p) > 2) {
			return ""
		}
		n[i] = v
	}
	if n[1] > 12 || time.Date(n[0], time.Month(n[1]), n[2], 0, 0, 0, 0, time.UTC).Day() != n[2] {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", n[0], n[1], n[2])[:3*len(parts)+1]
}

var stripMarks = runes.Remove(runes.In(unicode.Mn))

// NormalizeTitle folds a title for comparison: accents and case dropped, "&" read as "and", and
// punctuation as spaces.
func NormalizeTitle(s string) string {
	s = stripMarks.String(norm.NFKD.String(s))
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

var ageRatings = map[string]ContentRating{
	"everyone": Safe, "early childhood": Safe, "everyone 10+": Safe, "kids to adults": Safe, "g": Safe, "pg": Safe,
	"teen": Suggestive, "ma15+": Suggestive, "m": Suggestive, "mature 17+": Suggestive,
	"adults only 18+": Erotica, "r18+": Erotica,
	"x18+": Pornographic,
}

// AgeRating maps a ComicInfo AgeRating to a content rating; unknown and pending ratings are Absent.
func AgeRating(s string) Opt[ContentRating] {
	if r, ok := ageRatings[strings.ToLower(strings.TrimSpace(s))]; ok {
		return Val(r)
	}
	return Opt[ContentRating]{}
}
