package scanner

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"voltis/models"
	"voltis/models/metaraw"
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
	URI        string
	URIPart    string
	Order      *int
	OrderParts []*float32
	CoverURI   *string
	FileMtime  *time.Time
	Valid      bool
	Meta       metaraw.MetadataRaw
}

type write struct {
	id    string
	item  *ParsedItem
	added bool
}

type SeriesChanges struct {
	Ref     SeriesRef
	OldURI  string
	New     bool
	Writes  []write
	Invalid []string
	Deletes []string
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
		if c := compareOrderParts(a.OrderParts, b.OrderParts); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return ordered
}

func inherit(uriPart string, ordered []Child) models.Metadata {
	var inherited models.Metadata
	for _, child := range ordered {
		m := child.Meta.Merge()
		if inherited.Staff == nil && len(m.Staff) > 0 {
			inherited.Staff = slices.Clone(m.Staff)
		}
		inherited.Publisher = cmp.Or(inherited.Publisher, m.Publisher)
		inherited.Language = cmp.Or(inherited.Language, m.Language)
		inherited.Genre = cmp.Or(inherited.Genre, m.Genre)
		inherited.AgeRating = cmp.Or(inherited.AgeRating, m.AgeRating)
		inherited.Manga = cmp.Or(inherited.Manga, m.Manga)
		inherited.Imprint = cmp.Or(inherited.Imprint, m.Imprint)
		inherited.Description = cmp.Or(inherited.Description, m.Description)
		inherited.PublicationDate = cmp.Or(inherited.PublicationDate, m.PublicationDate)
		inherited.Title = cmp.Or(inherited.Title, m.Series)
	}
	inherited.Title = cmp.Or(inherited.Title, uriPart)
	return inherited
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
		if ai == nil && bi == nil {
			continue
		}
		if ai == nil {
			return 1
		}
		if bi == nil {
			return -1
		}
		if *ai < *bi {
			return -1
		}
		if *ai > *bi {
			return 1
		}
	}
	return len(a) - len(b)
}
