package epub

import (
	"archive/zip"
	"bytes"
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// minSpineWords is the weight carried by a document with no text at all.
const minSpineWords = 10

// separatorElements break the text flow. Everything else, known or not, is
// treated as inline so unrecognized markup cannot split a word; <br> separates
// but <wbr> is only a line-break opportunity.
var separatorElements = map[atom.Atom]bool{
	atom.Address: true, atom.Article: true, atom.Aside: true, atom.Blockquote: true,
	atom.Br: true, atom.Caption: true, atom.Center: true, atom.Col: true,
	atom.Colgroup: true, atom.Dd: true, atom.Details: true, atom.Dialog: true,
	atom.Dir: true, atom.Div: true, atom.Dl: true, atom.Dt: true, atom.Fieldset: true,
	atom.Figcaption: true, atom.Figure: true, atom.Footer: true, atom.Form: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true,
	atom.H6: true, atom.Header: true, atom.Hgroup: true, atom.Hr: true,
	atom.Legend: true, atom.Li: true, atom.Main: true, atom.Menu: true, atom.Nav: true,
	atom.Noscript: true, atom.Ol: true, atom.Optgroup: true, atom.Option: true,
	atom.P: true, atom.Pre: true, atom.Section: true, atom.Summary: true,
	atom.Table: true, atom.Tbody: true, atom.Td: true, atom.Textarea: true,
	atom.Tfoot: true, atom.Th: true, atom.Thead: true, atom.Tr: true, atom.Ul: true,
}

// WordCounts holds the body-text word counts of an EPUB's spine documents.
type WordCounts struct {
	// Docs is keyed by ZIP entry name; textless documents carry minSpineWords so reader weights
	// stay non-zero.
	Docs map[string]int
	// Linear sums the raw counts of documents referenced by a linear itemref.
	Linear int
	// FixedLayout is set for pre-paginated books, whose word count is not a useful length.
	FixedLayout bool
}

// CountWords counts the body-text words of every spine document.
func CountWords(filePath string) (*WordCounts, error) {
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

	wc := &WordCounts{
		Docs: map[string]int{},
		FixedLayout: slices.ContainsFunc(pkg.Metadata.Metas, func(m opfMeta) bool {
			return m.Property == "rendition:layout" && strings.TrimSpace(m.Value) == "pre-paginated"
		}),
	}
	linear := map[string]bool{}
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		target, err := resolveTarget(opfPath, item.Href)
		if err != nil {
			continue
		}
		if ref.Linear != "no" {
			linear[target.Href] = true
		}
		if _, done := wc.Docs[target.Href]; done {
			continue
		}
		data, err := readZipFile(zr, target.Href)
		if err != nil {
			continue
		}
		wc.Docs[target.Href] = countWords(normalizedText(data))
	}
	for href, n := range wc.Docs {
		if linear[href] {
			wc.Linear += n
		}
		wc.Docs[href] = cmp.Or(n, minSpineWords)
	}
	return wc, nil
}

// countWords counts UAX #29 word tokens holding at least one letter or digit.
// Han ideographs segment one per token and kana in runs, which is what a
// language-aware reading speed wants.
func countWords(text string) int {
	count, state := 0, -1
	for rest := text; rest != ""; {
		var word string
		word, rest, state = uniseg.FirstWordInString(rest, state)
		if strings.ContainsFunc(word, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			count++
		}
	}
	return count
}

// normalizedText flattens body text, separating block elements but keeping text
// continuous across inline ones.
func normalizedText(data []byte) string {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	body := findNode(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body })
	if body == nil {
		body = doc
	}

	var b strings.Builder
	pendingSpace := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			for _, r := range n.Data {
				if unicode.IsSpace(r) {
					pendingSpace = b.Len() > 0
					continue
				}
				if pendingSpace {
					b.WriteByte(' ')
					pendingSpace = false
				}
				b.WriteRune(r)
			}
			return
		}
		if n.DataAtom == atom.Script || n.DataAtom == atom.Style {
			return
		}
		separates := n.Type == html.ElementNode && separatorElements[n.DataAtom]
		if separates {
			pendingSpace = b.Len() > 0
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if separates {
			pendingSpace = b.Len() > 0
		}
	}
	walk(body)
	return b.String()
}
