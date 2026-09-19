package scanner

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/lib/sources"
	"voltis/models"
	"voltis/models/metaraw"
)

func f32(v float32) *float32 { return new(v) }

func childOf(id string, parts []*float32, m models.Metadata) Child {
	return Child{
		ID:         id,
		URI:        "comic/s/" + id,
		URIPart:    id,
		OrderParts: parts,
		Valid:      true,
		Meta:       rawMeta(m),
	}
}

func seriesChild(kind, id string, parts ...*float32) models.Content {
	ext := ".cbz"
	if kind == "book" {
		ext = ".epub"
	}
	c := testLeaf(id, kind, "/lib/s/"+id+ext, baseTime, 10, true)
	c.URI = kind + "/s/" + id
	c.ParentID = new("p")
	c.OrderParts = parts
	return c
}

func seriesRepo(kind string, children ...models.Content) *repository {
	r := newRepository(nil, "library")
	r.content = append([]models.Content{{
		ID: "p", LibraryID: "library", Type: kind + "_series", URIPart: "s", URI: kind + "/s", Valid: true,
	}}, children...)
	return r
}

func comicChild(id string, part float32, m models.Metadata) (models.Content, *metadataRow) {
	c := seriesChild("comic", id, new(part))
	return c, &metadataRow{URI: c.URI, LibraryID: "library", DataRaw: rawMeta(m)}
}

func orderedChildren(r *repository) []Child {
	return order(r.children(r.childrenOf("p")))
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

func TestOrderAcceptedChangeTiedSiblingsRankByIDNotRepositoryOrder(t *testing.T) {
	tied := func(id string, mtime time.Time) models.Content {
		c := seriesChild("book", id, f32(0))
		c.CoverURI = new("/lib/s/" + id + ".epub/cover.jpg")
		c.FileMtime = &mtime
		return c
	}
	r := seriesRepo("book", tied("z", baseTime.Add(2*time.Hour)), tied("a", baseTime.Add(time.Hour)))
	r.metadata = []*metadataRow{
		{URI: "book/s/z", LibraryID: "library", DataRaw: rawMeta(models.Metadata{Series: "Zed Series", Publisher: "Zed Press"})},
		{URI: "book/s/a", LibraryID: "library", DataRaw: rawMeta(models.Metadata{Series: "Alpha Series", Publisher: "Alpha Press"})},
	}

	updateGroupSeries(&BooksScanner{}, r, map[string]bool{"p": true})

	ranked := map[string]int{}
	for i := range r.content {
		if c := r.content[i]; c.Order != nil {
			ranked[c.ID] = *c.Order
		}
	}
	if len(ranked) != 2 || ranked["a"] != 0 || ranked["z"] != 1 {
		t.Fatalf("accepted change: tied siblings order by ID, got %v", ranked)
	}

	series := r.content[0]
	if series.CoverURI == nil || *series.CoverURI != "/lib/s/a.epub/cover.jpg" {
		t.Fatalf("accepted change: series cover comes from the lowest-ID tied child, got %v", series.CoverURI)
	}
	if series.FileMtime == nil || !series.FileMtime.Equal(baseTime.Add(time.Hour)) {
		t.Fatalf("accepted change: series mtime comes from the lowest-ID tied child, got %v", series.FileMtime)
	}

	file := r.getMetadata("book/s").DataRaw.File.Raw
	if file.Title != "Alpha Series" || file.Publisher != "Alpha Press" {
		t.Fatalf("accepted change: the lowest-ID tied child wins inherited fields, got %+v", file)
	}
}

type capturingScanner struct {
	BooksScanner
	ordered []Child
}

func (cs *capturingScanner) UpdateSeries(r *repository, series *models.Content, ordered []Child) {
	cs.ordered = slices.Clone(ordered)
	cs.BooksScanner.UpdateSeries(r, series, ordered)
}

func TestOrderStampsRanksOntoChildrenPassedToAdapters(t *testing.T) {
	r := seriesRepo("book", seriesChild("book", "b", f32(2)), seriesChild("book", "a", f32(1)))

	cs := &capturingScanner{}
	updateGroupSeries(cs, r, map[string]bool{"p": true})

	if len(cs.ordered) != 2 {
		t.Fatalf("adapter saw %d children, want 2", len(cs.ordered))
	}
	for i, c := range cs.ordered {
		if c.Order == nil || *c.Order != i {
			t.Fatalf("child %s reached the adapter with order %v, want %d", c.ID, c.Order, i)
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

func TestInheritRetainsInvalidChildren(t *testing.T) {
	invalid := childOf("a", []*float32{f32(1)}, models.Metadata{Series: "Retained Series", Publisher: "Retained Press"})
	invalid.Valid = false
	valid := childOf("b", []*float32{f32(2)}, models.Metadata{Series: "Other", Publisher: "Other Press", Genre: "Action"})

	got := inherit(SeriesRef{URIPart: "s"}, order([]Child{invalid, valid}))
	if got.Title != "Retained Series" || got.Publisher != "Retained Press" || got.Genre != "Action" {
		t.Fatalf("inherited = %+v", got)
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

func TestInheritCorrectsAfterEarlierChildArrives(t *testing.T) {
	late, lateMeta := comicChild("a", 2, models.Metadata{Series: "B Series", Publisher: "B Press", Genre: "Action"})
	r := seriesRepo("comic", late)
	r.metadata = []*metadataRow{lateMeta}

	inheritChildMetadata(r, &r.content[0], orderedChildren(r))
	if got := r.getMetadata("comic/s").DataRaw.File.Raw; got.Title != "B Series" || got.Publisher != "B Press" {
		t.Fatalf("b alone = %+v", got)
	}

	early, earlyMeta := comicChild("z", 1, models.Metadata{Series: "A Series", Publisher: "A Press"})
	r.content = append(r.content, early)
	r.metadata = append(r.metadata, earlyMeta)

	inheritChildMetadata(r, &r.content[0], orderedChildren(r))
	got := r.getMetadata("comic/s").DataRaw.File.Raw
	if got.Title != "A Series" || got.Publisher != "A Press" {
		t.Fatalf("not corrected by the earlier child: %+v", got)
	}
	if got.Genre != "Action" {
		t.Fatalf("later child field dropped: %+v", got)
	}
}

func TestInheritReplacesWholeFileLayer(t *testing.T) {
	child, childMeta := comicChild("a", 1, models.Metadata{Series: "New Series", Genre: "Child Genre", Language: "en"})
	r := seriesRepo("comic", child)
	r.metadata = []*metadataRow{childMeta, {
		URI: "comic/s", LibraryID: "library", DataRaw: metaraw.MetadataRaw{
			File: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{
				Title: "Stale", Publisher: "Stale Press", Genre: "Stale Genre", Language: "jp",
			}},
			MangaBaka: &metaraw.RawContainer[sources.Series]{Raw: sources.Series{
				ID: 7, Title: "External Title", Genres: []string{"Action", "Drama"},
				Publishers: []sources.Publisher{{Name: new("External Press")}},
			}},
			Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Publisher: "Override Press"}},
		},
	}}

	inheritChildMetadata(r, &r.content[0], orderedChildren(r))

	row := r.getMetadata("comic/s")
	if !row.dirty {
		t.Fatal("series metadata not marked dirty")
	}
	file := row.DataRaw.File.Raw
	if file.Title != "New Series" || file.Genre != "Child Genre" || file.Language != "en" {
		t.Fatalf("file layer = %+v", file)
	}
	if file.Publisher != "" {
		t.Fatalf("stale file fields persisted: %+v", file)
	}
	if row.DataRaw.MangaBaka == nil || row.DataRaw.MangaBaka.Raw.Title != "External Title" ||
		row.DataRaw.MangaBaka.Raw.ID != 7 {
		t.Fatalf("external layer = %+v", row.DataRaw.MangaBaka)
	}
	if row.DataRaw.Overrides == nil || row.DataRaw.Overrides.Raw.Publisher != "Override Press" {
		t.Fatalf("overrides = %+v", row.DataRaw.Overrides)
	}

	merged := row.DataRaw.Merge()
	if merged.Title != "External Title" || merged.Genre != "Action, Drama" {
		t.Fatalf("external layer must beat file: %+v", merged)
	}
	if merged.Publisher != "Override Press" {
		t.Fatalf("overrides must beat external: %+v", merged)
	}
	if merged.Language != "en" {
		t.Fatalf("file layer must survive where higher layers are empty: %+v", merged)
	}
}

func TestInheritSkipsSeriesWithoutChildren(t *testing.T) {
	r := newRepository(nil, "library")
	series := models.Content{ID: "p", LibraryID: "library", Type: "comic_series", URIPart: "s", URI: "comic/s"}
	r.metadata = []*metadataRow{{
		URI: "comic/s", LibraryID: "library",
		DataRaw: rawMeta(models.Metadata{Title: "Existing"}),
	}}

	inheritChildMetadata(r, &series, nil)

	row := r.getMetadata("comic/s")
	if row.dirty || row.DataRaw.File.Raw.Title != "Existing" {
		t.Fatalf("row = %+v", row.DataRaw.File.Raw)
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
	if err := os.WriteFile(coverPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(coverPath)
	if err != nil {
		t.Fatal(err)
	}

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

func TestOrderStepsSkipsSelfRename(t *testing.T) {
	set := &SeriesChanges{Ref: SeriesRef{ID: "p1", URI: "comic/S", URIPart: "S"}, OldURI: "comic/S"}
	steps, err := orderSteps(flush{sets: map[string]*SeriesChanges{"p1": set}}, map[string]Key{})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want no identity step for a self-rename", steps)
	}
}
