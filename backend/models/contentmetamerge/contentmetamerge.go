package contentmetamerge

import (
	"context"
	"encoding/json"

	"voltis/db"
	"voltis/lib/sources"
	"voltis/models/contentmeta"

	"github.com/jackc/pgx/v5"
)

// MaterializeSource converts raw JSON for a given source into Metadata.
func MaterializeSource(source string, raw json.RawMessage) contentmeta.Metadata {
	switch source {
	case "mangabaka":
		var s sources.Series
		if json.Unmarshal(raw, &s) == nil {
			return sources.MangaBakaSeriesToMetadata(&s)
		}
		return contentmeta.Metadata{}

	default:
		return contentmeta.ParseMetadata(raw)
	}
}

// ExtractRaw extracts the "raw" value from a layer entry. The entry is expected
// to be `{"raw": ...}`. Returns nil if parsing fails.
func ExtractRaw(entry json.RawMessage) json.RawMessage {
	var wrapper map[string]json.RawMessage
	if json.Unmarshal(entry, &wrapper) != nil {
		return nil
	}
	return wrapper["raw"]
}

// MergeRawLayers iterates MergeOrder, extracts the raw from each present layer
// entry, materializes it, and returns the merged Metadata.
func MergeRawLayers(dataRaw map[string]json.RawMessage) contentmeta.Metadata {
	var layers []contentmeta.Metadata
	for _, source := range contentmeta.MergeOrder {
		entry, ok := dataRaw[source]
		if !ok {
			continue
		}
		raw := ExtractRaw(entry)
		if raw == nil {
			continue
		}
		layers = append(layers, MaterializeSource(source, raw))
	}
	return contentmeta.Merge(layers...)
}

// BuildLayerEntry builds the {"raw": ...} JSON blob stored as a layer value.
func BuildLayerEntry(raw any) json.RawMessage {
	if raw == nil {
		raw = json.RawMessage("{}")
	}
	entry, _ := json.Marshal(map[string]any{"raw": raw})
	return entry
}

// SourceLayers is the parsed form of data_raw: source name → layer entry JSON.
type SourceLayers map[string]json.RawMessage

// WithSourceLayers loads the source layers for a content_metadata row, calls fn
// with them, and if fn returns true, serializes the layers back and upserts the
// row (recomputing merged data). If fn returns false, nothing is written. If no
// row exists yet, fn receives an empty SourceLayers and an upsert creates the row.
func WithSourceLayers(
	ctx context.Context, q db.Querier,
	uri, libraryID string,
	fn func(SourceLayers) bool,
) error {
	layers, err := loadSourceLayers(ctx, q, uri, libraryID)
	if err != nil {
		return err
	}

	if !fn(layers) {
		return nil
	}

	dataRawJSON, _ := json.Marshal(layers)
	mergedJSON, _ := json.Marshal(MergeRawLayers(layers))

	_, err = q.Exec(ctx, `
		INSERT INTO content_metadata (uri, library_id, data, data_raw, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (uri, library_id) DO UPDATE
		SET data = EXCLUDED.data, data_raw = EXCLUDED.data_raw, updated_at = now()
	`, uri, libraryID, mergedJSON, dataRawJSON)
	return err
}

func loadSourceLayers(ctx context.Context, q db.Querier, uri, libraryID string) (SourceLayers, error) {
	raw, err := db.SelectScalar[json.RawMessage](ctx, q,
		`SELECT data_raw FROM content_metadata WHERE uri = $1 AND library_id = $2`,
		uri, libraryID)
	if err == pgx.ErrNoRows {
		return SourceLayers{}, nil
	}
	if err != nil {
		return nil, err
	}

	var layers SourceLayers
	if json.Unmarshal(raw, &layers) != nil || layers == nil {
		return SourceLayers{}, nil
	}
	return layers, nil
}
