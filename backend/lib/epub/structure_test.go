package epub

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeEPUB(t *testing.T, entries map[string]string) string {
	t.Helper()
	epubPath := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return epubPath
}

func container(opfPath string) string {
	return `<?xml version="1.0"?><container><rootfiles><rootfile full-path="` + opfPath + `"/></rootfiles></container>`
}

func doc(body string) string {
	return `<?xml version="1.0"?><!DOCTYPE html><html xmlns="http://www.w3.org/1999/xhtml"><head><title>t</title></head><body>` +
		body + `</body></html>`
}

const nestedOPF = `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/" version="3.0"><metadata/>
<manifest>
	<item id="navlike" href="nav/other.xhtml" media-type="application/xhtml+xml" properties="navigation"/>
	<item id="nav" href="nav/nav.xhtml" media-type="application/xhtml+xml" properties="scripted nav"/>
	<item id="cover" href="text/cover.xhtml" media-type="application/xhtml+xml"/>
	<item id="c1" href="text/chapter%201.xhtml" media-type="application/xhtml+xml"/>
	<item id="c2" href="text/chapter2.xhtml" media-type="application/xhtml+xml"/>
	<item id="notes" href="text/notes.xhtml" media-type="application/xhtml+xml"/>
</manifest>
<spine>
	<itemref idref="cover"/>
	<itemref idref="c1"/>
	<itemref idref="c2"/>
	<itemref idref="notes" linear="no"/>
</spine></package>`

const nestedNav = `<?xml version="1.0"?><!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>
<nav epub:type="landmarks"><ol><li><a href="../text/chapter2.xhtml">Start Reading</a></li></ol></nav>
<nav epub:type="toc page-list"><ol>
	<li><a href="../text/cover.xhtml">Cover</a></li>
	<li id="part1"><span>Part One</span>
		<ol>
			<li><a id="c1link" href="../text/chapter%201.xhtml">Chapter 1</a>
				<ol>
					<li><a href="../text/chapter%201.xhtml#s1">Section <span>1</span></a>
						<ol><li><a href="../text/chapter%201.xhtml#s1a">Deep anchor</a></li></ol>
					</li>
				</ol>
			</li>
			<li id="2"><a href="../text/chapter2.xhtml">Chapter 2</a></li>
			<li><a href="../text/chapter2.xhtml#top">Chapter 2 again</a></li>
			<li><a href="../text/chapter2.xhtml#top">Chapter 2, same target</a></li>
		</ol>
	</li>
	<li><a href="../text/notes.xhtml#n1">Notes</a></li>
	<li><a href="https://example.com/">External</a></li>
</ol></nav></body></html>`

func nestedBook(t *testing.T) string {
	t.Helper()
	return writeEPUB(t, map[string]string{
		"META-INF/container.xml":   container("OPS/content.opf"),
		"OPS/content.opf":          nestedOPF,
		"OPS/nav/nav.xhtml":        nestedNav,
		"OPS/nav/other.xhtml":      doc(`<nav epub:type="toc"><ol><li><a href="../text/cover.xhtml">Wrong</a></li></ol></nav>`),
		"OPS/text/cover.xhtml":     doc(`<img src="../images/cover.jpg"/>`),
		"OPS/text/chapter 1.xhtml": doc(`<h1 id="s1">One</h1><p>Hello   there. This paragraph is long enough to clear the floor.</p>`),
		"OPS/text/chapter2.xhtml":  doc(`<p>Two</p>`),
		"OPS/text/notes.xhtml":     doc(`<p id="n1">Note</p>`),
	})
}

func TestBuildStructureNested(t *testing.T) {
	got, err := BuildStructure(nestedBook(t))
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"spine":[` +
		`{"href":"OPS/text/cover.xhtml","title":"Cover","linear":true,"words":0},` +
		`{"href":"OPS/text/chapter 1.xhtml","title":"Chapter 1","linear":true,"words":0},` +
		`{"href":"OPS/text/chapter2.xhtml","title":"Chapter 2","linear":true,"words":0},` +
		`{"href":"OPS/text/notes.xhtml","title":"Notes","linear":false,"words":0}],` +
		`"toc":[` +
		`{"id":"0","title":"Cover","depth":0,"href":"OPS/text/cover.xhtml","fragment":""},` +
		`{"id":"part1","title":"Part One","depth":0,"href":null,"fragment":""},` +
		`{"id":"c1link","title":"Chapter 1","depth":1,"href":"OPS/text/chapter 1.xhtml","fragment":""},` +
		`{"id":"1.0.0","title":"Section 1","depth":2,"href":"OPS/text/chapter 1.xhtml","fragment":"s1"},` +
		`{"id":"1.0.0.0","title":"Deep anchor","depth":3,"href":"OPS/text/chapter 1.xhtml","fragment":"s1a"},` +
		`{"id":"2","title":"Chapter 2","depth":1,"href":"OPS/text/chapter2.xhtml","fragment":""},` +
		`{"id":"1.2","title":"Chapter 2 again","depth":1,"href":"OPS/text/chapter2.xhtml","fragment":"top"},` +
		`{"id":"1.3","title":"Chapter 2, same target","depth":1,"href":"OPS/text/chapter2.xhtml","fragment":"top"},` +
		`{"id":"2_","title":"Notes","depth":0,"href":"OPS/text/notes.xhtml","fragment":"n1"},` +
		`{"id":"3","title":"External","depth":0,"href":null,"fragment":""}]}`
	if string(payload) != want {
		t.Errorf("got  %s\nwant %s", payload, want)
	}
}

func TestNavEntryIDsPreferAuthoredIDs(t *testing.T) {
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
			<item id="c1" href="a.xhtml" media-type="application/xhtml+xml"/>
			<item id="c2" href="b.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine><itemref idref="c1"/><itemref idref="c2"/></spine></package>`,
		"nav.xhtml": doc(`<nav epub:type="toc"><ol>
			<li><a href="a.xhtml">First</a></li>
			<li><a id="0" href="b.xhtml">Second</a></li>
		</ol></nav>`),
		"a.xhtml": doc(`<p>A</p>`),
		"b.xhtml": doc(`<p>B</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	assertTOC(t, got.TOC, []TocEntry{
		{ID: "0_", Title: "First", Href: ptr("a.xhtml")},
		{ID: "0", Title: "Second", Href: ptr("b.xhtml")},
	})
}

func TestNCXSelectedBySpineToc(t *testing.T) {
	ncx := func(title string) string {
		return `<?xml version="1.0"?><ncx><navMap><navPoint id="p1"><navLabel><text>` + title +
			`</text></navLabel><content src="ch1.xhtml"/></navPoint></navMap></ncx>`
	}
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="stale" href="stale.ncx" media-type="application/x-dtbncx+xml"/>
			<item id="real" href="real.ncx" media-type="application/x-dtbncx+xml"/>
			<item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine toc="real"><itemref idref="c1"/></spine></package>`,
		"stale.ncx": ncx("Stale"),
		"real.ncx":  ncx("Real"),
		"ch1.xhtml": doc(`<p>One</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TOC) != 1 || got.TOC[0].Title != "Real" {
		t.Fatalf("toc = %+v, want the NCX named by spine@toc", got.TOC)
	}
}

func TestBuildStructureNCXOnly(t *testing.T) {
	ncx := `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
		<navPoint id="np-1" playOrder="7"><navLabel><text>  Chapter
			One  </text></navLabel><content src="../text/ch1.xhtml"/>
			<navPoint playOrder="1"><navLabel><text>Part A</text></navLabel><content src="../text/ch1.xhtml#a"/>
				<navPoint><navLabel><text>Part A.1</text></navLabel><content src="../text/ch1.xhtml#a1"/></navPoint>
			</navPoint>
		</navPoint>
		<navPoint id="np-1"><navLabel><text>Chapter Two</text></navLabel><content src="../text/ch2.xhtml"/></navPoint>
	</navMap></ncx>`
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("OPS/content.opf"),
		"OPS/content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="toc" href="meta/toc.ncx" media-type="application/x-dtbncx+xml"/>
			<item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
			<item id="c2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine toc="toc"><itemref idref="c1"/><itemref idref="c2"/></spine></package>`,
		"OPS/meta/toc.ncx":   ncx,
		"OPS/text/ch1.xhtml": doc(`<p id="a">One</p>`),
		"OPS/text/ch2.xhtml": doc(`<p>Two</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	want := []TocEntry{
		{ID: "np-1", Title: "Chapter One", Depth: 0, Href: ptr("OPS/text/ch1.xhtml")},
		{ID: "0.0", Title: "Part A", Depth: 1, Href: ptr("OPS/text/ch1.xhtml"), Fragment: "a"},
		{ID: "0.0.0", Title: "Part A.1", Depth: 2, Href: ptr("OPS/text/ch1.xhtml"), Fragment: "a1"},
		{ID: "1", Title: "Chapter Two", Depth: 0, Href: ptr("OPS/text/ch2.xhtml")},
	}
	assertTOC(t, got.TOC, want)
	if got.Spine[0].Title != "Chapter One" || got.Spine[1].Title != "Chapter Two" {
		t.Errorf("spine titles = %q, %q", got.Spine[0].Title, got.Spine[1].Title)
	}
}

func TestBuildStructureNoNav(t *testing.T) {
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine><itemref idref="c1"/></spine></package>`,
		"ch1.xhtml": doc(`<p>One</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TOC) != 0 || len(got.Spine) != 1 || got.Spine[0].Title != "" {
		t.Fatalf("got %+v", got)
	}
	payload, _ := json.Marshal(got)
	if string(payload) != `{"spine":[{"href":"ch1.xhtml","title":"","linear":true,"words":0}],"toc":[]}` {
		t.Errorf("payload = %s", payload)
	}
}

func TestBuildStructureNavPreferredOverNCX(t *testing.T) {
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
			<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
			<item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine toc="ncx"><itemref idref="c1"/></spine></package>`,
		"nav.xhtml": doc(`<nav epub:type="toc"><ol><li><a href="ch1.xhtml">From nav</a></li></ol></nav>`),
		"toc.ncx": `<?xml version="1.0"?><ncx><navMap><navPoint><navLabel><text>From NCX</text></navLabel>
			<content src="ch1.xhtml"/></navPoint></navMap></ncx>`,
		"ch1.xhtml": doc(`<p>One</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TOC) != 1 || got.TOC[0].Title != "From nav" {
		t.Fatalf("toc = %+v", got.TOC)
	}
}

func TestBuildStructureFallsBackWhenNavHasNoTOC(t *testing.T) {
	book := writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata/><manifest>
			<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
			<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
			<item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine toc="ncx"><itemref idref="c1"/></spine></package>`,
		"nav.xhtml": doc(`<nav epub:type="landmarks"><ol><li><a href="ch1.xhtml">Landmark</a></li></ol></nav>`),
		"toc.ncx": `<?xml version="1.0"?><ncx><navMap><navPoint><navLabel><text>From NCX</text></navLabel>
			<content src="ch1.xhtml"/></navPoint></navMap></ncx>`,
		"ch1.xhtml": doc(`<p>One</p>`),
	})

	got, err := BuildStructure(book)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TOC) != 1 || got.TOC[0].Title != "From NCX" {
		t.Fatalf("toc = %+v", got.TOC)
	}
}

func TestBuildStructureKeepsCoverSpineItem(t *testing.T) {
	got, err := BuildStructure(nestedBook(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spine) != 4 || got.Spine[0].Href != "OPS/text/cover.xhtml" {
		t.Fatalf("spine = %+v", got.Spine)
	}
}

func countingBook(t *testing.T, docs map[string]string) map[string]int {
	t.Helper()
	entries := map[string]string{"META-INF/container.xml": container("content.opf")}
	var manifest, spine strings.Builder
	for i, href := range slices.Sorted(maps.Keys(docs)) {
		id := fmt.Sprintf("c%d", i)
		manifest.WriteString(`<item id="` + id + `" href="` + href + `" media-type="application/xhtml+xml"/>`)
		spine.WriteString(`<itemref idref="` + id + `"/>`)
		entries[href] = docs[href]
	}
	entries["content.opf"] = `<?xml version="1.0"?><package><metadata/><manifest>` + manifest.String() +
		`</manifest><spine>` + spine.String() + `</spine></package>`

	counts, err := CountWords(writeEPUB(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	return counts.Docs
}

func assertCounts(t *testing.T, got, want map[string]int) {
	t.Helper()
	for href, n := range want {
		if got[href] != n {
			t.Errorf("%s = %d, want %d", href, got[href], n)
		}
	}
}

func words(s string) int { return len(strings.Fields(s)) }

func TestCountWords(t *testing.T) {
	wc, err := CountWords(nestedBook(t))
	if err != nil {
		t.Fatal(err)
	}
	counts := wc.Docs
	text := "One Hello there. This paragraph is long enough to clear the floor."
	if got := counts["OPS/text/chapter 1.xhtml"]; got != words(text) {
		t.Errorf("chapter 1 words = %d, want %d", got, words(text))
	}
	if got := counts["OPS/text/cover.xhtml"]; got != minSpineWords {
		t.Errorf("textless document words = %d, want the floor %d", got, minSpineWords)
	}
	if len(counts) != 4 {
		t.Errorf("counts = %v, want one entry per spine document", counts)
	}
	// The textless cover adds nothing and the non-linear notes are left out.
	if want := words(text) + 1; wc.Linear != want {
		t.Errorf("linear = %d, want %d", wc.Linear, want)
	}
	if wc.FixedLayout {
		t.Error("reflowable book reported as fixed-layout")
	}
}

func TestCountWordsFixedLayout(t *testing.T) {
	wc, err := CountWords(writeEPUB(t, map[string]string{
		"META-INF/container.xml": container("content.opf"),
		"content.opf": `<?xml version="1.0"?><package><metadata>
			<meta property="rendition:layout"> pre-paginated </meta></metadata>
			<manifest><item id="p1" href="p1.xhtml" media-type="application/xhtml+xml"/></manifest>
			<spine><itemref idref="p1"/></spine></package>`,
		"p1.xhtml": doc(`<p>Overlay</p>`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !wc.FixedLayout || wc.Docs["p1.xhtml"] != 1 {
		t.Errorf("counts = %+v, want fixed-layout with docs filled", wc)
	}
}

func TestCountWordsSeparatesBlocksNotInlineMarkup(t *testing.T) {
	text := "This paragraph is comfortably longer than the textless floor."
	counts := countingBook(t, map[string]string{
		"inline.xhtml": doc(`<p>ab<em>cd</em>ef</p>`),
		"wbr.xhtml":    doc(`<p>co<wbr/>operate</p>`),
		"ins.xhtml":    doc(`<p>a<ins>b</ins>c</p>`),
		"del.xhtml":    doc(`<p>a<del>b</del>c</p>`),
		"br.xhtml":     doc(`<p>a<br/>b</p>`),
		"blocks.xhtml": doc(`<p>foo</p><p>bar</p>`),
		"plain.xhtml":  doc(`<p>` + text + `</p>`),
		"marked.xhtml": doc(`<p>This <em>paragraph</em> is <strong>comfortably</strong> longer than the <span>textless</span> floor.</p>`),
	})
	assertCounts(t, counts, map[string]int{
		"inline.xhtml": 1, "wbr.xhtml": 1, "ins.xhtml": 1, "del.xhtml": 1,
		"br.xhtml": 2, "blocks.xhtml": 2,
		"plain.xhtml": words(text), "marked.xhtml": words(text),
	})
}

func TestCountWordsSkipsPunctuation(t *testing.T) {
	counts := countingBook(t, map[string]string{
		"p.xhtml": doc(`<p>Hello, world! -- "yes"?</p>`),
	})
	assertCounts(t, counts, map[string]int{"p.xhtml": 3})
}

func TestCountWordsSegmentsCJK(t *testing.T) {
	counts := countingBook(t, map[string]string{
		"han.xhtml":   doc(`<p>中文字符测试</p>`),
		"mixed.xhtml": doc(`<p>日本語のテキストです。</p>`),
	})
	assertCounts(t, counts, map[string]int{"han.xhtml": 6, "mixed.xhtml": 7})
}

func TestCountWordsFloorAppliesOnlyToTextlessDocuments(t *testing.T) {
	long := "This paragraph is comfortably longer than the textless floor."
	counts := countingBook(t, map[string]string{
		"empty.xhtml": doc(`<img src="cover.jpg"/>`),
		"blank.xhtml": doc("  \n\t "),
		"short.xhtml": doc(`<p>One</p>`),
		"long.xhtml":  doc(`<p>` + long + `</p>`),
	})
	assertCounts(t, counts, map[string]int{
		"empty.xhtml": minSpineWords, "blank.xhtml": minSpineWords,
		"short.xhtml": 1, "long.xhtml": words(long),
	})
}

func TestCountWordsIgnoresScriptsAndHead(t *testing.T) {
	text := "Spaced out with enough body text to stay clear of the floor"
	counts := countingBook(t, map[string]string{
		"ch1.xhtml": `<?xml version="1.0"?><!DOCTYPE html><html><head><title>Ignored title</title>
			<style>p { color: red }</style></head><body>
			<p>  Spaced   out  </p><p>with enough body text to stay clear of the floor</p>
			<script>var ignored = 1;</script></body></html>`,
	})
	assertCounts(t, counts, map[string]int{"ch1.xhtml": words(text)})
}

func ptr(s string) *string { return &s }

func assertTOC(t *testing.T, got, want []TocEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("toc = %+v, want %d entries", got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Title != w.Title || g.Depth != w.Depth || g.Fragment != w.Fragment {
			t.Errorf("entry %d = %+v, want %+v", i, g, w)
			continue
		}
		if (g.Href == nil) != (w.Href == nil) || (g.Href != nil && *g.Href != *w.Href) {
			t.Errorf("entry %d href = %v, want %v", i, g.Href, w.Href)
		}
	}
}
