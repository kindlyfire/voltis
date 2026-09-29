package routes

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"voltis/covers"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type OPDSRoutes struct {
	pool  *pgxpool.Pool
	st    *settings.Store
	files *FileRoutes
}

// Register mounts the feeds on g, which is /opds/:key behind opdsCache and appKeyAuth.
func (o *OPDSRoutes) Register(g *echo.Group) {
	for _, v := range []struct {
		seg    string
		render opdsRenderer
	}{{"v1.2", renderV1}, {"v2", renderV2}} {
		g.GET("/"+v.seg+"/catalog", o.feed(o.root, v.seg, v.render))
		g.GET("/"+v.seg+"/libraries/:library_id", o.feed(o.library, v.seg, v.render))
		g.GET("/"+v.seg+"/series/:content_id", o.feed(o.series, v.seg, v.render))
		g.GET("/"+v.seg+"/continue", o.feed(o.continueFeed, v.seg, v.render))
		g.GET("/"+v.seg+"/starred", o.feed(o.starred, v.seg, v.render))
		g.GET("/"+v.seg+"/recent", o.feed(o.recent, v.seg, v.render))
		g.GET("/"+v.seg+"/lists/:list_id", o.feed(o.list, v.seg, v.render))
		g.GET("/"+v.seg+"/search", o.feed(o.search, v.seg, v.render))
	}
	g.GET("/v1.2/opensearch.xml", o.openSearch)
	g.GET("/cover/:content_id", o.files.getCover)
	g.GET("/file/:content_id/:filename", o.file)
	g.GET("/pse/:content_id/:page_index", o.pse)
}

// opdsBase is the absolute origin for OPDS links. Trusting X-Forwarded-Proto (through c.Scheme)
// is harmless here: it only shapes links in a private, no-store response to the client that sent
// it.
func opdsBase(c echo.Context, st *settings.Store) string {
	if u := st.String(settings.AppPublicURL); u != "" {
		return u
	}
	return c.Scheme() + "://" + c.Request().Host
}

// opdsLinks builds the absolute, key-carrying links of one feed version.
func (o *OPDSRoutes) links(c echo.Context, v string) opdsLinks {
	return opdsLinks{Prefix: opdsBase(c, o.st) + "/opds/" + c.Get(contextKeyAppKey).(*appKeyUser).Key, V: v}
}

// opdsCache sets Cache-Control just before the headers go out, so it overrides whatever a reused
// handler set. It runs before appKeyAuth to cover 401s too. private keeps shared caches away from
// key-bearing URLs, and no-store keeps feeds fresh.
func opdsCache(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		res := c.Response()
		res.Before(func() {
			h := res.Header()
			// getCover sets no-store on a fallback that must not be kept.
			if strings.HasPrefix(c.Path(), "/opds/:key/cover/") && res.Status == http.StatusOK &&
				!strings.Contains(h.Get("Cache-Control"), "no-store") {
				h.Set("Cache-Control", "private, max-age=86400")
			} else {
				h.Set("Cache-Control", "private, no-store")
			}
		})
		return next(c)
	}
}

type opdsHandler func(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error)
type opdsRenderer func(c echo.Context, l opdsLinks, f opdsFeed) error

func (o *OPDSRoutes) feed(h opdsHandler, v string, render opdsRenderer) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, err := requireUser(c)
		if err != nil {
			return err
		}
		l := o.links(c, v)
		f, err := h(c, user, l)
		if err != nil {
			return err
		}
		return render(c, l, f)
	}
}

// file serves a content file for acquisition. The :filename segment only names the download.
func (o *OPDSRoutes) file(c echo.Context) error {
	content, err := getContent(reqCtx(c), o.pool, c.Param("content_id"))
	if err != nil {
		return err
	}
	if (content.Type != "comic" && content.Type != "book") || content.FileURI == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content has no file")
	}
	return serveContentFile(c, *content.FileURI)
}

// pse serves a comic page as the type its PSE link declares, then records it as progress.
func (o *OPDSRoutes) pse(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var idx int
	if _, err := fmt.Sscanf(c.Param("page_index"), "%d", &idx); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid page index")
	}
	ctx := reqCtx(c)
	content, names, data, mediaType, err := comicPage(ctx, o.pool, c.Param("content_id"), idx)
	if err != nil {
		return err
	}
	declared := pseMediaType(names)
	if mediaType != declared {
		if data, err = covers.EncodeJPEG(data); err != nil {
			return fmt.Errorf("transcode pse page: %w", err)
		}
	}
	if err := c.Blob(http.StatusOK, declared, data); err != nil {
		return err
	}

	// The image is sent: a client hanging up must not cancel the write, and a failure is only logged.
	served := servedPage{Pages: len(names), FileMtime: content.FileMtime}
	recorded, err := recordComicPage(context.WithoutCancel(ctx), o.pool, user.ID,
		c.Get(contextKeyAppKey).(*appKeyUser).KeyID, content.ID, idx, served)
	if err != nil {
		slog.Warn("opds pse progress", "content_id", content.ID, "err", err)
	}
	slog.Debug("opds pse page", "content_id", content.ID, "page", idx, "recorded", recorded)
	return nil
}
