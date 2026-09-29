package routes

import (
	"cmp"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/labstack/echo/v4"
)

const opdsV2Type = "application/opds+json"

// opdsV2Role maps metadata.StaffRoles to RWPM contributor keys.
var opdsV2Role = map[string]string{
	"author": "author", "writer": "author", "artist": "artist", "cover_artist": "artist",
	"penciller": "penciler", "inker": "inker", "colorist": "colorist", "letterer": "letterer",
	"editor": "editor", "translator": "translator",
}

// RWPM types published as a date or date-time, so partial dates are left out.
var fullDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type v2Link struct {
	Rel        string       `json:"rel,omitempty"`
	Href       string       `json:"href"`
	Type       string       `json:"type,omitempty"`
	Title      string       `json:"title,omitempty"`
	Templated  bool         `json:"templated,omitempty"`
	Properties *v2LinkProps `json:"properties,omitempty"`
}

type v2LinkProps struct {
	NumberOfItems *int `json:"numberOfItems,omitempty"`
}

type v2Feed struct {
	Metadata struct {
		Title         string    `json:"title"`
		Modified      time.Time `json:"modified"`
		NumberOfItems *int      `json:"numberOfItems,omitempty"`
		ItemsPerPage  *int      `json:"itemsPerPage,omitempty"`
		CurrentPage   *int      `json:"currentPage,omitempty"`
	} `json:"metadata"`
	Links      []v2Link  `json:"links"`
	Facets     []v2Group `json:"facets,omitempty"`
	Navigation []v2Link  `json:"navigation,omitempty"`
	// Nil in a navigation feed; an empty acquisition feed still has "publications": []. The schema
	// wants at least one item, but a feed with neither collection would fail it too.
	Publications []v2Publication `json:"publications,omitzero"`
}

type v2Group struct {
	Metadata struct {
		Title string `json:"title"`
	} `json:"metadata"`
	Links []v2Link `json:"links"`
}

type v2Named struct {
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
}

type v2Publication struct {
	Metadata map[string]any `json:"metadata"` // absent values are left out, never "" or []
	Links    []v2Link       `json:"links"`
	Images   []v2Link       `json:"images"`
}

func renderV2(c echo.Context, l opdsLinks, f opdsFeed) error {
	var out v2Feed
	out.Metadata.Title = f.Title
	out.Metadata.Modified = f.Updated.UTC()
	out.Links = []v2Link{
		{Rel: "self", Href: f.Self, Type: opdsV2Type},
		{Rel: "start", Href: l.feed("/catalog", nil), Type: opdsV2Type},
	}
	if f.Up != "" {
		out.Links = append(out.Links, v2Link{Rel: "up", Href: f.Up, Type: opdsV2Type})
	}
	out.Links = append(out.Links, v2Link{Rel: "search", Href: l.feed("/search", nil) + "{?query}", Type: opdsV2Type,
		Templated: true})
	if p := f.Page; p != nil {
		out.Metadata.NumberOfItems, out.Metadata.ItemsPerPage, out.Metadata.CurrentPage = &p.Total, &p.Size, &p.Number
		for _, pl := range []struct{ rel, href string }{
			{"first", p.First}, {"previous", p.Prev}, {"next", p.Next}, {"last", p.Last},
		} {
			if pl.href != "" {
				out.Links = append(out.Links, v2Link{Rel: pl.rel, Href: pl.href, Type: opdsV2Type})
			}
		}
	}
	for _, g := range f.Facets {
		var group v2Group
		group.Metadata.Title = g.Title
		for _, fc := range g.Facets {
			link := v2Link{Href: fc.Href, Title: fc.Title, Type: opdsV2Type}
			if fc.Active {
				link.Rel = "self" // 2.0 has no active-facet marker
			}
			group.Links = append(group.Links, link)
		}
		out.Facets = append(out.Facets, group)
	}

	if f.Kind == kindNavigation {
		for _, e := range f.Entries {
			if e.Nav == nil {
				continue
			}
			link := v2Link{Rel: "subsection", Href: e.Nav.Href, Type: opdsV2Type, Title: e.Title}
			if e.Nav.Count != nil {
				link.Properties = &v2LinkProps{NumberOfItems: e.Nav.Count}
			}
			out.Navigation = append(out.Navigation, link)
		}
	} else {
		out.Publications = []v2Publication{}
		for _, e := range f.Entries {
			if e.Acq != nil {
				out.Publications = append(out.Publications, v2PublicationFor(l, e))
			}
		}
	}

	body, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, opdsV2Type, body)
}

func v2PublicationFor(l opdsLinks, e opdsEntry) v2Publication {
	m := map[string]any{"@type": "http://schema.org/Book", "identifier": e.ID, "title": e.Title,
		"modified": e.Updated.UTC()}
	if fullDate.MatchString(e.Issued) {
		m["published"] = e.Issued
	}
	if e.Language != "" {
		m["language"] = e.Language
	}
	if e.Summary != "" {
		m["description"] = e.Summary
	}
	if e.Publisher != "" {
		m["publisher"] = []v2Named{{Name: e.Publisher}}
	}
	if len(e.Genres) > 0 {
		subjects := make([]v2Named, len(e.Genres))
		for i, g := range e.Genres {
			subjects[i] = v2Named{Name: slugTitle(g), Code: g}
		}
		m["subject"] = subjects
	}
	roles := map[string][]v2Named{}
	for _, s := range e.Staff {
		role := cmp.Or(opdsV2Role[s.Role], "contributor")
		roles[role] = append(roles[role], v2Named{Name: s.Name})
	}
	for role, names := range roles {
		m[role] = names
	}
	// Readium-based clients reject a publication without images, so an entry without a cover
	// still links the cover route, which may 404.
	return v2Publication{
		Metadata: m,
		Links:    []v2Link{{Rel: "http://opds-spec.org/acquisition", Href: e.Acq.Href, Type: e.Acq.Type}},
		Images:   []v2Link{{Href: cmp.Or(e.Cover, l.bin("/cover/"+e.ContentID)), Type: coverMediaType}},
	}
}
