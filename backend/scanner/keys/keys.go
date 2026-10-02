package keys

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// Textual markers need a non-letter before them; not \b, which counts "_" as a word character.
	volumePattern  = regexp.MustCompile(`(?i)(?:\#|(?:^|[^\p{L}])(?:v|vo|vol|volu|volum|volume)\.?)\s*(\d+(?:\.\d+)?)`)
	chapterPattern = regexp.MustCompile(`(?i)(?:c|ch|chap|chapt|chapte|chapter)\.?\s*(\d+(?:\.\d+)?)`)
	// Positions a chapter marker for ParseVolume; bounded like volumePattern so "Arc 2" isn't one.
	chapterIndexPattern = regexp.MustCompile(`(?:^|[^\p{L}])` + chapterPattern.String())
	numberPattern       = regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	yearPattern         = regexp.MustCompile(`\((\d+)\)`)
	trailingTags        = regexp.MustCompile(`\s*[\[\(][^\[\]\(\)]*[\]\)]\s*$`)

	// Book patterns take Unicode spaces (\s alone is ASCII-only), as titles often carry NBSPs.
	bookVolumePattern = regexp.MustCompile(`(?i)(?:^|[^\p{L}])((?:v|vol\.?|volume)[\s\p{Z}]*(\d+(?:\.\d+)?))`)
	// After the number: the end of the string, or a subtitle separator followed by text.
	bookVolumeEnd   = regexp.MustCompile(`^[\s\p{Z}]*(?:$|[:,][\s\p{Z}]*[^\s\p{Z}]|[\s\p{Z}]-[\s\p{Z}]+[^\s\p{Z}])`)
	bookVolumeRange = regexp.MustCompile(`^[\s\p{Z}]*[-–~][\s\p{Z}]*\d`)
	bookFileTags    = regexp.MustCompile(`[\s\p{Z}]*[\[\(\{][^\[\]\(\)\{\}]*[\]\)\}][\s\p{Z}]*$`)
	specialPattern  = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:sp\d+|(?:short|side)[\s\p{Z}]+stor(?:y|ies)|bonus|extra|exclusive)(?:$|[^\p{L}\p{N}])`)
)

// ParseVolume ignores a volume after the chapter marker, as in "c010 v02".
func ParseVolume(name string) *float64 {
	m := volumePattern.FindStringSubmatchIndex(name)
	if m == nil {
		return nil
	}
	if c := chapterIndexPattern.FindStringIndex(name); c != nil && m[0] >= c[0] {
		return nil
	}
	return parseFloat(name[m[2]:m[3]])
}

func ParseChapter(name string) *float64 {
	if m := chapterPattern.FindStringSubmatch(name); m != nil {
		return parseFloat(m[1])
	}
	return nil
}

func ParseFallbackChapter(name string) *float64 {
	matches := numberPattern.FindAllStringSubmatch(name, -1)
	if len(matches) == 0 {
		return nil
	}

	best := matches[0][1]
	for _, m := range matches[1:] {
		if digitCount(m[1]) > digitCount(best) {
			best = m[1]
		}
	}

	return parseFloat(best)
}

func parseFloat(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func digitCount(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n++
		}
	}
	return n
}

func ParseSeriesName(name string) (string, *int) {
	year := ParseSeriesYear(name)
	cleaned := CleanSeriesName(name)
	return cleaned, year
}

func ParseSeriesYear(name string) *int {
	matches := yearPattern.FindAllStringSubmatch(name, -1)
	for _, m := range slices.Backward(matches) {
		v, err := strconv.Atoi(m[1])
		if err == nil && v >= 1000 && v <= 9999 {
			return &v
		}
	}
	return nil
}

func CleanSeriesName(name string) string { return stripTags(trailingTags, name) }

func stripTags(tags *regexp.Regexp, name string) string {
	for {
		cleaned := tags.ReplaceAllString(name, "")
		if cleaned == name {
			break
		}
		name = cleaned
	}
	return strings.TrimSpace(name)
}

// ParseBookVolume finds a single volume marker ("Vol. 3", "Volume 3", "v03") that ends s or
// precedes a subtitle, and returns the non-empty series prefix before it.
func ParseBookVolume(s string) (string, float64, bool) {
	ms := bookVolumePattern.FindAllStringSubmatchIndex(s, -1)
	if len(ms) != 1 || bookVolumeRange.MatchString(s[ms[0][1]:]) || !bookVolumeEnd.MatchString(s[ms[0][1]:]) {
		return "", 0, false
	}
	prefix := strings.TrimRightFunc(s[:ms[0][2]], func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(":-_,", r)
	})
	v, err := strconv.ParseFloat(s[ms[0][4]:ms[0][5]], 64)
	if prefix == "" || err != nil {
		return "", 0, false
	}
	return prefix, v, true
}

// ParseBookFileVolume is ParseBookVolume for a file stem, ignoring trailing [..], (..) and {..} tags.
func ParseBookFileVolume(stem string) (string, float64, bool) {
	return ParseBookVolume(stripTags(bookFileTags, stem))
}

// IsBookSpecial reports markers of specials and extras, whose volume numbers refer to another book.
func IsBookSpecial(s string) bool { return specialPattern.MatchString(s) }

// RemoveCommonPrefix strips from a its common prefix with b, backed off to a word boundary in
// both so "Series 12" with "Series 1" leaves "12", not "2".
func RemoveCommonPrefix(a, b string) string {
	i := 0
	for i < min(len(a), len(b)) && a[i] == b[i] {
		i++
	}
	for i > 0 && (midWord(a, i) || midWord(b, i)) {
		_, size := utf8.DecodeLastRuneInString(a[:i])
		i -= size
	}
	return a[i:]
}

// midWord reports whether cutting s at i splits a rune or a run of letters and digits.
func midWord(s string, i int) bool {
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	next, _ := utf8.DecodeRuneInString(s[i:])
	return (i < len(s) && !utf8.RuneStart(s[i])) ||
		(unicode.In(prev, unicode.Letter, unicode.Digit) && unicode.In(next, unicode.Letter, unicode.Digit))
}

func FormatNum(f float64) string {
	if f == float64(int(f)) {
		return strconv.Itoa(int(f))
	}
	return fmt.Sprintf("%g", f)
}

func ParseFloatStr(s string) (float64, error) {
	f := 0.0
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
