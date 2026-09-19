package keys

import (
	"fmt"
	"regexp"
	"slices"
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

func parseNumber(pattern *regexp.Regexp, name string) *float64 {
	m := pattern.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	return &v
}

func ParseVolume(name string) *float64 { return parseNumber(volumePattern, name) }

func ParseChapter(name string) *float64 { return parseNumber(chapterPattern, name) }

func ParseFallbackChapter(name string) *float64 {
	name = CleanSeriesName(name)
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

func CleanSeriesName(name string) string {
	for {
		cleaned := trailingTags.ReplaceAllString(name, "")
		if cleaned == name {
			break
		}
		name = cleaned
	}
	return strings.TrimSpace(name)
}

func RemoveCommonPrefix(a, b string) (string, string) {
	minLen := min(len(b), len(a))
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[i:], b[i:]
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
