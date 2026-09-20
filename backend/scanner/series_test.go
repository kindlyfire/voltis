package scanner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"voltis/models"
)

func f32(v float32) *float32 { return new(v) }

func childOf(id string, parts []*float32, m models.Metadata) Child {
	return Child{ID: id, OrderParts: parts, Meta: rawMeta(m)}
}

func ids(children []Child) string {
	out := make([]string, len(children))
	for i, c := range children {
		out[i] = c.ID
	}
	return strings.Join(out, ",")
}

func TestOrderSortsByPartsThenID(t *testing.T) {
	cases := []struct {
		name string
		in   []Child
		want string
	}{
		{
			"numeric parts",
			[]Child{
				childOf("c", []*float32{f32(2)}, models.Metadata{}),
				childOf("a", []*float32{f32(1)}, models.Metadata{}),
				childOf("b", []*float32{f32(1.5)}, models.Metadata{}),
			},
			"a,b,c",
		},
		{
			"nil parts sort last",
			[]Child{
				childOf("nil", []*float32{nil, f32(1)}, models.Metadata{}),
				childOf("one", []*float32{f32(1), f32(1)}, models.Metadata{}),
			},
			"one,nil",
		},
		{
			"equal parts fall back to id",
			[]Child{
				childOf("z", []*float32{f32(1)}, models.Metadata{}),
				childOf("m", []*float32{f32(1)}, models.Metadata{}),
				childOf("a", []*float32{f32(1)}, models.Metadata{}),
			},
			"a,m,z",
		},
		{
			"shorter part list first",
			[]Child{
				childOf("long", []*float32{f32(1), f32(2)}, models.Metadata{}),
				childOf("short", []*float32{f32(1)}, models.Metadata{}),
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
	tied := func(id string, mtime time.Time, m models.Metadata) Child {
		c := childOf(id, []*float32{f32(0)}, m)
		c.CoverURI = new("/lib/s/" + id + ".epub/cover.jpg")
		c.FileMtime = &mtime
		return c
	}
	z := tied("z", baseTime.Add(2*time.Hour), models.Metadata{Series: "Zed Series", Publisher: "Zed Press"})
	a := tied("a", baseTime.Add(time.Hour), models.Metadata{Series: "Alpha Series", Publisher: "Alpha Press"})

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

		file := inherit(SeriesRef{ID: "p", URIPart: "s"}, ordered)
		if file.Title != "Alpha Series" || file.Publisher != "Alpha Press" {
			t.Fatalf("the lowest-ID tied child wins inherited fields, got %+v", file)
		}
	}
}

func TestInheritConverges(t *testing.T) {
	a := childOf("a", []*float32{f32(1)}, models.Metadata{
		Series: "The Series", Publisher: "Press", Description: "From A",
	})
	b := childOf("b", []*float32{f32(2)}, models.Metadata{
		Series: "Other", Publisher: "Later Press", Genre: "Action", Language: "en",
		AgeRating: "Teen", Manga: "Yes", Imprint: "Imp", PublicationDate: "2019",
		Staff: []models.StaffEntry{{Name: "Ann", Role: "author"}},
	})

	partial := inherit(SeriesRef{URIPart: "s"}, order([]Child{a}))
	if partial.Title != "The Series" || partial.Publisher != "Press" || partial.Genre != "" {
		t.Fatalf("partial = %+v", partial)
	}

	full := inherit(SeriesRef{URIPart: "s"}, order([]Child{a, b}))
	want := models.Metadata{
		Title: "The Series", Publisher: "Press", Description: "From A",
		Genre: "Action", Language: "en", AgeRating: "Teen", Manga: "Yes",
		Imprint: "Imp", PublicationDate: "2019",
		Staff: []models.StaffEntry{{Name: "Ann", Role: "author"}},
	}
	if !reflect.DeepEqual(full, want) {
		t.Fatalf("full = %+v, want %+v", full, want)
	}

	reversed := inherit(SeriesRef{URIPart: "s"}, order([]Child{b, a}))
	if !reflect.DeepEqual(reversed, full) {
		t.Fatalf("arrival order matters: %+v != %+v", reversed, full)
	}

	if again := inherit(SeriesRef{URIPart: "s"}, order([]Child{a, b})); !reflect.DeepEqual(again, full) {
		t.Fatalf("inherit not idempotent: %+v", again)
	}
}

func TestInheritIsPure(t *testing.T) {
	build := func() []Child {
		return []Child{
			childOf("a", []*float32{f32(1)}, models.Metadata{
				Series: "S", Publisher: "P",
				Staff: []models.StaffEntry{{Name: "Ann", Role: "author"}},
			}),
			childOf("b", []*float32{f32(2)}, models.Metadata{
				Genre: "G",
				Staff: []models.StaffEntry{{Name: "Bob", Role: "artist"}},
			}),
		}
	}
	children, snapshot := build(), build()
	got := inherit(SeriesRef{URIPart: "s"}, children)
	if !reflect.DeepEqual(children, snapshot) {
		t.Fatalf("children mutated: %+v", children)
	}
	got.Publisher = "mutated"
	got.Staff[0] = models.StaffEntry{Name: "mutated", Role: "mutated"}
	if !reflect.DeepEqual(children, snapshot) {
		t.Fatalf("result shares backing storage with input: %+v", children)
	}
	fresh := inherit(SeriesRef{URIPart: "s"}, children)
	if fresh.Publisher != "P" || !reflect.DeepEqual(fresh.Staff, snapshot[0].Meta.File.Raw.Staff) {
		t.Fatalf("result aliases input: %+v", fresh)
	}
}

func TestInheritTitleFallback(t *testing.T) {
	if got := inherit(SeriesRef{URIPart: "Fallback Part"}, nil); got.Title != "Fallback Part" {
		t.Fatalf("empty children title = %q", got.Title)
	}
	children := []Child{childOf("a", nil, models.Metadata{Publisher: "P"})}
	if got := inherit(SeriesRef{URIPart: "Fallback Part"}, children); got.Title != "Fallback Part" || got.Publisher != "P" {
		t.Fatalf("inherited = %+v", got)
	}
}

func TestInheritTitleFallsBackToTheFolderOnlyWhenItSanitizesToTheKey(t *testing.T) {
	for _, tc := range []struct {
		dir, part, want string
	}{
		{"/lib/Foo\\bar", "Foo_bar", "Foo\\bar"},
		{"/lib/Foo_bar", "Foo_bar", "Foo_bar"},
		{"/lib/Foo (2019)", "Foo_2019", "Foo_2019"},
		{"/lib/(2019)", "_2019", "_2019"},
		{"/lib/Foo\\bar (2019)", "Foo_bar_2019", "Foo_bar_2019"},
	} {
		ref := SeriesRef{URIPart: tc.part, Type: "comic_series", FileURI: &tc.dir}
		if got := inherit(ref, nil); got.Title != tc.want {
			t.Errorf("%s title = %q, want %q", tc.dir, got.Title, tc.want)
		}
	}

	book := SeriesRef{URIPart: "Foo_bar", Type: "book_series"}
	if got := inherit(book, nil); got.Title != "Foo_bar" {
		t.Errorf("book title = %q, want the uri part", got.Title)
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
