package routes

import (
	"fmt"
	"net/http"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type TaskRoutes struct {
	pool *pgxpool.Pool
}

func (tr *TaskRoutes) Register(g *echo.Group) {
	g.GET("", tr.list)
}

type taskListQuery struct {
	Limit     *int   `query:"limit"      validate:"omitempty,min=1" default:"25"`
	Offset    int    `query:"offset"     validate:"min=0"`
	Sort      string `query:"sort"       validate:"omitempty,oneof=created_at updated_at" default:"created_at"`
	SortOrder string `query:"sort_order" validate:"omitempty,oneof=asc desc"              default:"desc"`
}

func (tr *TaskRoutes) list(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

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
