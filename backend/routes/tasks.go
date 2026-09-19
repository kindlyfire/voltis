package routes

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"voltis/db"
	"voltis/lib/tasks"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type TaskRoutes struct {
	pool    *pgxpool.Pool
	manager *tasks.Manager
}

func (tr *TaskRoutes) Register(g *echo.Group) {
	g.GET("", adminOnly(tr.list))
	g.GET("/snapshot", adminOnly(tr.snapshot))
	g.GET("/:id/logs", adminOnly(tr.logs))
}

type taskListQuery struct {
	Limit     *int   `query:"limit"      validate:"omitempty,min=1" default:"25"`
	Offset    int    `query:"offset"     validate:"min=0"`
	Sort      string `query:"sort"       validate:"omitempty,oneof=created_at updated_at" default:"created_at"`
	SortOrder string `query:"sort_order" validate:"omitempty,oneof=asc desc"              default:"desc"`
}

func (tr *TaskRoutes) list(c echo.Context) error {
	ctx := reqCtx(c)

	q, err := BindQuery[taskListQuery](c)
	if err != nil {
		return err
	}

	var total int
	if err := tr.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks").Scan(&total); err != nil {
		return err
	}

	query := fmt.Sprintf(
		`SELECT * FROM tasks ORDER BY %s %s LIMIT %d OFFSET %d`,
		q.Sort, q.SortOrder, *q.Limit, q.Offset,
	)
	items, err := db.Select[models.Task](ctx, tr.pool, query)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, PaginatedResponse[models.Task]{Data: items, Total: total})
}

func (tr *TaskRoutes) snapshot(c echo.Context) error {
	ctx := reqCtx(c)

	out := map[string]tasks.Snapshot{}
	union := map[string]bool{}
	for id := range strings.SplitSeq(c.QueryParam("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			union[id] = true
		}
	}
	for _, s := range tr.manager.Live() {
		out[s.ID] = s
		union[s.ID] = true
	}

	if len(union) > 0 {
		rows, err := db.Select[tasks.Snapshot](ctx, tr.pool, `
			SELECT id, name, status, input, COALESCE(output, '{}') AS output,
				created_at, updated_at,
				octet_length(COALESCE(logs, '')) AS log_len
			FROM tasks WHERE id = ANY($1)
		`, slices.Collect(maps.Keys(union)))
		if err != nil {
			return err
		}
		for _, row := range rows {
			if live, ok := out[row.ID]; ok && !row.UpdatedAt.After(live.UpdatedAt) {
				continue
			}
			out[row.ID] = row
		}
	}

	result := slices.AppendSeq([]tasks.Snapshot{}, maps.Values(out))
	slices.SortFunc(result, func(a, b tasks.Snapshot) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})

	return c.JSON(http.StatusOK, result)
}

type taskLogsResponse struct {
	Offset int    `json:"offset"`
	Text   string `json:"text"`
	Len    int    `json:"len"`
}

func (tr *TaskRoutes) logs(c echo.Context) error {
	ctx := reqCtx(c)
	id := c.Param("id")

	offset := 0
	if raw := c.QueryParam("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid offset")
		}
		offset = parsed
	}

	text, live := tr.manager.Logs(id)
	if !live {
		stored, err := db.SelectScalar[string](ctx, tr.pool,
			"SELECT COALESCE(logs, '') FROM tasks WHERE id = $1", id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "Task not found")
			}
			return err
		}
		text = stored
	}

	if offset > len(text) {
		return echo.NewHTTPError(http.StatusBadRequest, "Offset beyond end of logs")
	}

	return c.JSON(http.StatusOK, taskLogsResponse{Offset: offset, Text: text[offset:], Len: len(text)})
}
