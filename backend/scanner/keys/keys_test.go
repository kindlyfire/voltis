package keys

import (
	"fmt"
	"math"
	"strconv"
	"testing"
)

func fmtFloatPtr(v *float64) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%g", *v)
}

func fmtIntPtr(v *int) string {
	if v == nil {
		return "nil"
	}
	return strconv.Itoa(*v)
}

func TestKeysParseVolume(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Series v1", "1"},
		{"Series V02", "2"},
		{"Series vol. 3", "3"},
		{"Series volume 4", "4"},
		{"Series vol5.5", "5.5"},
		{"Series #7", "7"},
		{"Series Vo.8", "8"},
		{"Series", "nil"},
		{"Series ch3", "nil"},
		{"Series v", "nil"},
	}
	for _, c := range cases {
		if got := fmtFloatPtr(ParseVolume(c.in)); got != c.want {
			t.Errorf("ParseVolume(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestKeysParseChapter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Series c1", "1"},
		{"Series ch 2", "2"},
		{"Series chap.3", "3"},
		{"Series chapter 4", "4"},
		{"Series ch005", "5"},
		{"Series ch6.5", "6.5"},
		{"Series", "nil"},
		{"Series v3", "nil"},
	}
	for _, c := range cases {
		if got := fmtFloatPtr(ParseChapter(c.in)); got != c.want {
			t.Errorf("ParseChapter(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestKeysParseFallbackChapter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Series 012", "12"},
		{"1 - 0025", "25"},
		{"Series", "nil"},
		{"Series 3 (2019)", "3"},
		{"7.5", "7.5"},
		{"2 10 3", "10"},
	}
	for _, c := range cases {
		if got := fmtFloatPtr(ParseFallbackChapter(c.in)); got != c.want {
			t.Errorf("ParseFallbackChapter(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestKeysDigitCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"12.5", 3},
		{"abc", 0},
		{"0025", 4},
	}
	for _, c := range cases {
		if got := digitCount(c.in); got != c.want {
			t.Errorf("digitCount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestKeysCleanSeriesName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Series (2019)", "Series"},
		{"Series (2019) [Digital]", "Series"},
		{"Series [a] (b) ", "Series"},
		{"  Series  ", "Series"},
		{"Series (2019) Extra", "Series (2019) Extra"},
		{"(2019)", ""},
	}
	for _, c := range cases {
		if got := CleanSeriesName(c.in); got != c.want {
			t.Errorf("CleanSeriesName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKeysParseSeriesYear(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Series (2019)", "2019"},
		{"Series (1999) (2019)", "2019"},
		{"Series (19)", "nil"},
		{"Series (12345)", "nil"},
		{"Series 2019", "nil"},
		{"Series (2019) (v2)", "2019"},
	}
	for _, c := range cases {
		if got := fmtIntPtr(ParseSeriesYear(c.in)); got != c.want {
			t.Errorf("ParseSeriesYear(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestKeysParseSeriesName(t *testing.T) {
	cases := []struct{ in, wantName, wantYear string }{
		{"Series (2019)", "Series", "2019"},
		{"Series (2019) [Digital]", "Series", "2019"},
		{"Series", "Series", "nil"},
	}
	for _, c := range cases {
		name, year := ParseSeriesName(c.in)
		if name != c.wantName || fmtIntPtr(year) != c.wantYear {
			t.Errorf("ParseSeriesName(%q) = %q/%s, want %q/%s", c.in, name, fmtIntPtr(year), c.wantName, c.wantYear)
		}
	}
}

func TestKeysRemoveCommonPrefix(t *testing.T) {
	cases := []struct{ a, b, wantA, wantB string }{
		{"Series ch1", "Series", " ch1", ""},
		{"abc", "abd", "c", "d"},
		{"", "abc", "", "abc"},
		{"same", "same", "", ""},
	}
	for _, c := range cases {
		a, b := RemoveCommonPrefix(c.a, c.b)
		if a != c.wantA || b != c.wantB {
			t.Errorf("RemoveCommonPrefix(%q, %q) = %q/%q, want %q/%q", c.a, c.b, a, b, c.wantA, c.wantB)
		}
	}
}

func TestKeysFormatNum(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1, "1"},
		{1.5, "1.5"},
		{0, "0"},
		{-2, "-2"},
		{12.25, "12.25"},
		{1000000, "1000000"},
	}
	for _, c := range cases {
		if got := FormatNum(c.in); got != c.want {
			t.Errorf("FormatNum(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKeysParseFloatStr(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"1", 1, false},
		{"1.5", 1.5, false},
		{"1.5a", 1.5, false},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		got, err := ParseFloatStr(c.in)
		if (err != nil) != c.wantErr || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ParseFloatStr(%q) = %v/%v, want %v/err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}
