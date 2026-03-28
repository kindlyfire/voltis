package metaraw

import (
	"encoding/json"

	"voltis/lib/fp"
	"voltis/lib/sources"
	"voltis/models"
)

type RawContainer[T any] struct {
	Raw T `json:"raw"`
}

// MetadataRaw holds the per-source raw data for a content_metadata row.
type MetadataRaw struct {
	File      *RawContainer[models.Metadata] `json:"file,omitempty"`
	MangaBaka *RawContainer[sources.Series]  `json:"mangabaka,omitempty"`
	Overrides *RawContainer[models.Metadata] `json:"overrides,omitempty"`
}

// Layer is a single source layer with its name, raw JSON, and materialized metadata.
type Layer struct {
	Name         string
	Raw          json.RawMessage
	Materialized models.Metadata
}

var mergeOrder = []string{"file", "mangabaka", "overrides"}

// From parses a data_raw JSON blob into MetadataRaw.
func From(data json.RawMessage) MetadataRaw {
	var mr MetadataRaw
	if data != nil {
		_ = json.Unmarshal(data, &mr)
	}
	return mr
}

// Dump serializes MetadataRaw back to JSON.
func (mr *MetadataRaw) Dump() json.RawMessage {
	data, _ := json.Marshal(mr)
	return data
}

// Merge computes the merged Metadata from all layers in priority order.
func (mr *MetadataRaw) Merge() models.Metadata {
	layers := fp.Map(mr.Layers(), func(l Layer) models.Metadata {
		return l.Materialized
	})
	return models.MergeMetadata(layers...)
}

// Layers returns the source layers in merge order.
func (mr *MetadataRaw) Layers() []Layer {
	result := make([]Layer, 0, len(mergeOrder))
	for _, name := range mergeOrder {
		switch name {
		case "file":
			if mr.File != nil {
				raw, _ := json.Marshal(mr.File.Raw)
				result = append(result, Layer{Name: name, Raw: raw, Materialized: mr.File.Raw})
			}
		case "mangabaka":
			if mr.MangaBaka != nil {
				raw, _ := json.Marshal(mr.MangaBaka.Raw)
				result = append(result, Layer{
					Name:         name,
					Raw:          raw,
					Materialized: sources.MangaBakaSeriesToMetadata(&mr.MangaBaka.Raw),
				})
			}
		case "overrides":
			if mr.Overrides != nil {
				raw, _ := json.Marshal(mr.Overrides.Raw)
				result = append(result, Layer{Name: name, Raw: raw, Materialized: mr.Overrides.Raw})
			}
		}
	}
	return result
}

// EditInPlace parses the JSON in *dataRaw into MetadataRaw, calls fn, and if fn
// returns true, dumps the updated MetadataRaw back into *dataRaw and returns
// the serialized merged Metadata. If fn returns false, nothing is written.
func EditInPlace(dataRaw *json.RawMessage, fn func(*MetadataRaw) bool) (json.RawMessage, error) {
	mr := From(*dataRaw)
	if !fn(&mr) {
		return nil, nil
	}
	*dataRaw = mr.Dump()
	merged, err := json.Marshal(mr.Merge())
	if err != nil {
		return nil, err
	}
	return merged, nil
}
