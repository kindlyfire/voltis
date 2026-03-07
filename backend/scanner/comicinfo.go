package scanner

import (
	"encoding/xml"
	"fmt"
	"strings"

	"voltis/models/contentmeta"
)

// ComicInfo represents the ComicInfo.xml schema used in CBZ/CBR files.
type ComicInfo struct {
	Title           string `xml:"Title" json:"title,omitempty"`
	Series          string `xml:"Series" json:"series,omitempty"`
	Number          string `xml:"Number" json:"number,omitempty"`
	Count           int    `xml:"Count" json:"count,omitempty"`
	Volume          int    `xml:"Volume" json:"volume,omitempty"`
	AlternateSeries string `xml:"AlternateSeries" json:"alternate_series,omitempty"`
	AlternateNumber string `xml:"AlternateNumber" json:"alternate_number,omitempty"`
	AlternateCount  int    `xml:"AlternateCount" json:"alternate_count,omitempty"`
	Summary         string `xml:"Summary" json:"summary,omitempty"`
	Notes           string `xml:"Notes" json:"notes,omitempty"`
	Year            int    `xml:"Year" json:"year,omitempty"`
	Month           int    `xml:"Month" json:"month,omitempty"`
	Day             int    `xml:"Day" json:"day,omitempty"`
	Writer          string `xml:"Writer" json:"writer,omitempty"`
	Penciller       string `xml:"Penciller" json:"penciller,omitempty"`
	Inker           string `xml:"Inker" json:"inker,omitempty"`
	Colorist        string `xml:"Colorist" json:"colorist,omitempty"`
	Letterer        string `xml:"Letterer" json:"letterer,omitempty"`
	CoverArtist     string `xml:"CoverArtist" json:"cover_artist,omitempty"`
	Editor          string `xml:"Editor" json:"editor,omitempty"`
	Publisher       string `xml:"Publisher" json:"publisher,omitempty"`
	Imprint         string `xml:"Imprint" json:"imprint,omitempty"`
	Genre           string `xml:"Genre" json:"genre,omitempty"`
	Web             string `xml:"Web" json:"web,omitempty"`
	LanguageISO     string `xml:"LanguageISO" json:"language_iso,omitempty"`
	Format          string `xml:"Format" json:"format,omitempty"`
	AgeRating       string `xml:"AgeRating" json:"age_rating,omitempty"`
	Manga           string `xml:"Manga" json:"manga,omitempty"`
	BlackAndWhite   string `xml:"BlackAndWhite" json:"black_and_white,omitempty"`
	Characters      string `xml:"Characters" json:"characters,omitempty"`
	Teams           string `xml:"Teams" json:"teams,omitempty"`
	Locations       string `xml:"Locations" json:"locations,omitempty"`
	StoryArc        string `xml:"StoryArc" json:"story_arc,omitempty"`
	SeriesGroup     string `xml:"SeriesGroup" json:"series_group,omitempty"`
	ScanInformation string `xml:"ScanInformation" json:"scan_information,omitempty"`
}

func parseComicInfo(data []byte) (*ComicInfo, error) {
	var ci ComicInfo
	if err := xml.Unmarshal(data, &ci); err != nil {
		return nil, err
	}
	return &ci, nil
}

// comicInfoToMetadata converts a ComicInfo into a Metadata struct.
func comicInfoToMetadata(ci *ComicInfo) contentmeta.Metadata {
	m := contentmeta.Metadata{}

	clean := func(s string) string {
		if s == "Unknown" {
			return ""
		}
		return s
	}

	m.Title = clean(ci.Title)
	m.Description = ci.Summary
	m.Language = ci.LanguageISO
	m.Series = clean(ci.Series)
	m.Number = clean(ci.Number)
	m.Publisher = clean(ci.Publisher)
	m.Genre = clean(ci.Genre)
	m.AgeRating = clean(ci.AgeRating)
	m.Manga = clean(ci.Manga)
	m.Imprint = clean(ci.Imprint)
	m.Format = clean(ci.Format)
	m.Web = clean(ci.Web)
	m.Notes = clean(ci.Notes)
	m.ScanInformation = clean(ci.ScanInformation)
	m.BlackAndWhite = clean(ci.BlackAndWhite)
	m.SeriesGroup = clean(ci.SeriesGroup)
	m.AlternateSeries = clean(ci.AlternateSeries)
	m.AlternateNumber = clean(ci.AlternateNumber)
	m.Volume = ci.Volume
	m.Count = ci.Count
	m.AlternateCount = ci.AlternateCount

	if ci.Year != 0 {
		if ci.Month != 0 && ci.Day != 0 {
			m.PublicationDate = fmt.Sprintf("%04d-%02d-%02d", ci.Year, ci.Month, ci.Day)
		} else {
			m.PublicationDate = fmt.Sprintf("%d", ci.Year)
		}
	}

	// Staff
	addStaff := func(field, role string) {
		if field == "" || field == "Unknown" {
			return
		}
		for name := range strings.SplitSeq(field, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				m.Staff = append(m.Staff, contentmeta.StaffEntry{Name: name, Role: role})
			}
		}
	}
	addStaff(ci.Writer, "writer")
	addStaff(ci.Penciller, "penciller")
	addStaff(ci.Inker, "inker")
	addStaff(ci.Colorist, "colorist")
	addStaff(ci.Letterer, "letterer")
	addStaff(ci.CoverArtist, "cover_artist")
	addStaff(ci.Editor, "editor")

	return m
}
