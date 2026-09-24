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
		{"Series_v01", "1"},
		{"Series-v01", "1"},
		{"Vol.8 Series", "8"},
		{"Dev 2", "nil"},
		{"Revolver 3", "nil"},
		{"Series#7", "7"},
		{"Series#2 ch2", "2"},
	}
	for _, c := range cases {
		if got := fmtFloatPtr(ParseVolume(c.in)); got != c.want {
			t.Errorf("ParseVolume(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestKeysParseBookVolume(t *testing.T) {
	const ser = "Ironbound: From Nothing to Legend's End"
	cases := []struct {
		in, prefix string
		vol        float64
	}{
		{ser + " Vol. 01", ser, 1},
		{"Ironbound: From Nothing to Legend’s End Vol. 14", "Ironbound: From Nothing to Legend’s End", 14},
		{"Ironbound Zero: Volume 1", "Ironbound Zero", 1},
		{"Ironbound Zero: Volume 6", "Ironbound Zero", 6},
		{"Series, Vol. 3", "Series", 3},
		{"Series Vol. 3: Subtitle", "Series", 3},
		{"Series Vol 3, Subtitle", "Series", 3},
		{"Series - Volume 2 - The Return", "Series", 2},
		{"Series v2.5", "Series", 2.5},
		{"Series_v01", "Series", 1},
		{"Foo:\nVolume 1", "Foo", 1},
		{"Foo Vol.\u00a01", "Foo", 1},
		{"Foo Vol.\u20021:\u00a0Sub", "Foo", 1},
		{"Foo\u00a0-\u00a0Vol. 2", "Foo", 2},
		{"Ünïcødé 漫画 Volume 4", "Ünïcødé 漫画", 4},
		{"転生したらスライムだった件 Vol. 5", "転生したらスライムだった件", 5},
		// Rejected: no prefix, in-word markers, ranges, several markers, trailing text, bare numbers.
		{"Volume 1", "", 0},
		{"Vol. 1: Subtitle", "", 0},
		{"Dev 2", "", 0},
		{"Revolver 3", "", 0},
		{"Series v1-3", "", 0},
		{"Series Vol. 1-2", "", 0},
		{"Foo Vol. 1 - 3", "", 0},
		{"Foo Vol.\u00a01\u00a0-\u00a03", "", 0},
		{"Foo Vol. 1 – 3", "", 0},
		{"Foo v1 ~ 3", "", 0},
		{"Foo Vol. 1 - 3 Days", "", 0},
		{"Series Vol. 1 Vol. 2", "", 0},
		{"Series Vol. 3rd Edition", "", 0},
		{"Series Vol. 3 Part 2", "", 0},
		{"Series #3", "", 0},
		{"Series 3", "", 0},
		{"Series vo. 3", "", 0},
	}
	for _, c := range cases {
		prefix, vol, ok := ParseBookVolume(c.in)
		if prefix != c.prefix || vol != c.vol || ok != (c.prefix != "") {
			t.Errorf("ParseBookVolume(%q) = %q/%g/%v, want %q/%g", c.in, prefix, vol, ok, c.prefix, c.vol)
		}
	}
}

func TestKeysParseBookFileVolume(t *testing.T) {
	const ser = "Ironbound - From Nothing to Legend's End"
	cases := []struct {
		in, prefix string
		vol        float64
	}{
		{ser + " v01 [Pub] [Tier] [Group] {x}", ser, 1},
		{ser + " v14 [Pub] [Tier] [Group]", ser, 14},
		{"Ironbound Zero - From Nothing to Legend's End v06 [Pub] [Crew]",
			"Ironbound Zero - From Nothing to Legend's End", 6},
		{"The Day I Woke Up as a Teapot v17", "The Day I Woke Up as a Teapot", 17},
		{"Series (2019) Vol. 2 (Digital)", "Series (2019)", 2},
		// The SP stems parse; IsBookSpecial keeps them out of inference.
		{ser + " SP02 - Volume 10 [STORE☆FRONT Exclusive Short Story] [Group]", ser + " SP02", 10},
		{ser + " SP01 - Short Stories [Pub] [Tier] [Group] {x}", "", 0},
		{"Plenty - Jane Author", "", 0},
		{"v01 [Tag]", "", 0},
	}
	for _, c := range cases {
		prefix, vol, ok := ParseBookFileVolume(c.in)
		if prefix != c.prefix || vol != c.vol || ok != (c.prefix != "") {
			t.Errorf("ParseBookFileVolume(%q) = %q/%g/%v, want %q/%g", c.in, prefix, vol, ok, c.prefix, c.vol)
		}
	}
}

func TestKeysIsBookSpecial(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Ironbound - From Nothing to Legend's End SP01 - Short Stories [Pub]", true},
		{"Ironbound Volume 10 - STORE☆FRONT Exclusive Popularity Poll Short Story", true},
		{"Ironbound Volume 13 - STORE☆FRONT Exclusive Short Story", true},
		{"Series Vol. 2 Side Story", true},
		{"Foo Vol. 1 - Short  Stories", true},
		{"Foo Vol. 1 - Side\u00a0Story", true},
		{"Series Vol. 2 - Bonus", true},
		{"Series_sp3", true},
		{"Series Extra", true},
		{"Ironbound: From Nothing to Legend's End Vol. 01", false},
		{"Extraordinary Vol. 2", false},
		{"Spice and Wolf Vol. 1", false},
		{"Crisp3 Vol. 1", false},
	}
	for _, c := range cases {
		if got := IsBookSpecial(c.in); got != c.want {
			t.Errorf("IsBookSpecial(%q) = %v, want %v", c.in, got, c.want)
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
