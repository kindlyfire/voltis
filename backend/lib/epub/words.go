package epub

import (
	"archive/zip"
	"bytes"
	"cmp"
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

// CountWords returns the body-text word count of every spine document, keyed by
// its ZIP entry name.
func CountWords(filePath string) (map[string]int, error) {
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

	counts := map[string]int{}
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		target, err := resolveTarget(opfPath, item.Href)
		if err != nil {
			continue
		}
		if _, done := counts[target.Href]; done {
			continue
		}
		data, err := readZipFile(zr, target.Href)
		if err != nil {
			continue
		}
		counts[target.Href] = cmp.Or(countWords(normalizedText(data)), minSpineWords)
	}
	return counts, nil
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
