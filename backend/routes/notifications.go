package routes

import (
	"net/http"
	"slices"

	"voltis/lib/notify"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type NotificationRoutes struct {
	pool *pgxpool.Pool
	hub  *WebSocketHub
}

func (nr *NotificationRoutes) Register(g *echo.Group) {
	g.GET("", nr.feed)
	g.POST("/seen", nr.seen)
	g.DELETE("/:id", nr.dismiss)
	g.DELETE("", nr.clear)
}

type feedQuery struct {
	Limit  int    `query:"limit"  validate:"min=1,max=100" default:"30"`
	Cursor string `query:"cursor"`
	// TaskID answers with only that task's items, in data.
	TaskID string `query:"task_id"`
}

type feedResponse struct {
	notify.Page
	Unread int `json:"unread"`
}

func (nr *NotificationRoutes) feed(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	q, err := BindQuery[feedQuery](c)
	if err != nil {
		return err
	}
	var cur *notify.Cursor
	if q.Cursor != "" {
		if cur, err = notify.ParseCursor(q.Cursor); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	var res feedResponse
	// One snapshot, so the unread count and the items' flags agree.
	tx, err := nr.pool.BeginTx(reqCtx(c), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(reqCtx(c)) }()
	admin := slices.Contains(user.Permissions, "ADMIN")
	if q.TaskID != "" {
		res.Page = notify.Page{Active: []notify.Item{}, NeedsInput: []notify.Item{}}
		res.Data, err = notify.ForTask(reqCtx(c), tx, user.ID, admin, q.TaskID)
	} else {
		res.Page, err = notify.Feed(reqCtx(c), tx, user.ID, admin, cur, q.Limit)
	}
	if err != nil {
		return err
	}
	counts, err := notify.Unread(reqCtx(c), tx, []string{user.ID})
	if err != nil {
		return err
	}
	res.Unread = counts[user.ID]
	return c.JSON(http.StatusOK, res)
}

type seenRequest struct {
	Items []notify.Seen `json:"items" validate:"max=500,dive"`
}

func (nr *NotificationRoutes) seen(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req seenRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}
	if err := notify.MarkSeen(reqCtx(c), nr.pool, user.ID, req.Items); err != nil {
		return err
	}
	return nr.unreadResponse(c, user.ID)
}

func (nr *NotificationRoutes) dismiss(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	found, err := notify.Dismiss(reqCtx(c), nr.pool, user.ID, c.Param("id"))
	if err != nil {
		return err
	}
	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "Notification not found")
	}
	return nr.unreadResponse(c, user.ID)
}

func (nr *NotificationRoutes) clear(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	if err := notify.Clear(reqCtx(c), nr.pool, user.ID); err != nil {
		return err
	}
	return nr.unreadResponse(c, user.ID)
}

// unreadResponse answers with the user's count, and tells their other tabs.
func (nr *NotificationRoutes) unreadResponse(c echo.Context, userID string) error {
	counts, err := notify.Unread(reqCtx(c), nr.pool, []string{userID})
	if err != nil {
		return err
	}
	nr.hub.pushUnread(userID, counts[userID])
	return c.JSON(http.StatusOK, map[string]int{"unread": counts[userID]})
}
