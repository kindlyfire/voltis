package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/reading"
	"voltis/scrobble"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

type readingRequest struct {
	Op           reading.Op            `json:"op"            validate:"required,oneof=position finish set_status mark_completed clear restore series_status"`
	BaseRevision *string               `json:"base_revision"`
	WriterID     string                `json:"writer_id"`
	Seq          int64                 `json:"seq"           validate:"min=0"`
	Progress     json.RawMessage       `json:"progress"`
	Status       *models.ReadingStatus `json:"status"        validate:"omitempty,reading_status"`
	Snapshot     *reading.Snapshot     `json:"snapshot"`
	Series       *reading.SeriesPrev   `json:"series"`
	IDs          []string              `json:"ids" validate:"max=10000"`
}

// validate checks the request's shape and the HTTP rule that readers' events carry a writer. The
// rest is the reading service's.
func (r readingRequest) validate() error {
	if err := ValidateStruct(r); err != nil {
		return err
	}
	switch r.Op {
	case reading.OpPosition, reading.OpFinish, reading.OpRestore, reading.OpSeriesStatus:
		if r.WriterID == "" && r.Seq == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, string(r.Op)+" needs writer_id and seq")
		}
	}
	return nil
}

func (r readingRequest) toReading() reading.Request {
	return reading.Request{Op: r.Op, BaseRevision: r.BaseRevision, WriterID: r.WriterID, Seq: r.Seq,
		Progress: r.Progress, Status: r.Status, Snapshot: r.Snapshot, Series: r.Series, IDs: r.IDs}
}

// readingError maps the reading service's errors to the HTTP errors they answer with.
func readingError(err error) error {
	if errors.Is(err, reading.ErrNotFound) || errors.Is(err, reading.ErrVolumeNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	if bad, ok := errors.AsType[reading.InvalidError](err); ok {
		return echo.NewHTTPError(http.StatusBadRequest, bad.Msg)
	}
	return err
}

func (cr *ContentRoutes) readingGet(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	var res reading.Envelope
	// One snapshot: the item and its series as they stood together.
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
			return err
		}
		res, err = reading.Get(ctx, tx, user.ID, c.Param("content_id"))
		return err
	})
	if err != nil {
		return readingError(err)
	}
	return c.JSON(http.StatusOK, res)
}

func (cr *ContentRoutes) readingPost(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req readingRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := req.validate(); err != nil {
		return err
	}

	ctx, touches := scrobble.WithTouches(reqCtx(c))
	var res reading.Result
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		res, err = reading.Apply(ctx, tx, user.ID, c.Param("content_id"), req.toReading(), time.Now().UTC())
		return err
	})
	if conflict, ok := errors.AsType[reading.ConflictError](err); ok {
		return c.JSON(http.StatusConflict, struct {
			Message string `json:"message"`
			reading.Envelope
		}{conflict.Error(), conflict.Current})
	}
	if err != nil {
		return readingError(err)
	}
	cr.scrobbles.Changed(touches.Users())
	return c.JSON(http.StatusOK, res)
}

type seriesReadingRequest struct {
	Action        reading.SeriesAction `json:"action"         validate:"required,oneof=mark_through mark_series_completed clear"`
	UntilID       string               `json:"until_id"       validate:"required_if=Action mark_through"`
	IncludeUnread bool                 `json:"include_unread"`
	// A reader's request: its writes carry the reader's revision.
	WriterID string   `json:"writer_id"`
	Seq      int64    `json:"seq"       validate:"min=0"`
	IDs      []string `json:"ids" validate:"max=10000"`
}

// seriesReading applies a series-wide action; see reading.ApplySeries.
func (cr *ContentRoutes) seriesReading(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	var req seriesReadingRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	ctx, touches := scrobble.WithTouches(reqCtx(c))
	var res reading.SeriesResult
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		res, err = reading.ApplySeries(ctx, tx, user.ID, c.Param("content_id"), reading.SeriesRequest(req),
			time.Now().UTC())
		return err
	})
	if err != nil {
		return readingError(err)
	}
	cr.scrobbles.Changed(touches.Users())
	return c.JSON(http.StatusOK, res)
}
