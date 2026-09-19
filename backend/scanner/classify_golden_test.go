package scanner

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZip(t *testing.T, path string, entries map[string][]byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCBZ(t *testing.T, path, comicInfo string) string {
	t.Helper()
	entries := map[string][]byte{"001.jpg": testJPEG(t), "002.jpg": testJPEG(t)}
	if comicInfo != "" {
		entries["ComicInfo.xml"] = []byte(comicInfo)
	}
	return writeZip(t, path, entries)
}

func writeEPUB(t *testing.T, path, opf string) string {
	t.Helper()
	container := `<?xml version="1.0"?><container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`
	return writeZip(t, path, map[string][]byte{
		"META-INF/container.xml": []byte(container),
		"content.opf":            []byte(opf),
		"cover.jpg":              testJPEG(t),
	})
}

func statFile(t *testing.T, path string) FSFile {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return FSFile{Path: path, Mtime: info.ModTime(), Size: info.Size()}
}

func summarize(p *ParsedItem) string {
	if p == nil {
		return "nil"
	}
	parts := make([]string, 0, len(p.OrderParts))
	for _, o := range p.OrderParts {
		if o == nil {
			parts = append(parts, "nil")
			continue
		}
		parts = append(parts, fmt.Sprintf("%g", *o))
	}
	cover := "nil"
	if p.CoverSuffix != nil {
		cover = *p.CoverSuffix
	}
	series := "nil"
	if p.Series != nil {
		series = fmt.Sprintf("%s|%s|%s|%s", p.Series.URIPrefix, p.Series.ContentType, p.Series.URIPart, p.Series.Title)
	}
	return fmt.Sprintf("prefix=%s type=%s part=%s order=[%s] cover=%s title=%s series=%s index=%g data=%s",
		p.URIPrefix, p.ContentType, p.URIPart, strings.Join(parts, ","), cover,
		p.MetaRaw.Title, series, p.MetaRaw.SeriesIndex, string(p.FileData))
}

const comicInfoFull = `<?xml version="1.0"?><ComicInfo>
	<Title>Meta Title</Title>
	<Series>Meta Series</Series>
	<Number>4.5</Number>
	<Volume>2</Volume>
	<Year>2001</Year>
	<Publisher>Meta Press</Publisher>
	<Genre>Action</Genre>
</ComicInfo>`

func TestClassifyComicGoldens(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name      string
		rel       string
		comicInfo string
		want      string
	}{
		{
			"volume and chapter from filename",
			"Series Name (2019)/Series Name v01 ch003.cbz", "",
			"prefix=comic type=comic part=v1_ch3 order=[1,3] cover=001.jpg title=Vol. 1 Ch. 3 series=comic|comic_series|Series Name_2019|Series Name index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"comicinfo overrides filename",
			"Series Name (2019)/Series Name v01 ch003.cbz", comicInfoFull,
			"prefix=comic type=comic part=v2_ch4.5 order=[2,4.5] cover=001.jpg title=Meta Title series=comic|comic_series|Meta Series_2001|Meta Series index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"fallback chapter from digits",
			"Other Series/003 - Something.cbz", "",
			"prefix=comic type=comic part=ch3 order=[nil,3] cover=001.jpg title=Ch. 3 series=comic|comic_series|Other Series|Other Series index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"year only",
			"Yearly/Yearly (1995).cbz", "",
			"prefix=comic type=comic part=y1995 order=[nil,nil] cover=001.jpg title=Yearly (1995) series=comic|comic_series|Yearly|Yearly index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"unidentifiable",
			"Plain/Plain.cbz", "",
			"nil",
		},
	}
	cs := &ComicsScanner{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeCBZ(t, filepath.Join(root, c.name, c.rel), c.comicInfo)
			file := statFile(t, path)
			item := cs.ParseFile(file)
			if got := summarize(item); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
			if item == nil {
				return
			}
			if item.File != file {
				t.Errorf("file = %+v, want %+v", item.File, file)
			}
			if item.Series.FileURI == nil || *item.Series.FileURI != filepath.Dir(path) {
				t.Errorf("series file uri = %v, want %s", item.Series.FileURI, filepath.Dir(path))
			}
		})
	}
}

func opfWith(meta, manifest string) string {
	return `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata>` +
		meta + `</metadata><manifest>` + manifest + `</manifest><spine></spine></package>`
}

func TestClassifyBookGoldens(t *testing.T) {
	root := t.TempDir()
	coverItem := `<item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>`
	cases := []struct {
		name string
		rel  string
		opf  string
		want string
	}{
		{
			"series from calibre metadata",
			"Books/story-one.epub",
			opfWith(`<dc:title>Story One</dc:title><dc:creator>Ann Author</dc:creator>`+
				`<dc:publisher>Pub</dc:publisher><dc:language>en</dc:language><dc:date>2020-01-02</dc:date>`+
				`<meta name="calibre:series" content="Book Series"/><meta name="calibre:series_index" content="2"/>`+
				`<meta name="cover" content="cover-img"/>`, coverItem),
			"prefix=book type=book part=story-one order=[2] cover=cover.jpg title=Story One series=book|book_series|Book Series|Book Series index=2 data=",
		},
		{
			"standalone without series",
			"Books/lonely-book.epub",
			opfWith(`<dc:title>Lonely Book</dc:title><meta name="cover" content="cover-img"/>`, coverItem),
			"prefix=book type=book part=lonely-book order=[0] cover=cover.jpg title=Lonely Book series=nil index=0 data=",
		},
		{
			"missing title falls back to stem",
			"Books/untitled-file.epub",
			opfWith(``, ``),
			"prefix=book type=book part=untitled-file order=[0] cover=nil title=untitled-file series=nil index=0 data=",
		},
		{
			"cover path absent from archive",
			"Books/broken-cover.epub",
			opfWith(`<dc:title>Broken</dc:title><meta name="cover" content="cover-img"/>`,
				`<item id="cover-img" href="missing.jpg" media-type="image/jpeg"/>`),
			"prefix=book type=book part=broken-cover order=[0] cover=nil title=Broken series=nil index=0 data=",
		},
	}
	bs := &BooksScanner{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeEPUB(t, filepath.Join(root, c.name, c.rel), c.opf)
			got := summarize(bs.ParseFile(statFile(t, path)))
			if got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}
