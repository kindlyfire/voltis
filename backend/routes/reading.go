package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

// readingSnapshot is what Undo restores: the item's state before an event changed it.
type readingSnapshot struct {
	Status     *string         `json:"status"`
	Progress   json.RawMessage `json:"progress"`
	LastReadAt *time.Time      `json:"last_read_at"`
}

type ReadingStateDTO struct {
	Revision          *string         `json:"revision"            db:"revision"`
	Status            *string         `json:"status"              db:"status"`
	StatusUpdatedAt   *time.Time      `json:"status_updated_at"   db:"status_updated_at"`
	Progress          json.RawMessage `json:"progress"            db:"progress"`
	ProgressUpdatedAt *time.Time      `json:"progress_updated_at" db:"progress_updated_at"`
	LastReadAt        *time.Time      `json:"last_read_at"        db:"last_read_at"`
}

func (s ReadingStateDTO) snapshot() readingSnapshot {
	return readingSnapshot{Status: s.Status, Progress: s.Progress, LastReadAt: s.LastReadAt}
}

type SeriesReadingInfo struct {
	ID                     string  `json:"id"                       db:"id"`
	Status                 *string `json:"status"                   db:"status"`
	Revision               *string `json:"revision"                 db:"revision"`
	CaughtUp               bool    `json:"caught_up"                db:"caught_up"`
	ChildrenCount          int     `json:"children_count"           db:"children_count"`
	CompletedChildrenCount int     `json:"completed_children_count" db:"completed_children_count"`
	DroppedChildrenCount   int     `json:"dropped_children_count"   db:"dropped_children_count"`
}

// readingEnvelope is an item's state with its series', read together, and the reader that wrote the
// state, if any.
type readingEnvelope struct {
	State  ReadingStateDTO    `json:"state"`
	Series *SeriesReadingInfo `json:"series"`
	Writer *string            `json:"writer"`
}

// seriesPrev is a series' status before a child started it, with its revision after the start.
type seriesPrev struct {
	Revision        *string    `json:"revision"`
	Status          *string    `json:"status"`
	StatusUpdatedAt *time.Time `json:"status_updated_at"`
}

type readingRequest struct {
	Op           string           `json:"op"            validate:"required,oneof=position finish set_status mark_completed clear restore series_status"`
	BaseRevision *string          `json:"base_revision"`
	WriterID     string           `json:"writer_id"`
	Seq          int64            `json:"seq"           validate:"min=0"`
	Progress     json.RawMessage  `json:"progress"`
	Status       *string          `json:"status"        validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
	Snapshot     *readingSnapshot `json:"snapshot"`
	Series       *seriesPrev      `json:"series"`
	revision     string           // set by code: the token of a request without a writer
}

type readingResponse struct {
	readingEnvelope
	Outcome  string           `json:"outcome"`
	Previous *readingSnapshot `json:"previous"`
	// The series' status before this write started it, and its revision after.
	SeriesPrevious *seriesPrev `json:"series_previous"`
}

type errReadingConflict struct{ current readingEnvelope }

func (errReadingConflict) Error() string { return "reading state changed elsewhere" }

var writerIDPattern = regexp.MustCompile(`^t[0-9a-z]{16}$`)

func (r readingRequest) validate() error {
	if err := ValidateStruct(r); err != nil {
		return err
	}
	bad := func(msg string) error { return echo.NewHTTPError(http.StatusBadRequest, msg) }
	if r.WriterID != "" && !writerIDPattern.MatchString(r.WriterID) {
		return bad("invalid writer_id")
	}
	if (r.WriterID == "") != (r.Seq == 0) {
		return bad("writer_id and seq go together")
	}
	switch r.Op {
	case "position", "finish", "restore", "series_status":
		if r.WriterID == "" {
			return bad(r.Op + " needs writer_id and seq")
		}
	}
	if r.Op == "position" && !isJSONObject(r.Progress) {
		return bad("progress must be a JSON object")
	}
	if r.Op == "restore" && (r.Snapshot == nil ||
		r.Snapshot.Progress != nil && string(r.Snapshot.Progress) != "null" && !isJSONObject(r.Snapshot.Progress)) {
		return bad("restore needs a snapshot")
	}
	statuses := []string{"reading", "completed", "on_hold", "dropped", "plan_to_read"}
	if r.Snapshot != nil && r.Snapshot.Status != nil && !slices.Contains(statuses, *r.Snapshot.Status) {
		return bad("invalid snapshot status")
	}
	if r.Series != nil && r.Series.Status != nil && !slices.Contains(statuses, *r.Series.Status) {
		return bad("invalid series status")
	}
	if r.Series != nil && r.Op != "restore" && r.Op != "series_status" {
		return bad("series is only for restore and series_status")
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil
}

// parseRevision splits a token into its writer and, for a reader's token, its sequence number.
// Server tokens have a random suffix, so they never hold a reader sequence.
func parseRevision(token *string) (writer string, seq int64, ok bool) {
	if token == nil {
		return "", 0, false
	}
	i := strings.LastIndexByte(*token, ':')
	if i < 0 {
		return "", 0, false
	}
	seq, err := strconv.ParseInt((*token)[i+1:], 10, 64)
	return (*token)[:i], seq, err == nil
}

// revisionWriter is the reader that wrote revision, if a reader did.
func revisionWriter(revision *string) *string {
	if writer, _, ok := parseRevision(revision); ok && writerIDPattern.MatchString(writer) {
		return &writer
	}
	return nil
}

// checkRevision decides whether a reader's write applies to a row at revision cur. A request of
// the writer that wrote cur, at or below its seq, was delivered already (or overtaken): it changes
// nothing. Otherwise the request must be based on cur, the writer's own earlier writes included.
func checkRevision(cur *string, req readingRequest) (apply bool, conflict bool) {
	if req.WriterID == "" {
		return true, false
	}
	if writer, seq, ok := parseRevision(cur); ok && writer == req.WriterID && req.Seq <= seq {
		return false, false
	}
	return fp.PtrEq(cur, req.BaseRevision), !fp.PtrEq(cur, req.BaseRevision)
}

type readingTarget struct {
	ID        string  `db:"id"`
	Type      string  `db:"type"`
	ParentID  *string `db:"parent_id"`
	LibraryID string  `db:"library_id"`
	URI       string  `db:"uri"`
	Pages     int     `db:"pages"`
	ReadingStateDTO
}

func (t readingTarget) isSeries() bool { return slices.Contains(metadata.SeriesTypes, t.Type) }

const readingStateColumns = `utc.revision, utc.status, utc.status_updated_at,
	COALESCE(utc.progress, '{}') AS progress, utc.progress_updated_at, utc.last_read_at`

// pagesExpr is a comic's current page count, from the scan or its page list.
const pagesExpr = `COALESCE(c.page_count, CASE WHEN jsonb_typeof(c.file_data->'pages') = 'array'
	THEN jsonb_array_length(c.file_data->'pages') END, 0)`

func loadReadingTarget(ctx context.Context, q db.Querier, userID, contentID string) (readingTarget, error) {
	t, err := db.SelectOne[readingTarget](ctx, q, `
		SELECT c.id, c.type, c.parent_id, c.library_id, c.uri, `+pagesExpr+` AS pages, `+readingStateColumns+`
		FROM content c`+utcJoin+`
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": contentID})
	if errors.Is(err, pgx.ErrNoRows) {
		err = echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	return t, err
}

// endProgress is the progress of finished content: the last page of a comic, or the end of a book
// (at the reader's finishing locator when it supplies one). A series has no progress.
func endProgress(typ string, pages int, supplied json.RawMessage) json.RawMessage {
	var end map[string]any
	switch typ {
	case "comic":
		end = map[string]any{"current_page": max(pages-1, 0), "progress_percent": 100, "at_end": true}
	case "book":
		end = map[string]any{"progress_percent": 100, "at_end": true}
		var s struct {
			Book json.RawMessage `json:"book"`
		}
		if json.Unmarshal(supplied, &s) == nil && s.Book != nil && string(s.Book) != "null" {
			end["book"] = s.Book
		}
	default:
		return json.RawMessage("{}")
	}
	raw, _ := json.Marshal(end)
	return raw
}

type readingOp struct {
	Op       string
	Status   *string
	Progress json.RawMessage
	Snapshot *readingSnapshot
}

func isHeld(status *string) bool {
	return status != nil && (*status == "plan_to_read" || *status == "on_hold" || *status == "dropped")
}

// transition applies an event or command to an item's state. previous is the state that Undo
// restores, set only when reading overrode a deliberate status.
func transition(cur readingSnapshot, op readingOp, end json.RawMessage, now time.Time,
) (next readingSnapshot, outcome string, previous *readingSnapshot, write bool) {
	next = cur
	switch op.Op {
	case "position":
		next.Progress, next.LastReadAt = op.Progress, &now
		switch {
		case cur.Status == nil:
			next.Status, outcome = new("reading"), "started"
		case isHeld(cur.Status):
			next.Status, outcome, previous = new("reading"), "moved_to_reading", &cur
		default: // reading keeps its status, and so does a completed item being read again
			outcome = "saved"
		}
	case "finish":
		if cur.Status != nil && *cur.Status == "completed" {
			return cur, "none", nil, false
		}
		next.Status, next.Progress, next.LastReadAt, outcome = new("completed"), end, &now, "completed"
		if isHeld(cur.Status) {
			previous = &cur
		}
	case "set_status":
		if op.Status != nil && *op.Status == "completed" {
			return transition(cur, readingOp{Op: "mark_completed"}, end, now)
		}
		next.Status, outcome = op.Status, "status_set"
	case "mark_completed":
		next.Status, next.Progress, outcome = new("completed"), end, "completed"
	case "clear":
		next, outcome = readingSnapshot{Progress: json.RawMessage("{}")}, "cleared"
	case "restore":
		next, outcome = *op.Snapshot, "restored"
	}
	if next.Progress == nil || string(next.Progress) == "null" {
		next.Progress = json.RawMessage("{}")
	}
	return next, outcome, previous, true
}

// startsSeries says whether an outcome moves the item into reading or completed, which starts its
// series.
func startsSeries(op readingOp, outcome string) bool {
	switch outcome {
	case "started", "moved_to_reading", "completed":
		return true
	case "status_set":
		return op.Status != nil && *op.Status == "reading"
	}
	return false
}

// applyReading runs a reading event or command through the transition service, under the
// library locks, and returns the item's state with its series as of the write.
func applyReading(ctx context.Context, tx pgx.Tx, userID, contentID string, req readingRequest,
	now time.Time) (readingResponse, error) {
	if err := lockUserData(ctx, tx, userID, contentID); err != nil {
		return readingResponse{}, err
	}
	t, err := loadReadingTarget(ctx, tx, userID, contentID)
	if err != nil {
		return readingResponse{}, err
	}
	switch req.Op {
	case "position", "finish", "restore", "series_status":
		if t.isSeries() {
			return readingResponse{}, echo.NewHTTPError(http.StatusBadRequest, req.Op+" applies to items only")
		}
	}
	rev := req.revision
	if req.WriterID != "" {
		rev = req.WriterID + ":" + strconv.FormatInt(req.Seq, 10)
	}
	res := readingResponse{Outcome: "none"}
	state := t.ReadingStateDTO
	if req.Op == "series_status" {
		err = setSeriesStatus(ctx, tx, userID, t, req, rev, now, &res)
	} else if apply, conflict := checkRevision(t.Revision, req); conflict {
		current, err := envelope(ctx, tx, userID, t.ParentID, state)
		if err != nil {
			return res, err
		}
		return res, errReadingConflict{current}
	} else if apply {
		state, err = writeReading(ctx, tx, userID, t, req, rev, now, &res)
	}
	if err != nil {
		return res, err
	}
	res.readingEnvelope, err = envelope(ctx, tx, userID, t.ParentID, state)
	return res, err
}

// setSeriesStatus sets the status of the item's series for a reader, last writer wins: the series'
// revision isn't checked, but a request delivered again while its write is the last does nothing.
func setSeriesStatus(ctx context.Context, tx pgx.Tx, userID string, t readingTarget, req readingRequest,
	rev string, now time.Time, res *readingResponse) error {
	if t.ParentID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Not in a series")
	}
	series, err := loadReadingTarget(ctx, tx, userID, *t.ParentID)
	if err != nil {
		return err
	}
	if apply, _ := checkRevision(series.Revision, readingRequest{WriterID: req.WriterID, Seq: req.Seq,
		BaseRevision: series.Revision}); !apply {
		return nil
	}
	res.Outcome = "series_status"
	if req.Series != nil {
		return revertSeries(ctx, tx, userID, *t.ParentID, *req.Series, rev)
	}
	_, err = applyReading(ctx, tx, userID, *t.ParentID, readingRequest{Op: "set_status", Status: req.Status,
		revision: rev}, now)
	return err
}

func writeReading(ctx context.Context, tx pgx.Tx, userID string, t readingTarget, req readingRequest,
	rev string, now time.Time, res *readingResponse) (ReadingStateDTO, error) {
	progress := req.Progress
	if req.Op == "position" {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(progress, &m)
		delete(m, "at_end") // only a finish ends the item
		progress, _ = json.Marshal(m)
	}
	op := readingOp{Op: req.Op, Status: req.Status, Progress: progress, Snapshot: req.Snapshot}
	next, outcome, previous, write := transition(t.snapshot(), op, endProgress(t.Type, t.Pages, req.Progress), now)
	if !write {
		return t.ReadingStateDTO, nil
	}
	res.Outcome, res.Previous = outcome, previous
	if outcome == "cleared" {
		if err := clearContent(ctx, tx, userID, []string{t.ID}, rev); err != nil {
			return ReadingStateDTO{}, err
		}
		return db.SelectOne[ReadingStateDTO](ctx, tx, `SELECT `+readingStateColumns+` FROM content c`+utcJoin+`
			WHERE c.id = @id`, pgx.NamedArgs{"user_id": userID, "id": t.ID})
	}

	statusAt := t.StatusUpdatedAt
	if !fp.PtrEq(next.Status, t.Status) {
		statusAt = &now
	}
	progressAt := t.ProgressUpdatedAt
	if outcome != "status_set" {
		progressAt = &now
		if string(next.Progress) == "{}" {
			progressAt = nil
		}
	}
	state, err := db.SelectOne[ReadingStateDTO](ctx, tx, `
		INSERT INTO user_to_content AS utc (id, user_id, library_id, uri, status, status_updated_at, progress,
			progress_updated_at, last_read_at, revision)
		VALUES (@id, @user_id, @library_id, @uri, @status, @status_at, @progress, @progress_at, @last_read_at, @rev)
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status,
			status_updated_at = EXCLUDED.status_updated_at, progress = EXCLUDED.progress,
			progress_updated_at = EXCLUDED.progress_updated_at, last_read_at = EXCLUDED.last_read_at,
			revision = EXCLUDED.revision
		RETURNING `+readingStateColumns, pgx.NamedArgs{
		"id": models.MakeUserToContentID(), "user_id": userID, "library_id": t.LibraryID, "uri": t.URI,
		"status": next.Status, "status_at": statusAt, "progress": []byte(next.Progress),
		"progress_at": progressAt, "last_read_at": next.LastReadAt, "rev": rev,
	})
	if err != nil {
		return state, err
	}
	if t.ParentID == nil {
		return state, nil
	}
	if startsSeries(op, outcome) {
		// Starting a volume, not finishing one, reopens a completed series: it got new volumes.
		reopen := outcome == "started" || outcome == "moved_to_reading"
		res.SeriesPrevious, err = startSeries(ctx, tx, userID, *t.ParentID, rev, now, reopen)
	} else if outcome == "restored" && req.Series != nil {
		err = revertSeries(ctx, tx, userID, *t.ParentID, *req.Series, rev)
	}
	return state, err
}

// revertSeries undoes a start of the series, status time included, so the volumes added since it
// was completed count as new again. Only while nothing has written the series since.
func revertSeries(ctx context.Context, tx pgx.Tx, userID, seriesID string, prev seriesPrev, rev string) error {
	_, err := tx.Exec(ctx, `
		UPDATE user_to_content utc SET status = @status, status_updated_at = @status_at, revision = @rev
		FROM content s
		WHERE s.id = @series_id AND utc.user_id = @user_id AND utc.library_id = s.library_id
			AND utc.uri = s.uri AND utc.revision IS NOT DISTINCT FROM @expected
	`, pgx.NamedArgs{"status": prev.Status, "status_at": prev.StatusUpdatedAt, "rev": rev,
		"series_id": seriesID, "user_id": userID, "expected": prev.Revision})
	return err
}

// envelope reads the item's series with its state, in the same transaction.
func envelope(ctx context.Context, q db.Querier, userID string, parentID *string, state ReadingStateDTO,
) (readingEnvelope, error) {
	env := readingEnvelope{State: state, Writer: revisionWriter(state.Revision)}
	if parentID == nil {
		return env, nil
	}
	var err error
	env.Series, err = seriesReadingInfo(ctx, q, userID, *parentID)
	return env, err
}

// startSeries moves a series without a status, or planned, to reading, and with reopen a
// completed one too. It returns the series' previous status and new revision, or nil when the
// series kept its status.
func startSeries(ctx context.Context, tx pgx.Tx, userID, seriesID, revision string, now time.Time,
	reopen bool) (*seriesPrev, error) {
	var prev seriesPrev
	err := tx.QueryRow(ctx, `
		SELECT utc.status, utc.status_updated_at FROM content c`+utcJoin+` WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": seriesID}).Scan(&prev.Status, &prev.StatusUpdatedAt)
	if err != nil {
		return nil, err
	}
	switch {
	case prev.Status == nil || *prev.Status == "plan_to_read":
	case reopen && *prev.Status == "completed":
	default:
		return nil, nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at, revision)
		SELECT @utc_id, @user_id, library_id, uri, 'reading', @now, @rev FROM content WHERE id = @id
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = 'reading',
			status_updated_at = EXCLUDED.status_updated_at, revision = EXCLUDED.revision
	`, pgx.NamedArgs{"utc_id": models.MakeUserToContentID(), "user_id": userID, "id": seriesID, "now": now,
		"rev": revision})
	prev.Revision = &revision
	return &prev, err
}

// clearContent resets the user's status, position and last read time on the content and on the
// children of any series among it. Rows are written even where none existed, so every cleared item
// carries the clear's revision and a reader's write from before it conflicts.
func clearContent(ctx context.Context, tx pgx.Tx, userID string, ids []string, revision string) error {
	args := pgx.NamedArgs{"ids": ids, "user_id": userID, "rev": revision}
	targets, err := db.SelectScalars[string](ctx, tx,
		"SELECT id FROM content WHERE id = ANY(@ids) OR parent_id = ANY(@ids)", args)
	if err != nil {
		return err
	}
	args["targets"] = targets
	args["utc_ids"] = fp.Map(targets, func(string) string { return models.MakeUserToContentID() })
	_, err = tx.Exec(ctx, `
		INSERT INTO user_to_content (id, user_id, library_id, uri, revision)
		SELECT r.id, @user_id, c.library_id, c.uri, @rev
		FROM unnest(@utc_ids::text[], @targets::text[]) r(id, cid)
		JOIN content c ON c.id = r.cid
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = NULL, status_updated_at = NULL,
			progress = '{}', progress_updated_at = NULL, last_read_at = NULL, revision = EXCLUDED.revision
	`, args)
	return err
}

type completionRow struct {
	ID    string `db:"id"`
	Type  string `db:"type"`
	Pages int    `db:"pages"`
}

const completionColumns = `c.id, c.type, ` + pagesExpr + ` AS pages`

// markCompleted marks content completed at its end, keeping when it was last read.
func markCompleted(ctx context.Context, tx pgx.Tx, userID string, rows []completionRow, revision string, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	utcIDs, ids, progress := make([]string, len(rows)), make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		utcIDs[i], ids[i] = models.MakeUserToContentID(), r.ID
		progress[i] = string(endProgress(r.Type, r.Pages, nil))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO user_to_content AS utc (id, user_id, library_id, uri, status, status_updated_at, progress,
			progress_updated_at, revision)
		SELECT r.id, @user_id, c.library_id, c.uri, 'completed', @now, r.progress::jsonb,
			CASE WHEN r.progress <> '{}' THEN @now::timestamptz END, @rev
		FROM unnest(@utc_ids::text[], @ids::text[], @progress::text[]) r(id, cid, progress)
		JOIN content c ON c.id = r.cid
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = 'completed',
			status_updated_at = CASE WHEN utc.status IS DISTINCT FROM 'completed'
				THEN EXCLUDED.status_updated_at ELSE utc.status_updated_at END,
			progress = EXCLUDED.progress, progress_updated_at = EXCLUDED.progress_updated_at,
			revision = EXCLUDED.revision
	`, pgx.NamedArgs{"user_id": userID, "now": now, "utc_ids": utcIDs, "ids": ids, "progress": progress,
		"rev": revision})
	return err
}

// unfinishedChildRows selects the valid children of the series @ids that the user hasn't
// completed or dropped, as completionRows.
const unfinishedChildRows = `SELECT ` + completionColumns + `
	FROM content c` + utcJoin + `
	WHERE c.parent_id = ANY(@ids) AND c.valid
		AND (utc.status IS NULL OR utc.status NOT IN ('completed', 'dropped'))`

func seriesReadingInfo(ctx context.Context, q db.Querier, userID, seriesID string) (*SeriesReadingInfo, error) {
	info, err := db.SelectOne[SeriesReadingInfo](ctx, q, `
		SELECT c.id, utc.status, utc.revision, cc.children_count, cc.completed_children_count,
			cc.dropped_children_count,
			cc.children_count > 0 AND cc.completed_children_count + cc.dropped_children_count = cc.children_count
				AS caught_up
		FROM content c`+utcJoin+childCountsJoin+`
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": seriesID})
	return &info, err
}

func (cr *ContentRoutes) readingGet(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	var res readingEnvelope
	// One snapshot: the item and its series as they stood together.
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
			return err
		}
		t, err := loadReadingTarget(ctx, tx, user.ID, c.Param("content_id"))
		if err != nil {
			return err
		}
		res, err = envelope(ctx, tx, user.ID, t.ParentID, t.ReadingStateDTO)
		return err
	})
	if err != nil {
		return err
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
	req.revision = db.ServerRevision()

	ctx := reqCtx(c)
	var res readingResponse
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		res, err = applyReading(ctx, tx, user.ID, c.Param("content_id"), req, time.Now().UTC())
		return err
	})
	if conflict, ok := errors.AsType[errReadingConflict](err); ok {
		return c.JSON(http.StatusConflict, struct {
			Message string `json:"message"`
			readingEnvelope
		}{conflict.Error(), conflict.current})
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

type seriesReadingRequest struct {
	Action        string `json:"action"         validate:"required,oneof=mark_through mark_series_completed clear"`
	UntilID       string `json:"until_id"       validate:"required_if=Action mark_through"`
	IncludeUnread bool   `json:"include_unread"`
	// A reader's request: its writes carry the reader's revision.
	WriterID string `json:"writer_id"`
	Seq      int64  `json:"seq"       validate:"min=0"`
}

// seriesReading applies a series-wide action: completing the volumes up to one, completing the
// series (and optionally its unread volumes), or clearing the series and its volumes.
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

	ctx := reqCtx(c)
	seriesID := c.Param("content_id")
	if (req.WriterID == "") != (req.Seq == 0) || req.WriterID != "" && !writerIDPattern.MatchString(req.WriterID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid writer_id or seq")
	}
	rev, now := db.ServerRevision(), time.Now().UTC()
	if req.WriterID != "" {
		rev = req.WriterID + ":" + strconv.FormatInt(req.Seq, 10)
	}
	args := pgx.NamedArgs{"user_id": user.ID, "ids": []string{seriesID}, "series_id": seriesID, "until_id": req.UntilID}
	var count int
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if err := lockUserData(ctx, tx, user.ID, seriesID); err != nil {
			return err
		}
		t, err := loadReadingTarget(ctx, tx, user.ID, seriesID)
		if err != nil {
			return err
		}
		if !t.isSeries() {
			return echo.NewHTTPError(http.StatusBadRequest, "Not a series")
		}
		// A reader's request delivered already, or overtaken by its later write to the series or a
		// volume, changes nothing.
		if req.WriterID != "" {
			revs, err := db.SelectScalars[*string](ctx, tx, `
				SELECT utc.revision FROM content c`+utcJoin+`
				WHERE c.id = @series_id OR c.parent_id = @series_id`, args)
			if err != nil {
				return err
			}
			for _, rev := range revs {
				if writer, seq, ok := parseRevision(rev); ok && writer == req.WriterID && seq >= req.Seq {
					return nil
				}
			}
		}

		var rows []completionRow
		switch req.Action {
		case "clear":
			count, err = db.SelectScalar[int](ctx, tx,
				"SELECT COUNT(*) FROM content WHERE id = ANY(@ids) OR parent_id = ANY(@ids)", args)
			if err != nil {
				return err
			}
			return clearContent(ctx, tx, user.ID, []string{seriesID}, rev)
		case "mark_through":
			rows, err = db.Select[completionRow](ctx, tx, `
				WITH k AS (
					SELECT c.id, row_number() OVER (ORDER BY c."order" ASC NULLS LAST, c.id) AS pos
					FROM content c WHERE c.parent_id = @series_id AND c.valid
				)
				SELECT u.* FROM (`+unfinishedChildRows+`) u JOIN k ON k.id = u.id
				WHERE k.pos <= (SELECT pos FROM k WHERE id = @until_id)
			`, args)
			if err != nil {
				return err
			}
			if exists, err := db.SelectScalar[bool](ctx, tx,
				"SELECT EXISTS (SELECT 1 FROM content WHERE id = @until_id AND parent_id = @series_id AND valid)", args); err != nil {
				return err
			} else if !exists {
				return echo.NewHTTPError(http.StatusNotFound, "Volume not found")
			}
		case "mark_series_completed":
			if req.IncludeUnread {
				if rows, err = db.Select[completionRow](ctx, tx, unfinishedChildRows, args); err != nil {
					return err
				}
			}
			rows = append(rows, completionRow{ID: seriesID, Type: t.Type})
		}
		count = len(rows)
		if err := markCompleted(ctx, tx, user.ID, rows, rev, now); err != nil {
			return err
		}
		if req.Action == "mark_through" && count > 0 {
			_, err = startSeries(ctx, tx, user.ID, seriesID, rev, now, false)
		}
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"count": count})
}
