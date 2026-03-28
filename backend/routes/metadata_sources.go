package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"voltis/lib/sources"
	"voltis/models/metaraw"

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

	err = editMetadataRaw(ctx, r.pool, content.URI, content.LibraryID, func(mr *metaraw.MetadataRaw) bool {
		mr.MangaBaka = &metaraw.RawContainer[sources.Series]{Raw: *series}
		return true
	})
	if err != nil {
		return err
	}
	return okResponse(c)
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

	err = editMetadataRaw(ctx, r.pool, content.URI, content.LibraryID, func(mr *metaraw.MetadataRaw) bool {
		switch req.Source {
		case "mangabaka":
			mr.MangaBaka = nil
		}
		return true
	})
	if err != nil {
		return err
	}
	return okResponse(c)
}
