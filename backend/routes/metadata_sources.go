package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"voltis/lib/sources"
	"voltis/models"
	"voltis/models/contentmeta"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type MetadataSourceRoutes struct {
	pool      *pgxpool.Pool
	mangabaka *sources.MangaBaka
}

func (r *MetadataSourceRoutes) Register(g *echo.Group) {
	g.GET("/mangabaka/search", r.mangabakaSearch)
	g.POST("/mangabaka/link", r.mangabakaLink)
	g.POST("/unlink", r.unlink)
}

type mangaBakaResultDTO struct {
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Year     *int     `json:"year"`
	CoverURL *string  `json:"cover_url"`
	Authors  []string `json:"authors"`
	Genres   []string `json:"genres"`
}

type mangaBakaSearchResponse struct {
	Data []mangaBakaResultDTO `json:"data"`
}

func seriesToDTO(s *sources.Series) mangaBakaResultDTO {
	dto := mangaBakaResultDTO{
		ID:      s.ID,
		Title:   s.Title,
		Type:    s.Type,
		Status:  s.Status,
		Year:    s.Year,
		Authors: s.Authors,
		Genres:  s.Genres,
	}
	if dto.Authors == nil {
		dto.Authors = []string{}
	}
	if dto.Genres == nil {
		dto.Genres = []string{}
	}
	if s.Cover.X250.X1 != nil {
		dto.CoverURL = s.Cover.X250.X1
	} else if s.Cover.Raw.URL != nil {
		dto.CoverURL = s.Cover.Raw.URL
	}
	return dto
}

var comicSeriesTypes = []sources.SeriesType{
	sources.SeriesTypeManga,
	sources.SeriesTypeManhwa,
	sources.SeriesTypeManhua,
	sources.SeriesTypeOEL,
	sources.SeriesTypeOther,
}

var bookSeriesTypes = []sources.SeriesType{
	sources.SeriesTypeNovel,
}

func (r *MetadataSourceRoutes) mangabakaSearch(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	q := strings.TrimSpace(c.QueryParam("q"))
	if q == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "q is required")
	}

	ctx := reqCtx(c)

	// If q is a numeric ID, fetch that single series
	if id, err := strconv.Atoi(q); err == nil && id > 0 {
		series, err := r.mangabaka.SeriesGet(ctx, id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("MangaBaka error: %v", err))
		}
		return c.JSON(http.StatusOK, mangaBakaSearchResponse{
			Data: []mangaBakaResultDTO{seriesToDTO(series)},
		})
	}

	contentType := c.QueryParam("type")
	var seriesTypes []sources.SeriesType
	switch contentType {
	case "comic":
		seriesTypes = comicSeriesTypes
	case "book":
		seriesTypes = bookSeriesTypes
	}

	results, err := r.mangabaka.SeriesSearch(ctx, sources.SeriesSearchOpts{
		Query: q,
		Type:  seriesTypes,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("MangaBaka error: %v", err))
	}

	data := make([]mangaBakaResultDTO, len(results.Data))
	for i := range results.Data {
		data[i] = seriesToDTO(&results.Data[i])
	}
	return c.JSON(http.StatusOK, mangaBakaSearchResponse{Data: data})
}

func seriesToMetadata(s *sources.Series) contentmeta.Metadata {
	meta := contentmeta.Metadata{
		Title: s.Title,
	}
	if s.Description != nil {
		meta.Description = *s.Description
	}

	var staff []contentmeta.StaffEntry
	for _, a := range s.Authors {
		staff = append(staff, contentmeta.StaffEntry{Name: a, Role: "author"})
	}
	for _, a := range s.Artists {
		staff = append(staff, contentmeta.StaffEntry{Name: a, Role: "artist"})
	}
	meta.Staff = staff

	for _, p := range s.Publishers {
		if p.Name != nil && *p.Name != "" {
			meta.Publisher = *p.Name
			break
		}
	}

	if s.Year != nil {
		meta.PublicationDate = strconv.Itoa(*s.Year)
	}
	if len(s.Genres) > 0 {
		meta.Genre = strings.Join(s.Genres, ", ")
	}
	if s.ContentRating != "" {
		meta.AgeRating = string(s.ContentRating)
	}

	switch s.Type {
	case sources.SeriesTypeManga, sources.SeriesTypeManhwa, sources.SeriesTypeManhua:
		meta.Manga = "Yes"
	case sources.SeriesTypeNovel, sources.SeriesTypeOEL:
		meta.Manga = "No"
	}

	return meta
}

type mangaBakaLinkRequest struct {
	ContentID   string `json:"content_id" validate:"required"`
	MangaBakaID int    `json:"mangabaka_id" validate:"required"`
}

func (r *MetadataSourceRoutes) mangabakaLink(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	var req mangaBakaLinkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	ctx := reqCtx(c)

	content, err := getContent(ctx, r.pool, req.ContentID)
	if err != nil {
		return err
	}

	series, err := r.mangabaka.SeriesGet(ctx, req.MangaBakaID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("MangaBaka error: %v", err))
	}

	meta := seriesToMetadata(series)
	rawJSON, _ := json.Marshal(series)

	return setSourceLayer(ctx, r.pool, content, "mangabaka", meta, rawJSON, c)
}

type unlinkRequest struct {
	ContentID string `json:"content_id" validate:"required"`
	Source    string `json:"source" validate:"required"`
}

func (r *MetadataSourceRoutes) unlink(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	var req unlinkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	if req.Source == "file" || req.Source == "overrides" {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot unlink file or overrides source")
	}

	ctx := reqCtx(c)

	content, err := getContent(ctx, r.pool, req.ContentID)
	if err != nil {
		return err
	}

	return removeSourceLayer(ctx, r.pool, content, req.Source, c)
}

// Helpers

func setSourceLayer(
	ctx context.Context,
	pool *pgxpool.Pool, content models.Content,
	source string, meta contentmeta.Metadata, rawJSON json.RawMessage,
	c echo.Context,
) error {
	metaJSON, _ := json.Marshal(meta)
	entry, _ := json.Marshal(map[string]json.RawMessage{
		"data": metaJSON,
		"raw":  rawJSON,
	})

	now := time.Now().UTC()

	var dataRaw json.RawMessage
	err := pool.QueryRow(ctx, `
		SELECT data_raw FROM content_metadata WHERE uri = $1 AND library_id = $2
	`, content.URI, content.LibraryID).Scan(&dataRaw)

	if errors.Is(err, pgx.ErrNoRows) {
		newDataRaw, _ := json.Marshal(map[string]json.RawMessage{source: entry})
		mergedJSON, _ := json.Marshal(contentmeta.MergeRawLayers(map[string]json.RawMessage{source: entry}))

		_, err = pool.Exec(ctx, `
			INSERT INTO content_metadata (id, uri, library_id, data, data_raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, models.MakeContentID(), content.URI, content.LibraryID, mergedJSON, newDataRaw, now)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		rawMap := parseDataRaw(dataRaw)
		rawMap[source] = entry

		mergedJSON, _ := json.Marshal(contentmeta.MergeRawLayers(rawMap))
		updatedRaw, _ := json.Marshal(rawMap)

		_, err = pool.Exec(ctx, `
			UPDATE content_metadata SET data = $1, data_raw = $2, updated_at = $3
			WHERE uri = $4 AND library_id = $5
		`, mergedJSON, updatedRaw, now, content.URI, content.LibraryID)
		if err != nil {
			return err
		}
	}

	return okResponse(c)
}

func removeSourceLayer(
	ctx context.Context,
	pool *pgxpool.Pool, content models.Content, source string,
	c echo.Context,
) error {
	var dataRaw json.RawMessage
	err := pool.QueryRow(ctx, `
		SELECT data_raw FROM content_metadata WHERE uri = $1 AND library_id = $2
	`, content.URI, content.LibraryID).Scan(&dataRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return okResponse(c)
	}
	if err != nil {
		return err
	}

	rawMap := parseDataRaw(dataRaw)
	delete(rawMap, source)

	now := time.Now().UTC()
	mergedJSON, _ := json.Marshal(contentmeta.MergeRawLayers(rawMap))
	updatedRaw, _ := json.Marshal(rawMap)

	_, err = pool.Exec(ctx, `
		UPDATE content_metadata SET data = $1, data_raw = $2, updated_at = $3
		WHERE uri = $4 AND library_id = $5
	`, mergedJSON, updatedRaw, now, content.URI, content.LibraryID)
	if err != nil {
		return err
	}

	return okResponse(c)
}

func parseDataRaw(dataRaw json.RawMessage) map[string]json.RawMessage {
	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(dataRaw, &rawMap)
	if rawMap == nil {
		rawMap = map[string]json.RawMessage{}
	}
	return rawMap
}
