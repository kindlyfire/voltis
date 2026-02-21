package scanner

import (
	"encoding/xml"
	"fmt"
	"strings"
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

// comicInfoToMetadata converts a ComicInfo into a flat metadata map.
func comicInfoToMetadata(ci *ComicInfo) map[string]any {
	m := map[string]any{}

	if ci.Summary != "" {
		m["description"] = ci.Summary
	}
	if ci.LanguageISO != "" {
		m["language"] = ci.LanguageISO
	}

	if ci.Year != 0 {
		if ci.Month != 0 && ci.Day != 0 {
			m["publication_date"] = fmt.Sprintf("%04d-%02d-%02d", ci.Year, ci.Month, ci.Day)
		} else {
			m["publication_date"] = fmt.Sprintf("%d", ci.Year)
		}
	}

	if ci.Writer != "" {
		authors := strings.Split(ci.Writer, ",")
		for i := range authors {
			authors[i] = strings.TrimSpace(authors[i])
		}
		m["authors"] = authors
	}

	// Direct fields
	set := func(key, val string) {
		if val != "" && val != "Unknown" {
			m[key] = val
		}
	}
	set("title", ci.Title)
	set("series", ci.Series)
	set("number", ci.Number)
	set("publisher", ci.Publisher)
	set("genre", ci.Genre)
	set("age_rating", ci.AgeRating)
	set("manga", ci.Manga)
	set("imprint", ci.Imprint)
	set("format", ci.Format)
	set("web", ci.Web)
	set("notes", ci.Notes)
	set("scan_information", ci.ScanInformation)
	set("black_and_white", ci.BlackAndWhite)
	set("characters", ci.Characters)
	set("teams", ci.Teams)
	set("locations", ci.Locations)
	set("story_arc", ci.StoryArc)
	set("series_group", ci.SeriesGroup)
	set("penciller", ci.Penciller)
	set("inker", ci.Inker)
	set("colorist", ci.Colorist)
	set("letterer", ci.Letterer)
	set("cover_artist", ci.CoverArtist)
	set("editor", ci.Editor)
	set("alternate_series", ci.AlternateSeries)
	set("alternate_number", ci.AlternateNumber)

	if ci.Volume != 0 {
		m["volume"] = ci.Volume
	}
	if ci.Count != 0 {
		m["count"] = ci.Count
	}
	if ci.AlternateCount != 0 {
		m["alternate_count"] = ci.AlternateCount
	}

	return m
}
