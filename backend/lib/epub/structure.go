package epub

import (
	"archive/zip"
	"bytes"
	"cmp"
	"encoding/xml"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type BookStructure struct {
	Spine []SpineItem `json:"spine"`
	TOC   []TocEntry  `json:"toc"`
}

type SpineItem struct {
	Href   string `json:"href"`
	Title  string `json:"title"`
	Linear bool   `json:"linear"`
	Words  int    `json:"words"`
}

type TocEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Depth    int     `json:"depth"`
	Href     *string `json:"href"`
	Fragment string  `json:"fragment"`
}

// BuildStructure returns the whole spine and the TOC flattened into preorder.
func BuildStructure(filePath string) (*BookStructure, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	pkg, opfPath, err := readPackage(zr)
	if err != nil {
		return nil, err
	}

	manifest := manifestByID(pkg)
	toc := parseTOC(zr, pkg, manifest, opfPath)
	titles := tocTitles(toc)

	spine := make([]SpineItem, 0, len(pkg.Spine.ItemRefs))
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		target, err := resolveTarget(opfPath, item.Href)
		if err != nil {
			continue
		}
		spine = append(spine, SpineItem{
			Href:   target.Href,
			Title:  titles[target.Href],
			Linear: ref.Linear != "no",
		})
	}

	return &BookStructure{Spine: spine, TOC: toc}, nil
}

func readPackage(zr *zip.ReadCloser) (*opfPackage, string, error) {
	opfPath, opfData, err := readOPF(zr)
	if err != nil {
		return nil, "", fmt.Errorf("no OPF file found")
	}
	var pkg opfPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return nil, "", fmt.Errorf("invalid OPF: %w", err)
	}
	return &pkg, opfPath, nil
}

func manifestByID(pkg *opfPackage) map[string]manifestItem {
	manifest := make(map[string]manifestItem, len(pkg.Manifest.Items))
	for _, item := range pkg.Manifest.Items {
		manifest[item.ID] = item
	}
	return manifest
}

func tocTitles(toc []TocEntry) map[string]string {
	titles := map[string]string{}
	for _, documentStart := range []bool{true, false} {
		for _, e := range toc {
			if e.Href == nil || titles[*e.Href] != "" || (documentStart && e.Fragment != "") {
				continue
			}
			titles[*e.Href] = e.Title
		}
	}
	return titles
}

func parseTOC(zr *zip.ReadCloser, pkg *opfPackage, manifest map[string]manifestItem, opfPath string) []TocEntry {
	if entries := parseEPUB3Nav(zr, pkg, opfPath); len(entries) > 0 {
		return entries
	}
	if entries := parseNCX(zr, pkg, manifest, opfPath); len(entries) > 0 {
		return entries
	}
	return []TocEntry{}
}

func parseEPUB3Nav(zr *zip.ReadCloser, pkg *opfPackage, opfPath string) []TocEntry {
	for _, item := range pkg.Manifest.Items {
		if !slices.Contains(strings.Fields(item.Properties), "nav") {
			continue
		}
		target, err := resolveTarget(opfPath, item.Href)
		if err != nil {
			continue
		}
		data, err := readZipFile(zr, target.Href)
		if err != nil {
			continue
		}
		if entries := parseNavDocument(data, target.Href); len(entries) > 0 {
			return entries
		}
	}
	return nil
}

func parseNavDocument(data []byte, navPath string) []TocEntry {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	nav := findNode(doc, func(n *html.Node) bool {
		return n.DataAtom == atom.Nav && slices.Contains(strings.Fields(attrValue(n, "epub:type")), "toc")
	})
	if nav == nil {
		return nil
	}
	list := findNode(nav, func(n *html.Node) bool { return n.DataAtom == atom.Ol })
	if list == nil {
		return nil
	}

	var entries []TocEntry
	walkNavList(list, navPath, 0, "", newIDAllocator(elementIDs(list)), &entries)
	return entries
}

func walkNavList(list *html.Node, navPath string, depth int, prefix string, ids *idAllocator, out *[]TocEntry) {
	index := 0
	for li := list.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		treePath := strconv.Itoa(index)
		if prefix != "" {
			treePath = prefix + "." + treePath
		}
		index++

		label := findChild(li, func(n *html.Node) bool {
			return n.DataAtom == atom.A || n.DataAtom == atom.Span
		})
		nested := findChild(li, func(n *html.Node) bool { return n.DataAtom == atom.Ol })

		if label == nil {
			if nested != nil {
				walkNavList(nested, navPath, depth, treePath, ids, out)
			}
			continue
		}

		entry := TocEntry{
			ID:    ids.take(cmp.Or(attrValue(label, "id"), attrValue(li, "id")), treePath),
			Title: labelText(label),
			Depth: depth,
		}
		if href := attrValue(label, "href"); label.DataAtom == atom.A && href != "" {
			if target, err := resolveTarget(navPath, href); err == nil {
				entry.Href = &target.Href
				entry.Fragment = target.Fragment
			}
		}
		*out = append(*out, entry)

		if nested != nil {
			walkNavList(nested, navPath, depth+1, treePath, ids, out)
		}
	}
}

func parseNCX(zr *zip.ReadCloser, pkg *opfPackage, manifest map[string]manifestItem, opfPath string) []TocEntry {
	var hrefs []string
	if item, ok := manifest[pkg.Spine.Toc]; ok && pkg.Spine.Toc != "" {
		hrefs = append(hrefs, item.Href)
	}
	for _, item := range pkg.Manifest.Items {
		if item.MediaType == "application/x-dtbncx+xml" {
			hrefs = append(hrefs, item.Href)
		}
	}

	for _, href := range hrefs {
		target, err := resolveTarget(opfPath, href)
		if err != nil {
			continue
		}
		data, err := readZipFile(zr, target.Href)
		if err != nil {
			continue
		}
		if entries := parseNCXDocument(data, target.Href); len(entries) > 0 {
			return entries
		}
	}
	return nil
}

type ncxNavPoint struct {
	ID       string `xml:"id,attr"`
	NavLabel struct {
		Text string `xml:"text"`
	} `xml:"navLabel"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxNavPoint `xml:"navPoint"`
}

func parseNCXDocument(data []byte, ncxPath string) []TocEntry {
	var doc struct {
		XMLName xml.Name      `xml:"ncx"`
		Points  []ncxNavPoint `xml:"navMap>navPoint"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}

	var entries []TocEntry
	walkNavPoints(doc.Points, ncxPath, 0, "", newIDAllocator(navPointIDs(doc.Points)), &entries)
	return entries
}

func walkNavPoints(points []ncxNavPoint, ncxPath string, depth int, prefix string, ids *idAllocator, out *[]TocEntry) {
	for i, p := range points {
		treePath := strconv.Itoa(i)
		if prefix != "" {
			treePath = prefix + "." + treePath
		}

		entry := TocEntry{
			ID:    ids.take(p.ID, treePath),
			Title: collapseSpace(p.NavLabel.Text),
			Depth: depth,
		}
		if src := strings.TrimSpace(p.Content.Src); src != "" {
			if target, err := resolveTarget(ncxPath, src); err == nil {
				entry.Href = &target.Href
				entry.Fragment = target.Fragment
			}
		}
		*out = append(*out, entry)

		walkNavPoints(p.Children, ncxPath, depth+1, treePath, ids, out)
	}
}

// idAllocator keeps tree-path fallbacks clear of authored ids, so moving a
// sibling cannot rename an entry that has one.
type idAllocator struct{ authored, used map[string]bool }

func newIDAllocator(authored []string) *idAllocator {
	a := &idAllocator{authored: make(map[string]bool, len(authored)), used: map[string]bool{}}
	for _, id := range authored {
		a.authored[id] = true
	}
	return a
}

func (a *idAllocator) take(authored, fallback string) string {
	if authored != "" && !a.used[authored] {
		a.used[authored] = true
		return authored
	}
	id := fallback
	for a.used[id] || a.authored[id] {
		id += "_"
	}
	a.used[id] = true
	return id
}

func elementIDs(root *html.Node) []string {
	var ids []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if id := attrValue(n, "id"); n.Type == html.ElementNode && id != "" {
			ids = append(ids, id)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return ids
}

func navPointIDs(points []ncxNavPoint) []string {
	var ids []string
	for _, p := range points {
		if p.ID != "" {
			ids = append(ids, p.ID)
		}
		ids = append(ids, navPointIDs(p.Children)...)
	}
	return ids
}

func labelText(label *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.Ol || n.DataAtom == atom.Ul) {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(label)
	return collapseSpace(b.String())
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func findNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && match(c) {
			return c
		}
		if found := findNode(c, match); found != nil {
			return found
		}
	}
	return nil
}

func findChild(parent *html.Node, match func(*html.Node) bool) *html.Node {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && match(c) {
			return c
		}
	}
	return nil
}

func attrValue(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
