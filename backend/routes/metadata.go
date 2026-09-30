package routes

import (
	"encoding/json"
	"errors"
	"net/http"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/linking"
	"voltis/metadata"
	"voltis/providers"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type MetadataRoutes struct {
	pool  *pgxpool.Pool
	hub   *WebSocketHub
	store *metadata.Store
	links *linking.Service
	reg   *providers.Registry
}

func (r *MetadataRoutes) Register(g *echo.Group) {
	g.GET("/config", adminOnly(r.config))
	g.GET("/content/:id", adminOnly(r.get))
	g.POST("/content/:id/overrides", adminOnly(r.setOverrides))
	g.GET("/content/:id/candidates", adminOnly(r.candidates))
	g.POST("/content/:id/link", adminOnly(r.link))
	g.POST("/content/:id/reject", adminOnly(r.reject))
	g.POST("/content/:id/ignore", adminOnly(r.ignore))
	g.POST("/content/:id/rematch", adminOnly(r.rematch))
	g.POST("/content/:id/refresh", adminOnly(r.refresh))
	g.POST("/content/:id/undo", adminOnly(r.undo))
	g.GET("/review", adminOnly(r.review))
	g.POST("/review/resolve", adminOnly(r.resolveReview))
	g.GET("/summary", adminOnly(r.summary))
	g.POST("/match", adminOnly(r.matchNow))
	g.POST("/refresh", adminOnly(r.refreshNow))
}

type providerDTO struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	ContentTypes []string `json:"content_types"`
}

func (r *MetadataRoutes) config(c echo.Context) error {
	ps := []providerDTO{}
	for _, p := range r.reg.All() {
		ps = append(ps, providerDTO{Name: p.Name(), Label: p.Label(), ContentTypes: providers.ContentTypes(p)})
	}
	return c.JSON(http.StatusOK, map[string]any{"fields": metadata.Defs(), "providers": ps})
}

func (r *MetadataRoutes) get(c echo.Context) error { return r.respond(c, nil) }

func (r *MetadataRoutes) setOverrides(c echo.Context) error {
	var req struct {
		Rev    *int64          `json:"rev"`
		Fields json.RawMessage `json:"fields"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Rev == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "rev is required")
	}
	o, err := metadata.DecodeOverrides(req.Fields)
	if err != nil {
		return metadataError(err)
	}
	ctx := reqCtx(c)
	var t metadata.Target
	err = db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if t, err = r.store.Lock(ctx, tx, c.Param("id")); err != nil {
			return err
		}
		return r.store.SetOverrides(ctx, tx, t, *req.Rev, o)
	})
	if err == nil {
		r.hub.LibraryChanged(t.LibraryID)
	}
	return r.respond(c, err)
}

func (r *MetadataRoutes) candidates(c echo.Context) error {
	cands, err := r.links.Candidates(reqCtx(c), c.Param("id"), c.QueryParam("provider"), c.QueryParam("q"))
	if err != nil {
		return metadataError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"data": cands})
}

type linkRequest struct {
	Provider    string   `json:"provider"`
	ExternalID  string   `json:"external_id"`
	ExternalIDs []string `json:"external_ids"`
	ExpectRev   *int64   `json:"expect_rev"`
}

func (r *MetadataRoutes) link(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	return r.respond(c, r.links.Link(reqCtx(c), c.Param("id"), req.Provider, req.ExternalID, req.ExpectRev))
}

func (r *MetadataRoutes) reject(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	_, err := r.links.Reject(reqCtx(c), c.Param("id"), req.Provider, req.ExternalIDs, req.ExpectRev)
	return r.respond(c, err)
}

func (r *MetadataRoutes) ignore(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	_, err := r.links.Ignore(reqCtx(c), c.Param("id"), req.Provider, req.ExpectRev)
	return r.respond(c, err)
}

func (r *MetadataRoutes) rematch(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	return r.respond(c, r.links.Rematch(reqCtx(c), c.Param("id"), req.Provider, req.ExpectRev))
}

func (r *MetadataRoutes) refresh(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	return r.respond(c, r.links.Refresh(reqCtx(c), c.Param("id"), req.Provider))
}

func (r *MetadataRoutes) undo(c echo.Context) error {
	var req linkRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.ExpectRev == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "expect_rev is required")
	}
	return r.respond(c, r.links.Undo(reqCtx(c), c.Param("id"), req.Provider, *req.ExpectRev))
}

type reviewQuery struct {
	LibraryID string `query:"library_id"`
	Tab       string `query:"tab"    default:"review"`
	Search    string `query:"q"`
	Failed    bool   `query:"failed"`
	Limit     int    `query:"limit"  default:"50" validate:"min=1,max=200"`
	Offset    int    `query:"offset" validate:"min=0"`
}

type reviewItemDTO struct {
	Content    ContentDTO       `json:"content"`
	LocalTitle string           `json:"local_title"`
	Link       linking.LinkView `json:"link"`
}

func (r *MetadataRoutes) review(c echo.Context) error {
	q, err := BindQuery[reviewQuery](c)
	if err != nil {
		return err
	}
	page, err := r.links.Review(reqCtx(c), linking.ReviewQuery(q))
	if err != nil {
		return metadataError(err)
	}
	items := fp.Map(page.Items, func(it linking.ReviewItem) reviewItemDTO {
		return reviewItemDTO{contentToDTO(it.Content, contentDTOOpts{meta: it.Data}), it.LocalTitle, it.Link}
	})
	return c.JSON(http.StatusOK, PaginatedResponse[reviewItemDTO]{Data: items, Total: page.Total})
}

func (r *MetadataRoutes) resolveReview(c echo.Context) error {
	var req struct {
		Items []linking.ReviewAction `json:"items"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.Items) > 200 { // a review page at most
		return echo.NewHTTPError(http.StatusBadRequest, "at most 200 items")
	}
	return c.JSON(http.StatusOK, map[string]any{"results": r.links.ResolveReview(reqCtx(c), req.Items)})
}

func (r *MetadataRoutes) summary(c echo.Context) error {
	s, err := r.links.Summary(reqCtx(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s)
}

func (r *MetadataRoutes) matchNow(c echo.Context) error {
	var req struct {
		LibraryIDs []string `json:"library_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := r.links.MatchNow(reqCtx(c), req.LibraryIDs, nil); err != nil {
		return err
	}
	return okResponse(c)
}

func (r *MetadataRoutes) refreshNow(c echo.Context) error {
	if err := r.links.RefreshNow(reqCtx(c)); err != nil {
		return err
	}
	return okResponse(c)
}

// respond answers with the content's metadata once the action succeeded.
func (r *MetadataRoutes) respond(c echo.Context, err error) error {
	if err != nil {
		return metadataError(err)
	}
	v, err := r.links.View(reqCtx(c), r.pool, c.Param("id"))
	if err != nil {
		return metadataError(err)
	}
	return c.JSON(http.StatusOK, v)
}

func metadataError(err error) error {
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	case errors.Is(err, linking.ErrLinked):
		return echo.NewHTTPError(http.StatusConflict, "Linked; link another entry, reject it or ignore it instead")
	case errors.Is(err, linking.ErrNoUndo):
		return echo.NewHTTPError(http.StatusConflict, "Can no longer undo; it expired or changed since")
	case errors.Is(err, metadata.ErrConflict):
		return echo.NewHTTPError(http.StatusConflict, "Changed since it was loaded; reload and try again")
	}
	if ve, ok := errors.AsType[*metadata.ValidationError](err); ok {
		return echo.NewHTTPError(http.StatusBadRequest, ve.Error())
	}
	if pe, ok := errors.AsType[*linking.ProviderError](err); ok {
		return echo.NewHTTPError(http.StatusBadGateway, pe.Error())
	}
	return err
}
