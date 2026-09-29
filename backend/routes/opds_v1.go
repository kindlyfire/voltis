package routes

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

const opdsCatalogType = "application/atom+xml;profile=opds-catalog"

// opdsSearchType has no kind, because a search result may be a navigation feed. The direct Atom
// search template is a compatibility deviation: OPDS 1.2 searches through OpenSearch, but Kavita
// and Komga emit the template and some clients read only that.
const opdsSearchType = opdsCatalogType

// linkType is the full OPDS 1.2 type of a catalog link.
func linkType(k opdsKind) string {
	if k == kindNavigation {
		return opdsCatalogType + ";kind=navigation"
	}
	return opdsCatalogType + ";kind=acquisition"
}

// encoding/xml has no namespace prefixes of its own, so names carry them literally and the root
// declares them.
type atomFeed struct {
	XMLName      xml.Name    `xml:"feed"`
	NS           string      `xml:"xmlns,attr"`
	NSOPDS       string      `xml:"xmlns:opds,attr"`
	NSDC         string      `xml:"xmlns:dc,attr"`
	NSOpenSearch string      `xml:"xmlns:opensearch,attr"`
	NSPSE        string      `xml:"xmlns:pse,attr"`
	NSThr        string      `xml:"xmlns:thr,attr"`
	ID           string      `xml:"id"`
	Title        string      `xml:"title"`
	Updated      string      `xml:"updated"`
	Author       atomPerson  `xml:"author"`
	Links        []atomLink  `xml:"link"`
	TotalResults *int        `xml:"opensearch:totalResults"`
	ItemsPerPage *int        `xml:"opensearch:itemsPerPage"`
	StartIndex   *int        `xml:"opensearch:startIndex"`
	Entries      []atomEntry `xml:"entry"`
}

type atomPerson struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type atomLink struct {
	Rel          string `xml:"rel,attr"`
	Href         string `xml:"href,attr"`
	Type         string `xml:"type,attr,omitempty"`
	Title        string `xml:"title,attr,omitempty"`
	Length       *int   `xml:"length,attr,omitempty"`
	Count        *int   `xml:"thr:count,attr,omitempty"`
	FacetGroup   string `xml:"opds:facetGroup,attr,omitempty"`
	ActiveFacet  string `xml:"opds:activeFacet,attr,omitempty"`
	PSECount     int    `xml:"pse:count,attr,omitempty"`
	LastRead     int    `xml:"pse:lastRead,attr,omitempty"`
	LastReadDate string `xml:"pse:lastReadDate,attr,omitempty"`
}

type atomText struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type atomCategory struct {
	Term  string `xml:"term,attr"`
	Label string `xml:"label,attr"`
}

type atomEntry struct {
	ID           string         `xml:"id"`
	Title        string         `xml:"title"`
	Updated      string         `xml:"updated"`
	Authors      []atomPerson   `xml:"author"`
	Contributors []atomPerson   `xml:"contributor"`
	Publisher    string         `xml:"dc:publisher,omitempty"`
	Language     string         `xml:"dc:language,omitempty"`
	Issued       string         `xml:"dc:issued,omitempty"` // partial dates as stored
	Categories   []atomCategory `xml:"category"`
	Summary      *atomText      `xml:"summary"`
	Content      atomText       `xml:"content"` // an entry without content needs an alternate link
	Links        []atomLink     `xml:"link"`
}

func atomTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func renderV1(c echo.Context, l opdsLinks, f opdsFeed) error {
	ct := linkType(f.Kind)
	start := l.feed("/catalog", nil)
	out := atomFeed{
		NS:           "http://www.w3.org/2005/Atom",
		NSOPDS:       "http://opds-spec.org/2010/catalog",
		NSDC:         "http://purl.org/dc/terms/",
		NSOpenSearch: "http://a9.com/-/spec/opensearch/1.1/",
		NSPSE:        "http://vaemendis.net/opds-pse/ns",
		NSThr:        "http://purl.org/syndication/thread/1.0",
		ID:           f.ID,
		Title:        f.Title,
		Updated:      atomTime(f.Updated),
		// RFC 4287 requires a feed author unless every entry has one.
		Author: atomPerson{Name: "Voltis", URI: start},
		Links: []atomLink{
			{Rel: "self", Href: f.Self, Type: ct},
			{Rel: "start", Href: start, Type: linkType(kindNavigation)},
		},
	}
	if f.Up != "" {
		out.Links = append(out.Links, atomLink{Rel: "up", Href: f.Up, Type: linkType(kindNavigation)})
	}
	out.Links = append(out.Links,
		atomLink{Rel: "search", Href: l.feed("/opensearch.xml", nil), Type: "application/opensearchdescription+xml"},
		atomLink{Rel: "search", Href: opdsSearchTemplate(l), Type: opdsSearchType},
	)
	if p := f.Page; p != nil {
		for _, pl := range []struct{ rel, href string }{
			{"first", p.First}, {"previous", p.Prev}, {"next", p.Next}, {"last", p.Last},
		} {
			if pl.href != "" {
				out.Links = append(out.Links, atomLink{Rel: pl.rel, Href: pl.href, Type: ct})
			}
		}
		out.TotalResults, out.ItemsPerPage = &p.Total, &p.Size
		out.StartIndex = new((p.Number-1)*p.Size + 1)
	}
	for _, g := range f.Facets {
		for _, fc := range g.Facets {
			link := atomLink{Rel: "http://opds-spec.org/facet", Href: fc.Href, Type: linkType(kindAcquisition),
				Title: fc.Title, FacetGroup: g.Title}
			if fc.Active {
				link.ActiveFacet = "true"
			}
			out.Links = append(out.Links, link)
		}
	}
	for _, e := range f.Entries {
		out.Entries = append(out.Entries, atomEntryFor(e))
	}

	body, err := xml.Marshal(out)
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, ct+";charset=utf-8", append([]byte(xml.Header), body...))
}

func atomEntryFor(e opdsEntry) atomEntry {
	a := atomEntry{ID: e.ID, Title: e.Title, Updated: atomTime(e.Updated), Publisher: e.Publisher,
		Language: e.Language, Issued: e.Issued, Content: atomText{Type: "text", Text: e.Content}}
	for _, s := range e.Staff {
		if s.Role == "author" || s.Role == "writer" {
			a.Authors = append(a.Authors, atomPerson{Name: s.Name})
		} else {
			a.Contributors = append(a.Contributors, atomPerson{Name: s.Name})
		}
	}
	for _, g := range e.Genres {
		a.Categories = append(a.Categories, atomCategory{Term: g, Label: slugTitle(g)})
	}
	if e.Summary != "" {
		a.Summary = &atomText{Type: "text", Text: e.Summary}
	}
	if e.Cover != "" {
		a.Links = append(a.Links,
			atomLink{Rel: "http://opds-spec.org/image", Href: e.Cover, Type: coverMediaType},
			atomLink{Rel: "http://opds-spec.org/image/thumbnail", Href: e.Cover, Type: coverMediaType})
	}
	if e.Nav != nil {
		a.Links = append(a.Links, atomLink{Rel: "subsection", Href: e.Nav.Href, Type: linkType(e.Nav.Kind), Count: e.Nav.Count})
	}
	if e.Acq != nil {
		a.Links = append(a.Links, atomLink{Rel: "http://opds-spec.org/acquisition", Href: e.Acq.Href, Type: e.Acq.Type,
			Length: e.Acq.Size})
	}
	if p := e.PSE; p != nil {
		link := atomLink{Rel: "http://vaemendis.net/opds-pse/stream", Href: p.Template, Type: p.Type, PSECount: p.Count,
			LastRead: p.LastRead}
		if p.LastRead > 0 && p.LastReadAt != nil {
			link.LastReadDate = atomTime(*p.LastReadAt)
		}
		a.Links = append(a.Links, link)
	}
	return a
}

// opdsSearchTemplate is written unescaped: the braces are literal, and only XML escaping applies.
func opdsSearchTemplate(l opdsLinks) string { return l.feed("/search", nil) + "?query={searchTerms}" }

func (o *OPDSRoutes) openSearch(c echo.Context) error {
	type url struct {
		Type     string `xml:"type,attr"`
		Template string `xml:"template,attr"`
	}
	body, err := xml.Marshal(struct {
		XMLName     xml.Name `xml:"http://a9.com/-/spec/opensearch/1.1/ OpenSearchDescription"`
		ShortName   string
		Description string
		URL         url `xml:"Url"`
	}{
		ShortName:   "Voltis",
		Description: "Search the Voltis library",
		URL:         url{Type: opdsSearchType, Template: opdsSearchTemplate(o.links(c, "v1.2"))},
	})
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, "application/opensearchdescription+xml", append([]byte(xml.Header), body...))
}
