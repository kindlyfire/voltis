package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"slices"
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

// ReadMetadata extracts metadata from an EPUB file.
func ReadMetadata(filePath string) (*Metadata, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	m := &Metadata{}
	opfPath, opfData, err := readOPF(zr)
	if err != nil {
		return m, nil
	}

	var pkg opfPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return m, nil
	}

	parseDCMetadata(&pkg, m)
	parseCalibreMetadata(&pkg, m)
	findCover(&pkg, m, opfPath)

	return m, nil
}

// ReadChapter reads raw XHTML content of a chapter by href.
func ReadChapter(filePath string, chapterHref string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name == chapterHref {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer func() { _ = rc.Close() }()
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
	Toc      string         `xml:"toc,attr"`
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
			defer func() { _ = rc.Close() }()

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
			defer func() { _ = rc.Close() }()
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

func findCover(pkg *opfPackage, m *Metadata, opfPath string) {
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
			if item.ID == coverID {
				if target, err := resolveTarget(opfPath, item.Href); err == nil {
					m.CoverPath = target.Href
					return
				}
			}
		}
	}

	// Method 2: properties="cover-image"
	for _, item := range items {
		if slices.Contains(strings.Fields(item.Properties), "cover-image") {
			if target, err := resolveTarget(opfPath, item.Href); err == nil {
				m.CoverPath = target.Href
				return
			}
		}
	}

	// Method 3: id contains "cover" + image media type
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.ID), "cover") && strings.HasPrefix(item.MediaType, "image/") {
			if target, err := resolveTarget(opfPath, item.Href); err == nil {
				m.CoverPath = target.Href
				return
			}
		}
	}
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
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name == coverPath {
			return true
		}
	}
	return false
}
