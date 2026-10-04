package scanner

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"voltis/metadata"
	"voltis/models"
	"voltis/scanner/keys"
)

type Key struct{ Parent, URIPart string }

type SeriesRef struct {
	ID      string
	URI     string
	URIPart string
	Type    string
	FileURI *string
}

type Child struct {
	ID         string
	URIPart    string
	OrderParts []*float32
	CoverURI   *string
	FileMtime  *time.Time
	Invalid    bool
	Meta       metadata.Fields // the file layer
}

// creativeRoles are the staff roles every valid child adds to its series' staff.
var creativeRoles = []string{"author", "writer", "artist", "penciller"}

type dirPick struct {
	stored *string
	dirs   map[string]int
}

func (d *dirPick) add(dir string) {
	if d.dirs == nil {
		d.dirs = map[string]int{}
	}
	d.dirs[dir]++
}

func (d *dirPick) drop(dir string) {
	if n := d.dirs[dir]; n > 1 {
		d.dirs[dir] = n - 1
	} else {
		delete(d.dirs, dir)
	}
}

func (d *dirPick) pick() *string {
	if len(d.dirs) == 0 || (d.stored != nil && d.dirs[*d.stored] > 0) {
		return d.stored
	}
	return new(slices.Min(slices.Collect(maps.Keys(d.dirs))))
}

type write struct {
	id   string
	item *ParsedItem
	tick int
}

type deletion struct {
	id   string
	tick int
}

type SeriesChanges struct {
	Ref     SeriesRef
	OldURI  string
	New     bool
	Writes  []write
	Invalid []string
	Deletes []deletion
}

type flush struct {
	seq   int
	at    time.Time
	sets  map[string]*SeriesChanges
	gone  map[string]bool
	final bool
	keep  bool
}

type step struct {
	set   *SeriesChanges
	write *write
}

func (s *SeriesChanges) renamed() bool { return s.OldURI != "" && s.OldURI != s.Ref.URI }

func order(children []Child) []Child {
	ordered := slices.Clone(children)
	slices.SortStableFunc(ordered, func(a, b Child) int {
		return cmp.Or(compareOrderParts(a.OrderParts, b.OrderParts),
			strings.Compare(a.URIPart, b.URIPart), strings.Compare(a.ID, b.ID))
	})
	return ordered
}

// seriesLayer derives a series' file layer from its children's, never from their overrides.
// Book series have no folder, so those steps are skipped.
func seriesLayer(ref SeriesRef, ordered []Child) metadata.Fields {
	var f metadata.Fields
	var folder string
	var folderYear *int
	if ref.FileURI != nil {
		folder, folderYear = keys.ParseSeriesName(filepath.Base(*ref.FileURI))
		f.AltTitles = metadata.Val([]string{folder})
	}
	var earliest string
	var staff []metadata.Staff
	for _, child := range ordered {
		m := child.Meta.Normalize()
		f.Title = first(f.Title, m.Series)
		if s, _ := m.Staff.Get(); len(s) > 0 && !child.Invalid {
			base := staff == nil
			for _, e := range s {
				if base || slices.Contains(creativeRoles, e.Role) {
					staff = append(staff, e)
				}
			}
		}
		f.Publishers = first(f.Publishers, m.Publishers)
		f.Language = first(f.Language, m.Language)
		f.Genres = first(f.Genres, m.Genres)
		f.ContentRating = first(f.ContentRating, m.ContentRating)
		f.Manga = first(f.Manga, m.Manga)
		if d, ok := m.PublicationDate.Get(); ok && (earliest == "" || d < earliest) {
			earliest = d
		}
	}
	if staff != nil {
		f.Staff = metadata.Fields{Staff: metadata.Val(staff)}.Normalize().Staff
	}
	f.Title = first(f.Title, metadata.Set(folder), metadata.Val(ref.URIPart))
	switch {
	case folderYear != nil:
		f.PublicationDate = metadata.Val(fmt.Sprintf("%04d", *folderYear))
	case earliest != "":
		f.PublicationDate = metadata.Val(earliest)
	}
	return f
}

// first returns the first option that holds a value.
func first[T any](opts ...metadata.Opt[T]) metadata.Opt[T] {
	for _, o := range opts {
		if o.P == metadata.Value {
			return o
		}
	}
	return metadata.Opt[T]{}
}

func leafRow(id, libraryID, uri string, p ParsedItem, parentID *string, old *models.Content, now time.Time) models.Content {
	c := models.Content{
		ID:         id,
		LibraryID:  libraryID,
		CreatedAt:  now,
		UpdatedAt:  now,
		Type:       p.ContentType,
		URI:        uri,
		URIPart:    p.URIPart,
		Valid:      true,
		FileURI:    new(p.File.Path),
		FileMtime:  new(p.File.Mtime.UTC()),
		FileSize:   new(int(p.File.Size)),
		OrderParts: p.OrderParts,
		FileData:   p.FileData,
		ParentID:   parentID,
		WordCount:  p.WordCount,
		PageCount:  p.PageCount,
	}
	if old != nil {
		c.CreatedAt = old.CreatedAt
		c.Order = old.Order
		c.CoverURI = old.CoverURI
	}
	if p.CoverSuffix != nil {
		c.CoverURI = new(p.File.Path + "/" + *p.CoverSuffix)
	}
	return c
}

func (s step) id() string {
	if s.write == nil {
		return s.set.Ref.ID
	}
	return s.write.id
}

func (s step) claim() Key {
	if s.write == nil {
		return Key{"", s.set.Ref.URIPart}
	}
	return Key{s.set.Ref.ID, s.write.item.URIPart}
}

func orderSteps(f flush, key map[string]Key) ([]step, error) {
	var all []step
	ident, holder := map[string]int{}, map[Key]int{}
	for _, id := range slices.Sorted(maps.Keys(f.sets)) {
		s := f.sets[id]
		if s.New || s.renamed() {
			ident[id] = len(all)
			all = append(all, step{set: s})
		}
		for i := range s.Writes {
			all = append(all, step{s, &s.Writes[i]})
		}
	}
	for i, st := range all {
		if k, ok := key[st.id()]; ok && k != st.claim() {
			holder[k] = i
		}
	}
	out, state := make([]step, 0, len(all)), make([]uint8, len(all))
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case 1:
			return fmt.Errorf("key cycle at %s", all[i].id())
		case 2:
			return nil
		}
		state[i] = 1
		var deps []int
		if j, ok := holder[all[i].claim()]; ok {
			deps = append(deps, j)
		}
		if j, ok := ident[all[i].set.Ref.ID]; ok && all[i].write != nil {
			deps = append(deps, j)
		}
		for _, j := range deps {
			if err := visit(j); err != nil {
				return err
			}
		}
		state[i] = 2
		out = append(out, all[i])
		return nil
	}
	for i := range all {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func compareOrderParts(a, b []*float32) int {
	for i := range min(len(a), len(b)) {
		ai, bi := a[i], b[i]
		switch {
		case ai == nil && bi == nil:
			continue
		case ai == nil:
			return 1
		case bi == nil:
			return -1
		case *ai < *bi:
			return -1
		case *ai > *bi:
			return 1
		}
	}
	return len(a) - len(b)
}
