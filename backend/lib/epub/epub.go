package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

type Metadata struct {
	Title           string
	Authors         []string
	Series          string
	SeriesIndex     float64
	HasSeriesIndex  bool
	CoverPath       string
	Description     string
	Publisher       string
	Language        string
	PublicationDate string
}

type Chapter struct {
	ID     string
	Href   string
	Title  string
	Linear bool
}

// ReadMetadata extracts metadata from an EPUB file.
func ReadMetadata(filePath string) (*Metadata, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	m := &Metadata{}
	opfPath, opfData, err := readOPF(zr)
	if err != nil {
		return m, nil
	}

	var pkg opfPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return m, nil
	}

	opfDir := path.Dir(opfPath)
	parseDCMetadata(&pkg, m)
	parseCalibreMetadata(&pkg, m)
	findCover(&pkg, m, opfDir)

	return m, nil
}

// ListChapters returns chapters in reading order.
func ListChapters(filePath string) ([]Chapter, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	opfPath, opfData, err := readOPF(zr)
	if err != nil {
		return nil, fmt.Errorf("no OPF file found")
	}

	var pkg opfPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return nil, fmt.Errorf("invalid OPF: %w", err)
	}

	opfDir := path.Dir(opfPath)

	manifest := map[string]manifestItem{}
	for _, item := range pkg.Manifest.Items {
		manifest[item.ID] = item
	}

	titles := parseNavTitles(zr, manifest, opfDir)

	var chapters []Chapter
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		fullHref := resolvePath(opfDir, item.Href)
		title := titles[item.Href]
		if title == "" {
			title = titles[fullHref]
		}
		linear := ref.Linear != "no"
		chapters = append(chapters, Chapter{
			ID:     ref.IDRef,
			Href:   fullHref,
			Title:  title,
			Linear: linear,
		})
	}

	// Skip cover chapter
	if len(chapters) > 0 {
		first := strings.ToLower(chapters[0].Title + " " + chapters[0].ID)
		if strings.Contains(first, "cover") {
			chapters = chapters[1:]
		}
	}

	return chapters, nil
}

// ReadChapter reads raw XHTML content of a chapter by href.
func ReadChapter(filePath string, chapterHref string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == chapterHref {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			var buf strings.Builder
			if _, err := io.Copy(&buf, rc); err != nil {
				return "", err
			}
			return buf.String(), nil
		}
	}
	return "", fmt.Errorf("chapter not found: %s", chapterHref)
}

// OPF XML structures

type opfPackage struct {
	XMLName  xml.Name    `xml:"package"`
	Metadata opfMetadata `xml:"metadata"`
	Manifest opfManifest `xml:"manifest"`
	Spine    opfSpine    `xml:"spine"`
}

type opfMetadata struct {
	Titles      []string  `xml:"title"`
	Creators    []string  `xml:"creator"`
	Description []string  `xml:"description"`
	Publishers  []string  `xml:"publisher"`
	Languages   []string  `xml:"language"`
	Dates       []string  `xml:"date"`
	Metas       []opfMeta `xml:"meta"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Value    string `xml:",chardata"`
}

type opfManifest struct {
	Items []manifestItem `xml:"item"`
}

type manifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfSpine struct {
	ItemRefs []spineItemRef `xml:"itemref"`
}

type spineItemRef struct {
	IDRef  string `xml:"idref,attr"`
	Linear string `xml:"linear,attr"`
}

func readOPF(zr *zip.ReadCloser) (string, []byte, error) {
	// Try container.xml first
	for _, f := range zr.File {
		if f.Name == "META-INF/container.xml" {
			rc, err := f.Open()
			if err != nil {
				break
			}
			defer rc.Close()

			var container struct {
				RootFiles []struct {
					FullPath string `xml:"full-path,attr"`
				} `xml:"rootfiles>rootfile"`
			}
			if err := xml.NewDecoder(rc).Decode(&container); err == nil && len(container.RootFiles) > 0 {
				opfPath := container.RootFiles[0].FullPath
				data, err := readZipFile(zr, opfPath)
				if err == nil {
					return opfPath, data, nil
				}
			}
			break
		}
	}

	// Fallback: find any .opf
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".opf") {
			data, err := readZipFile(zr, f.Name)
			if err == nil {
				return f.Name, data, nil
			}
		}
	}

	return "", nil, fmt.Errorf("no OPF found")
}

func readZipFile(zr *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("file not found: %s", name)
}

func parseDCMetadata(pkg *opfPackage, m *Metadata) {
	md := &pkg.Metadata

	if len(md.Titles) > 0 && strings.TrimSpace(md.Titles[0]) != "" {
		m.Title = strings.TrimSpace(md.Titles[0])
	}
	if len(md.Creators) > 0 {
		for _, c := range md.Creators {
			if s := strings.TrimSpace(c); s != "" {
				m.Authors = append(m.Authors, s)
			}
		}
	}
	if len(md.Description) > 0 && strings.TrimSpace(md.Description[0]) != "" {
		m.Description = strings.TrimSpace(md.Description[0])
	}
	if len(md.Publishers) > 0 && strings.TrimSpace(md.Publishers[0]) != "" {
		m.Publisher = strings.TrimSpace(md.Publishers[0])
	}
	if len(md.Languages) > 0 && strings.TrimSpace(md.Languages[0]) != "" {
		m.Language = strings.TrimSpace(md.Languages[0])
	}
	if len(md.Dates) > 0 && strings.TrimSpace(md.Dates[0]) != "" {
		m.PublicationDate = strings.TrimSpace(md.Dates[0])
	}

	// EPUB3 series (belongs-to-collection)
	for _, meta := range md.Metas {
		if meta.Property == "belongs-to-collection" && strings.TrimSpace(meta.Value) != "" {
			m.Series = strings.TrimSpace(meta.Value)
		} else if meta.Property == "group-position" && strings.TrimSpace(meta.Value) != "" {
			if v, err := parseFloat(meta.Value); err == nil {
				m.SeriesIndex = v
				m.HasSeriesIndex = true
			}
		}
	}
}

func parseCalibreMetadata(pkg *opfPackage, m *Metadata) {
	for _, meta := range pkg.Metadata.Metas {
		content := strings.TrimSpace(meta.Content)
		if content == "" {
			continue
		}
		switch meta.Name {
		case "calibre:series":
			if m.Series == "" {
				m.Series = content
			}
		case "calibre:series_index":
			if !m.HasSeriesIndex {
				if v, err := parseFloat(content); err == nil {
					m.SeriesIndex = v
					m.HasSeriesIndex = true
				}
			}
		}
	}
}

func findCover(pkg *opfPackage, m *Metadata, opfDir string) {
	items := pkg.Manifest.Items

	// Method 1: meta name="cover" -> manifest item
	var coverID string
	for _, meta := range pkg.Metadata.Metas {
		if meta.Name == "cover" {
			coverID = meta.Content
			break
		}
	}
	if coverID != "" {
		for _, item := range items {
			if item.ID == coverID && item.Href != "" {
				m.CoverPath = resolvePath(opfDir, item.Href)
				return
			}
		}
	}

	// Method 2: properties="cover-image"
	for _, item := range items {
		if strings.Contains(item.Properties, "cover-image") && item.Href != "" {
			m.CoverPath = resolvePath(opfDir, item.Href)
			return
		}
	}

	// Method 3: id contains "cover" + image media type
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.ID), "cover") &&
			strings.HasPrefix(item.MediaType, "image/") && item.Href != "" {
			m.CoverPath = resolvePath(opfDir, item.Href)
			return
		}
	}
}

func parseNavTitles(zr *zip.ReadCloser, manifest map[string]manifestItem, opfDir string) map[string]string {
	// Try EPUB3 nav
	for _, item := range manifest {
		if strings.Contains(item.Properties, "nav") {
			navPath := resolvePath(opfDir, item.Href)
			data, err := readZipFile(zr, navPath)
			if err == nil {
				titles := parseEPUB3Nav(data, opfDir)
				if len(titles) > 0 {
					return titles
				}
			}
		}
	}

	// Fallback: toc.ncx
	for _, item := range manifest {
		if item.MediaType == "application/x-dtbncx+xml" {
			ncxPath := resolvePath(opfDir, item.Href)
			data, err := readZipFile(zr, ncxPath)
			if err == nil {
				titles := parseNCXTitles(data, opfDir)
				if len(titles) > 0 {
					return titles
				}
			}
		}
	}

	return map[string]string{}
}

func parseEPUB3Nav(data []byte, opfDir string) map[string]string {
	titles := map[string]string{}

	type anchor struct {
		XMLName xml.Name `xml:"a"`
		Href    string   `xml:"href,attr"`
		Text    string   `xml:",chardata"`
	}
	type li struct {
		A anchor `xml:"a"`
	}
	type ol struct {
		Items []li `xml:"li"`
	}
	type nav struct {
		XMLName xml.Name `xml:"nav"`
		Type    string   `xml:"type,attr"`
		OL      ol       `xml:"ol"`
	}

	// Simple approach: find all <a> elements in toc nav using string scanning
	// since namespace-heavy XHTML is tricky with encoding/xml.
	// We'll use a more manual approach.
	content := string(data)

	// Find toc nav section
	tocIdx := strings.Index(content, `epub:type="toc"`)
	if tocIdx == -1 {
		tocIdx = strings.Index(content, `type="toc"`)
	}
	if tocIdx == -1 {
		return titles
	}

	// Extract anchors after the toc marker
	remaining := content[tocIdx:]
	for {
		aStart := strings.Index(remaining, "<a ")
		if aStart == -1 {
			break
		}
		aEnd := strings.Index(remaining[aStart:], "</a>")
		if aEnd == -1 {
			break
		}
		aEnd += aStart + 4

		aTag := remaining[aStart:aEnd]

		// Extract href
		hrefIdx := strings.Index(aTag, `href="`)
		if hrefIdx == -1 {
			remaining = remaining[aEnd:]
			continue
		}
		hrefStart := hrefIdx + 6
		hrefEnd := strings.Index(aTag[hrefStart:], `"`)
		if hrefEnd == -1 {
			remaining = remaining[aEnd:]
			continue
		}
		href := aTag[hrefStart : hrefStart+hrefEnd]

		// Extract text (strip tags)
		textStart := strings.Index(aTag, ">")
		textContent := aTag[textStart+1 : strings.LastIndex(aTag, "</a>")]
		text := stripTags(textContent)
		text = strings.TrimSpace(text)

		if href != "" && text != "" {
			baseHref := strings.SplitN(href, "#", 2)[0]
			if baseHref != "" {
				fullHref := resolvePath(opfDir, baseHref)
				titles[baseHref] = text
				titles[fullHref] = text
			}
		}

		// Check if we hit a closing nav tag
		navEnd := strings.Index(remaining[aStart:], "</nav>")
		if navEnd != -1 && navEnd+aStart < aEnd {
			break
		}

		remaining = remaining[aEnd:]
	}

	return titles
}

func parseNCXTitles(data []byte, opfDir string) map[string]string {
	titles := map[string]string{}

	type ncxContent struct {
		Src string `xml:"src,attr"`
	}
	type ncxText struct {
		Text string `xml:",chardata"`
	}
	type ncxLabel struct {
		Text ncxText `xml:"text"`
	}
	type navPoint struct {
		Label   ncxLabel   `xml:"navLabel"`
		Content ncxContent `xml:"content"`
	}
	type navMap struct {
		Points []navPoint `xml:"navPoint"`
	}
	type ncx struct {
		XMLName xml.Name `xml:"ncx"`
		NavMap  navMap   `xml:"navMap"`
	}

	var n ncx
	if err := xml.Unmarshal(data, &n); err != nil {
		return titles
	}

	for _, p := range n.NavMap.Points {
		text := strings.TrimSpace(p.Label.Text.Text)
		src := p.Content.Src
		if text != "" && src != "" {
			baseSrc := strings.SplitN(src, "#", 2)[0]
			if baseSrc != "" {
				fullHref := resolvePath(opfDir, baseSrc)
				titles[baseSrc] = text
				titles[fullHref] = text
			}
		}
	}

	return titles
}

func resolvePath(dir, href string) string {
	if dir == "." || dir == "" {
		return href
	}
	return dir + "/" + href
}

func stripTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

// ValidateCoverPath checks if the cover path exists in the EPUB zip.
func ValidateCoverPath(filePath, coverPath string) bool {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return false
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == coverPath {
			return true
		}
	}
	return false
}
