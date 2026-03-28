package models

import (
	"encoding/json"
	"reflect"
	"strings"
)

type StaffEntry struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type Metadata struct {
	Title           string       `json:"title,omitempty"`
	Description     string       `json:"description,omitempty"`
	Staff           []StaffEntry `json:"staff,omitempty"`
	Publisher       string       `json:"publisher,omitempty"`
	Language        string       `json:"language,omitempty"`
	PublicationDate string       `json:"publication_date,omitempty"`

	// Comic fields
	Series          string `json:"series,omitempty"`
	Number          string `json:"number,omitempty"`
	Volume          int    `json:"volume,omitempty"`
	Count           int    `json:"count,omitempty"`
	Genre           string `json:"genre,omitempty"`
	AgeRating       string `json:"age_rating,omitempty"`
	Manga           string `json:"manga,omitempty"`
	Format          string `json:"format,omitempty"`
	Imprint         string `json:"imprint,omitempty"`
	Web             string `json:"web,omitempty"`
	Notes           string `json:"notes,omitempty"`
	ScanInformation string `json:"scan_information,omitempty"`
	BlackAndWhite   string `json:"black_and_white,omitempty"`
	SeriesGroup     string `json:"series_group,omitempty"`
	AlternateSeries string `json:"alternate_series,omitempty"`
	AlternateNumber string `json:"alternate_number,omitempty"`
	AlternateCount  int    `json:"alternate_count,omitempty"`

	// Book fields
	SeriesIndex float64 `json:"series_index,omitempty"`

	// Source links
	MangaBakaID *int `json:"mangabaka_id,omitempty"`
}

// Merge combines multiple metadata layers in priority order. Later layers
// override earlier ones. Staff is taken as a whole from the highest-priority
// layer that has it set.
func MergeMetadata(layers ...Metadata) Metadata {
	var result Metadata
	typ := reflect.TypeFor[Metadata]()
	resultElem := reflect.ValueOf(&result).Elem()

	for _, layer := range layers {
		lv := reflect.ValueOf(layer)
		for i := range typ.NumField() {
			field := typ.Field(i)
			src := lv.Field(i)

			if field.Name == "Staff" {
				// Staff: only override if the layer has entries
				if len(layer.Staff) > 0 {
					result.Staff = layer.Staff
				}
				continue
			}

			if !src.IsZero() {
				resultElem.Field(i).Set(src)
			}
		}
	}

	return result
}

// jsonKeyToField maps JSON keys to struct field indices.
var jsonKeyToField = func() map[string]int {
	m := map[string]int{}
	typ := reflect.TypeFor[Metadata]()
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		key, _, _ := strings.Cut(tag, ",")
		if key != "" {
			m[key] = i
		}
	}
	return m
}()

// ParseMetadata parses JSON into Metadata leniently. Unknown keys and values
// that don't match the expected type are silently skipped. Never fails.
func ParseMetadata(data []byte) Metadata {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return Metadata{}
	}

	var result Metadata
	rv := reflect.ValueOf(&result).Elem()
	for key, val := range raw {
		idx, ok := jsonKeyToField[key]
		if !ok {
			continue
		}
		field := rv.Field(idx)
		dst := reflect.New(field.Type())
		if json.Unmarshal(val, dst.Interface()) == nil {
			field.Set(dst.Elem())
		}
	}
	return result
}
