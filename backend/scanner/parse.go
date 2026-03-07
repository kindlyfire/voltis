package scanner

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	volumePattern  = regexp.MustCompile(`(?i)(?:\#|(?:v|vo|vol|volu|volum|volume)\.?)\s*(\d+(?:\.\d+)?)`)
	chapterPattern = regexp.MustCompile(`(?i)(?:c|ch|chap|chapt|chapte|chapter)\.?\s*(\d+(?:\.\d+)?)`)
	numberPattern  = regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	yearPattern    = regexp.MustCompile(`\((\d+)\)`)
	trailingTags   = regexp.MustCompile(`\s*[\[\(][^\[\]\(\)]*[\]\)]\s*$`)
)

func parseVolume(name string) *float64 {
	m := volumePattern.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseChapter(name string) *float64 {
	m := chapterPattern.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseFallbackChapter(name string) *float64 {
	name = cleanSeriesName(name)
	matches := numberPattern.FindAllStringSubmatch(name, -1)
	if len(matches) == 0 {
		return nil
	}

	// Find the match with the most digits
	best := matches[0][1]
	for _, m := range matches[1:] {
		if digitCount(m[1]) > digitCount(best) {
			best = m[1]
		}
	}

	v, err := strconv.ParseFloat(best, 64)
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

func parseSeriesName(name string) (string, *int) {
	year := parseSeriesYear(name)
	cleaned := cleanSeriesName(name)
	return cleaned, year
}

func parseSeriesYear(name string) *int {
	matches := yearPattern.FindAllStringSubmatch(name, -1)
	// Search from right to left for a 4-digit year
	for i := len(matches) - 1; i >= 0; i-- {
		v, err := strconv.Atoi(matches[i][1])
		if err != nil {
			continue
		}
		if v >= 1000 && v <= 9999 {
			return &v
		}
	}
	return nil
}

func cleanSeriesName(name string) string {
	for {
		cleaned := trailingTags.ReplaceAllString(name, "")
		if cleaned == name {
			break
		}
		name = cleaned
	}
	return strings.TrimSpace(name)
}

func removeCommonPrefix(a, b string) (string, string) {
	minLen := min(len(b), len(a))
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[i:], b[i:]
}
