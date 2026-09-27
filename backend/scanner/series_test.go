package scanner

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/metadata"
	"voltis/models"
)

func f32(v float32) *float32 { return new(v) }

func childOf(id string, parts []*float32, m metadata.Fields) Child {
	return Child{ID: id, OrderParts: parts, Meta: m}
}

func ids(children []Child) string {
	out := make([]string, len(children))
	for i, c := range children {
		out[i] = c.ID
	}
	return strings.Join(out, ",")
}

func TestOrderSortsByPartsThenURIPartThenID(t *testing.T) {
	cases := []struct {
		name string
		in   []Child
		want string
	}{
		{
			"numeric parts",
			[]Child{
				childOf("c", []*float32{f32(2)}, metadata.Fields{}),
				childOf("a", []*float32{f32(1)}, metadata.Fields{}),
				childOf("b", []*float32{f32(1.5)}, metadata.Fields{}),
			},
			"a,b,c",
		},
		{
			"nil parts sort last",
			[]Child{
				childOf("nil", []*float32{nil, f32(1)}, metadata.Fields{}),
				childOf("zero", []*float32{f32(0), f32(1)}, metadata.Fields{}),
			},
			"zero,nil",
		},
		{
			"equal parts fall back to id",
			[]Child{
				childOf("z", []*float32{f32(1)}, metadata.Fields{}),
				childOf("m", []*float32{f32(1)}, metadata.Fields{}),
				childOf("a", []*float32{f32(1)}, metadata.Fields{}),
			},
			"a,m,z",
		},
		{
			"equal parts fall back to uri part before id",
			[]Child{
				{ID: "a", URIPart: "SP03", OrderParts: []*float32{f32(100000)}},
				{ID: "b", URIPart: "SP01", OrderParts: []*float32{f32(100000)}},
				{ID: "c", URIPart: "SP02", OrderParts: []*float32{f32(100000)}},
				{ID: "d", URIPart: "v14", OrderParts: []*float32{f32(14)}},
			},
			"d,b,c,a",
		},
		{
			"shorter part list first",
			[]Child{
				childOf("long", []*float32{f32(1), f32(2)}, metadata.Fields{}),
				childOf("short", []*float32{f32(1)}, metadata.Fields{}),
			},
			"short,long",
		},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := ids(c.in)
			got := order(c.in)
			if ids(got) != c.want {
				t.Fatalf("order = %s, want %s", ids(got), c.want)
			}
			if ids(c.in) != before {
				t.Fatalf("input reordered: %s, want %s", ids(c.in), before)
			}
			if ids(order(got)) != c.want {
				t.Fatalf("order not idempotent: %s", ids(order(got)))
			}
		})
	}
}

func TestTiedChildrenChooseCoverAndMetadataByID(t *testing.T) {
	tied := func(id string, mtime time.Time, series, publisher string) Child {
		c := childOf(id, []*float32{f32(0)}, metadata.Fields{
			Series: metadata.Val(series), Publishers: metadata.Val([]string{publisher}),
		})
		c.CoverURI = new("/lib/s/" + id + ".epub/cover.jpg")
		c.FileMtime = &mtime
		return c
	}
	z := tied("z", baseTime.Add(2*time.Hour), "Zed Series", "Zed Press")
	a := tied("a", baseTime.Add(time.Hour), "Alpha Series", "Alpha Press")

	for _, arrival := range [][]Child{{z, a}, {a, z}} {
		ordered := order(arrival)
		if ids(ordered) != "a,z" {
			t.Fatalf("tied siblings order by ID, got %s", ids(ordered))
		}

		cover, mtime := (&BooksScanner{}).SeriesCover(SeriesRef{ID: "p", URIPart: "s"}, ordered)
		if cover == nil || *cover != "/lib/s/a.epub/cover.jpg" {
			t.Fatalf("series cover comes from the lowest-ID tied child, got %v", cover)
		}
		if mtime == nil || !mtime.Equal(baseTime.Add(time.Hour)) {
			t.Fatalf("series mtime comes from the lowest-ID tied child, got %v", mtime)
		}

		file := seriesLayer(SeriesRef{ID: "p", URIPart: "s"}, ordered)
		if file.Title.V != "Alpha Series" || !slices.Equal(file.Publishers.V, []string{"Alpha Press"}) {
			t.Fatalf("the lowest-ID tied child wins inherited fields, got %+v", file)
		}
	}
}

func TestSeriesLayerConverges(t *testing.T) {
	a := childOf("a", []*float32{f32(1)}, metadata.Fields{
		Series: metadata.Val("The Series"), Publishers: metadata.Val([]string{"Press"}),
		Description: metadata.Val("From A"),
	})
	b := childOf("b", []*float32{f32(2)}, metadata.Fields{
		Series: metadata.Val("Other"), Publishers: metadata.Val([]string{"Later Press"}),
		Genres: metadata.Val([]string{"Action"}), Language: metadata.Val("en"),
		ContentRating: metadata.Val(metadata.Suggestive), Manga: metadata.Val("Yes"), Imprint: metadata.Val("Imp"),
		PublicationDate: metadata.Val("2019"), Staff: metadata.Val([]metadata.Staff{{Name: "Ann", Role: "author"}}),
	})

	partial := seriesLayer(SeriesRef{URIPart: "s"}, order([]Child{a}))
	if partial.Title.V != "The Series" || partial.Genres.P != metadata.Absent {
		t.Fatalf("partial = %+v", partial)
	}

	// Descriptions and imprints describe a volume, not the series.
	full := seriesLayer(SeriesRef{URIPart: "s"}, order([]Child{a, b}))
	want := metadata.Fields{
		Title: metadata.Val("The Series"), Publishers: metadata.Val([]string{"Press"}),
		Genres: metadata.Val([]string{"action"}), Language: metadata.Val("en"),
		ContentRating: metadata.Val(metadata.Suggestive), Manga: metadata.Val("Yes"),
		PublicationDate: metadata.Val("2019"), Staff: metadata.Val([]metadata.Staff{{Name: "Ann", Role: "author"}}),
	}
	if !reflect.DeepEqual(full, want) {
		t.Fatalf("full = %+v, want %+v", full, want)
	}

	if reversed := seriesLayer(SeriesRef{URIPart: "s"}, order([]Child{b, a})); !reflect.DeepEqual(reversed, full) {
		t.Fatalf("arrival order matters: %+v != %+v", reversed, full)
	}
}

func TestSeriesLayerIsPure(t *testing.T) {
	build := func() []Child {
		return []Child{
			childOf("a", []*float32{f32(1)}, metadata.Fields{
				Series: metadata.Val("S"), Publishers: metadata.Val([]string{"P"}),
				Staff: metadata.Val([]metadata.Staff{{Name: "Ann", Role: "author"}}),
			}),
			childOf("b", []*float32{f32(2)}, metadata.Fields{
				Genres: metadata.Val([]string{"g"}),
				Staff:  metadata.Val([]metadata.Staff{{Name: "Bob", Role: "artist"}}),
			}),
		}
	}
	children, snapshot := build(), build()
	got := seriesLayer(SeriesRef{URIPart: "s"}, children)
	got.Publishers.V[0] = "mutated"
	got.Staff.V[0] = metadata.Staff{Name: "mutated", Role: "mutated"}
	if !reflect.DeepEqual(children, snapshot) {
		t.Fatalf("result shares backing storage with input: %+v", children)
	}
}

func TestSeriesLayerTitle(t *testing.T) {
	named := []Child{childOf("a", nil, metadata.Fields{Series: metadata.Val("Child Series")})}
	unnamed := []Child{childOf("a", nil, metadata.Fields{Title: metadata.Val("Vol. 1")})}
	for _, tc := range []struct {
		dir, part string
		children  []Child
		want      string
	}{
		{"/lib/Foo (2019)", "Foo_2019", named, "Child Series"},
		{"/lib/Foo\\bar", "Foo_bar", unnamed, "Foo\\bar"},
		{"/lib/Foo (2019)", "Foo_2019", unnamed, "Foo"},
		{"/lib/Foo\\bar (2019)", "Foo_bar_2019", nil, "Foo\\bar"},
		{"/lib/(2019)", "_2019", nil, "_2019"},
		// Book series have no folder, and inferred ones no series name.
		{"", "Fallback Part", unnamed, "Fallback Part"},
		{"", "Fallback Part", nil, "Fallback Part"},
	} {
		ref := SeriesRef{URIPart: tc.part}
		if tc.dir != "" {
			ref.FileURI = &tc.dir
		}
		if got := seriesLayer(ref, tc.children); got.Title.V != tc.want {
			t.Errorf("%q/%s title = %q, want %q", tc.dir, tc.part, got.Title.V, tc.want)
		}
	}
}

func TestSeriesLayerFolderAndDate(t *testing.T) {
	dated := []Child{
		childOf("a", []*float32{f32(1)}, metadata.Fields{PublicationDate: metadata.Val("2014-06-15T00:00:00Z")}),
		childOf("b", []*float32{f32(2)}, metadata.Fields{PublicationDate: metadata.Val("2012")}),
		childOf("c", []*float32{f32(3)}, metadata.Fields{PublicationDate: metadata.Val("unknown")}),
	}
	for _, tc := range []struct {
		name, dir string
		children  []Child
		alt       []string
		date      string
	}{
		{"folder year wins", "/lib/Foo (2019)", dated, []string{"Foo"}, "2019"},
		{"earliest child date", "/lib/Foo", dated, []string{"Foo"}, "2012"},
		{"book series", "", dated[:1], nil, "2014-06-15"},
		{"no date", "", nil, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := SeriesRef{URIPart: "s"}
			if tc.dir != "" {
				ref.FileURI = &tc.dir
			}
			got := seriesLayer(ref, tc.children)
			if !slices.Equal(got.AltTitles.V, tc.alt) || got.PublicationDate.V != tc.date {
				t.Fatalf("alt titles %v, date %q; want %v, %q", got.AltTitles.V, got.PublicationDate.V, tc.alt, tc.date)
			}
		})
	}
}

func TestLeafRowKeepsCreationAndCover(t *testing.T) {
	now := baseTime.Add(48 * time.Hour)
	file := FSFile{Path: "/lib/s/ch1.cbz", Mtime: baseTime.Add(time.Hour), Size: 42}
	item := ParsedItem{
		File:        file,
		ContentType: "comic",
		URIPart:     "ch1",
		OrderParts:  []*float32{f32(1), f32(2)},
		FileData:    []byte(`{"pages":[]}`),
	}
	old := models.Content{
		ID: "leaf", CreatedAt: baseTime, UpdatedAt: baseTime,
		CoverURI: new("/lib/s/ch1.cbz/old.jpg"), Order: new(3),
	}

	kept := leafRow("leaf", "library", "comic/s/ch1", item, new("p"), &old, now)
	if !kept.CreatedAt.Equal(baseTime) || !kept.UpdatedAt.Equal(now) {
		t.Fatalf("times = %v/%v", kept.CreatedAt, kept.UpdatedAt)
	}
	if kept.CoverURI == nil || *kept.CoverURI != "/lib/s/ch1.cbz/old.jpg" {
		t.Fatalf("cover = %v, want the old cover", kept.CoverURI)
	}
	if kept.Order == nil || *kept.Order != 3 {
		t.Fatalf("order = %v", kept.Order)
	}
	if !kept.Valid || kept.URI != "comic/s/ch1" || kept.URIPart != "ch1" || kept.Type != "comic" ||
		*kept.FileURI != file.Path || *kept.FileSize != 42 || !kept.FileMtime.Equal(file.Mtime) ||
		kept.ParentID == nil || *kept.ParentID != "p" || string(kept.FileData) != `{"pages":[]}` {
		t.Fatalf("row = %+v", kept)
	}

	item.CoverSuffix = new("001.jpg")
	replaced := leafRow("leaf", "library", "comic/s/ch1", item, nil, &old, now)
	if replaced.CoverURI == nil || *replaced.CoverURI != "/lib/s/ch1.cbz/001.jpg" {
		t.Fatalf("cover = %v", replaced.CoverURI)
	}
	if !replaced.CreatedAt.Equal(baseTime) || replaced.ParentID != nil {
		t.Fatalf("row = %+v", replaced)
	}

	created := leafRow("fresh", "library", "comic/s/ch1", item, nil, nil, now)
	if !created.CreatedAt.Equal(now) || created.Order != nil {
		t.Fatalf("row = %+v", created)
	}
}

func TestSeriesCoverComics(t *testing.T) {
	cs := &ComicsScanner{}
	child := Child{ID: "a", CoverURI: new("/lib/s/ch1.cbz/001.jpg"), FileMtime: new(baseTime)}

	dir := t.TempDir()
	coverPath := filepath.Join(dir, "cover.png")
	must(t, os.WriteFile(coverPath, []byte("x"), 0o644))
	info, err := os.Stat(coverPath)
	must(t, err)

	uri, mtime := cs.SeriesCover(SeriesRef{ID: "p", FileURI: &dir}, []Child{child})
	if uri == nil || *uri != coverPath || mtime == nil || !mtime.Equal(info.ModTime().UTC()) {
		t.Fatalf("cover = %v/%v, want %s", uri, mtime, coverPath)
	}

	empty := t.TempDir()
	uri, mtime = cs.SeriesCover(SeriesRef{ID: "p", FileURI: &empty}, []Child{child})
	if uri == nil || *uri != *child.CoverURI || mtime == nil || !mtime.Equal(baseTime) {
		t.Fatalf("fallback cover = %v/%v", uri, mtime)
	}

	if uri, mtime = cs.SeriesCover(SeriesRef{ID: "p"}, nil); uri != nil || mtime != nil {
		t.Fatalf("empty cover = %v/%v", uri, mtime)
	}
}

func TestSeriesCoverBooks(t *testing.T) {
	bs := &BooksScanner{}
	first := Child{ID: "a", CoverURI: new("/lib/b/one.epub/cover.jpg"), FileMtime: new(baseTime)}
	second := Child{ID: "b", CoverURI: new("/lib/b/two.epub/cover.jpg")}

	uri, mtime := bs.SeriesCover(SeriesRef{ID: "p"}, []Child{first, second})
	if uri == nil || *uri != *first.CoverURI || mtime == nil || !mtime.Equal(baseTime) {
		t.Fatalf("cover = %v/%v", uri, mtime)
	}

	if uri, mtime = bs.SeriesCover(SeriesRef{ID: "p"}, nil); uri != nil || mtime != nil {
		t.Fatalf("empty cover = %v/%v", uri, mtime)
	}
}

func leafWrite(id, uriPart string) write {
	return write{id: id, item: &ParsedItem{URIPart: uriPart}}
}

func stepIDs(steps []step) string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.id()
	}
	return strings.Join(out, ",")
}

func TestOrderSteps(t *testing.T) {
	cases := []struct {
		name    string
		sets    map[string]*SeriesChanges
		key     map[string]Key
		build   func() (map[string]*SeriesChanges, map[string]Key)
		runs    int
		want    string
		wantErr string
	}{
		{
			name: "cross series leaf chain",
			sets: map[string]*SeriesChanges{
				"S": {Ref: SeriesRef{ID: "S", URIPart: "S"}, Writes: []write{leafWrite("B", "ch1")}},
				"T": {Ref: SeriesRef{ID: "T", URIPart: "T"}, Writes: []write{leafWrite("A", "ch1")}},
			},
			key:  map[string]Key{"A": {"S", "ch1"}},
			want: "A,B",
		},
		{
			name: "standalone and rename releases precede their claims against enumeration order",
			sets: map[string]*SeriesChanges{
				"":  {Ref: SeriesRef{ID: ""}, Writes: []write{leafWrite("L2", "Old")}},
				"F": {Ref: SeriesRef{ID: "F", URIPart: "Foo"}, New: true},
				"R": {Ref: SeriesRef{ID: "R", URIPart: "New"}, OldURI: "comic/Old"},
				"T": {Ref: SeriesRef{ID: "T", URIPart: "T"}, Writes: []write{leafWrite("L1", "ch1")}},
			},
			key:  map[string]Key{"L1": {"", "Foo"}, "R": {"", "Old"}},
			want: "R,L2,L1,F",
		},
		{
			name: "series rename chain with children",
			sets: map[string]*SeriesChanges{
				"A": {Ref: SeriesRef{ID: "A", URIPart: "b"}, OldURI: "comic/a", Writes: []write{leafWrite("ca", "ch1")}},
				"B": {Ref: SeriesRef{ID: "B", URIPart: "c"}, OldURI: "comic/b", Writes: []write{leafWrite("cb", "ch1")}},
			},
			key:  map[string]Key{"A": {"", "a"}, "B": {"", "b"}},
			want: "B,A,ca,cb",
		},
		{
			name: "new parent before the child whose key an earlier set claims",
			sets: map[string]*SeriesChanges{
				"A": {Ref: SeriesRef{ID: "A", URIPart: "a"}, Writes: []write{leafWrite("x", "ch1")}},
				"Z": {Ref: SeriesRef{ID: "Z", URIPart: "z"}, New: true, Writes: []write{leafWrite("y", "ch1")}},
			},
			key:  map[string]Key{"y": {"A", "ch1"}},
			want: "Z,y,x",
		},
		{
			name: "self rename",
			sets: map[string]*SeriesChanges{
				"p1": {Ref: SeriesRef{ID: "p1", URI: "comic/S", URIPart: "S"}, OldURI: "comic/S"},
			},
			key:  map[string]Key{},
			want: "",
		},
		{
			name: "absent hydrated key claims no release",
			sets: map[string]*SeriesChanges{
				"S": {Ref: SeriesRef{ID: "S", URIPart: "S"}, Writes: []write{leafWrite("B", "ch1"), leafWrite("A", "ch2")}},
			},
			key:  map[string]Key{},
			want: "B,A",
		},
		{
			name: "unchanged key claims no release",
			sets: map[string]*SeriesChanges{
				"S": {Ref: SeriesRef{ID: "S", URIPart: "S"}, Writes: []write{leafWrite("B", "ch1"), leafWrite("A", "ch2")}},
			},
			key:  map[string]Key{"A": {"S", "ch2"}, "B": {"S", "ch1"}},
			want: "B,A",
		},
		{
			name: "swap cycle errors",
			sets: map[string]*SeriesChanges{
				"S": {Ref: SeriesRef{ID: "S", URIPart: "S"}, Writes: []write{leafWrite("A", "ch2"), leafWrite("B", "ch1")}},
			},
			key:     map[string]Key{"A": {"S", "ch1"}, "B": {"S", "ch2"}},
			wantErr: "key cycle",
		},
		{
			name: "deterministic across rebuilt fixtures",
			build: func() (map[string]*SeriesChanges, map[string]Key) {
				return map[string]*SeriesChanges{
						"A": {Ref: SeriesRef{ID: "A", URIPart: "b"}, OldURI: "comic/a", Writes: []write{leafWrite("a1", "ch1"), leafWrite("a2", "ch2")}},
						"B": {Ref: SeriesRef{ID: "B", URIPart: "c"}, OldURI: "comic/b", Writes: []write{leafWrite("b1", "ch1")}},
						"C": {Ref: SeriesRef{ID: "C", URIPart: "d"}, New: true, Writes: []write{leafWrite("c1", "ch1")}},
						"D": {Ref: SeriesRef{ID: "D", URIPart: "e"}, Writes: []write{leafWrite("d1", "ch3")}},
					},
					map[string]Key{"A": {"", "a"}, "B": {"", "b"}, "d1": {"D", "ch9"}}
			},
			runs: 20,
			want: "B,A,a1,a2,b1,C,c1,d1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i := range max(c.runs, 1) {
				sets, key := c.sets, c.key
				if c.build != nil {
					sets, key = c.build()
				}
				steps, err := orderSteps(flush{sets: sets}, key)
				if c.wantErr != "" {
					if err == nil {
						t.Fatalf("steps = %s, want a %q error", stepIDs(steps), c.wantErr)
					}
					if !strings.Contains(err.Error(), c.wantErr) {
						t.Fatalf("err = %v, want %q", err, c.wantErr)
					}
					continue
				}
				if err != nil {
					t.Fatalf("orderSteps: %v", err)
				}
				if got := stepIDs(steps); got != c.want {
					t.Fatalf("run %d: steps = %s, want %s", i, got, c.want)
				}
			}
		})
	}
}
