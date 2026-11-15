package reading

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"
	"voltis/scrobble"

	"github.com/jackc/pgx/v5"
)

const readingStateColumns = `utc.revision, utc.status, utc.status_updated_at,
	COALESCE(utc.progress, '{}') AS progress, utc.progress_updated_at, utc.last_read_at,
	COALESCE(utc.reading_seq, 0) AS reading_seq`

// pagesExpr is a comic's current page count, from the scan or its page list.
const pagesExpr = `COALESCE(c.page_count, CASE WHEN jsonb_typeof(c.file_data->'pages') = 'array'
	THEN jsonb_array_length(c.file_data->'pages') END, 0)`

// Target is the content a write applies to, with the user's state on it.
type Target struct {
	ID        string  `db:"id"`
	Type      string  `db:"type"`
	ParentID  *string `db:"parent_id"`
	LibraryID string  `db:"library_id"`
	URI       string  `db:"uri"`
	Pages     int     `db:"pages"`
	State
}

func (t Target) IsSeries() bool { return slices.Contains(metadata.SeriesTypes, t.Type) }

// LoadTarget reads the content and the user's state on it, or returns ErrNotFound.
func LoadTarget(ctx context.Context, q db.Querier, userID, contentID string) (Target, error) {
	t, err := db.SelectOne[Target](ctx, q, `
		SELECT c.id, c.type, c.parent_id, c.library_id, c.uri, `+pagesExpr+` AS pages, `+readingStateColumns+`
		FROM content c`+db.UTCJoin+`
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": contentID})
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return t, err
}

// Get reads the content's state with its series'.
func Get(ctx context.Context, q db.Querier, userID, contentID string) (Envelope, error) {
	t, err := LoadTarget(ctx, q, userID, contentID)
	if err != nil {
		return Envelope{}, err
	}
	return envelope(ctx, q, userID, t.ParentID, t.State)
}

// readingItems reads the current state of the content that idsSQL selects, in the caller's
// transaction, plus the extra ids that still exist. args holds idsSQL's parameters and @user_id.
// Content without a row reads as the empty state at seq 0.
func readingItems(ctx context.Context, q db.Querier, idsSQL string, args pgx.NamedArgs, extra []string,
) ([]Item, error) {
	args["extra_ids"] = extra
	items, err := db.Select[Item](ctx, q, `
		SELECT c.id, `+readingStateColumns+` FROM content c`+db.UTCJoin+`
		WHERE c.id IN (`+idsSQL+`) OR c.id = ANY(@extra_ids) ORDER BY c.id
	`, args)
	return items, err
}

// envelope reads the item's series with its state, in the same transaction.
func envelope(ctx context.Context, q db.Querier, userID string, parentID *string, state State,
) (Envelope, error) {
	env := Envelope{State: state, Writer: revisionWriter(state.Revision)}
	if parentID == nil {
		return env, nil
	}
	var err error
	env.Series, err = seriesInfo(ctx, q, userID, *parentID)
	return env, err
}

func seriesInfo(ctx context.Context, q db.Querier, userID, seriesID string) (*SeriesInfo, error) {
	info, err := db.SelectOne[SeriesInfo](ctx, q, `
		SELECT c.id, `+readingStateColumns+`, cc.children_count, cc.completed_children_count,
			cc.dropped_children_count,
			cc.children_count > 0 AND cc.completed_children_count + cc.dropped_children_count = cc.children_count
				AS caught_up
		FROM content c`+db.UTCJoin+db.ChildCountsJoin+`
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": seriesID})
	info.Writer = revisionWriter(info.Revision)
	return &info, err
}

func writeReading(ctx context.Context, tx pgx.Tx, userID string, t Target, req Request,
	rev models.Revision, now time.Time, res *Result) (State, error) {
	if req.Op == OpPosition {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(req.Progress, &m)
		delete(m, "at_end") // only a finish ends the item
		req.Progress, _ = json.Marshal(m)
	}
	next, outcome, previous, write := transition(t.snapshot(), req, endProgress(t.Type, t.Pages, req.Progress), now)
	if !write {
		return t.State, nil
	}
	res.Outcome, res.Previous = outcome, previous
	if outcome == OutcomeCleared {
		if err := clearContent(ctx, tx, userID, []string{t.ID}, rev); err != nil {
			return State{}, err
		}
		return db.SelectOne[State](ctx, tx, `SELECT `+readingStateColumns+` FROM content c`+db.UTCJoin+`
			WHERE c.id = @id`, pgx.NamedArgs{"user_id": userID, "id": t.ID})
	}

	statusAt := t.StatusUpdatedAt
	if !fp.PtrEq(next.Status, t.Status) {
		statusAt = &now
	}
	progressAt := t.ProgressUpdatedAt
	if outcome != OutcomeStatusSet {
		progressAt = &now
		if string(next.Progress) == "{}" {
			progressAt = nil
		}
	}
	state, err := db.SelectOne[State](ctx, tx, `
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
	// Only completion and a series' status reach MangaBaka.
	if !fp.PtrEq(next.Status, t.Status) && (t.IsSeries() || isCompleted(t.Status) != isCompleted(next.Status)) {
		if err := scrobble.Touch(ctx, userID, t.ID); err != nil {
			return state, err
		}
	}
	if t.ParentID == nil {
		return state, nil
	}
	if startsSeries(req, outcome) {
		// Starting a volume, not finishing one, reopens a completed series: it got new volumes.
		reopen := outcome == OutcomeStarted || outcome == OutcomeMovedToReading
		res.SeriesPrevious, err = startSeries(ctx, tx, userID, *t.ParentID, rev, now, reopen)
	} else if outcome == OutcomeRestored && req.Series != nil {
		err = revertSeries(ctx, tx, userID, *t.ParentID, *req.Series, rev)
	}
	return state, err
}

// revertSeries undoes a start of the series, status time included, so the volumes added since it
// was completed count as new again. Only while nothing has written the series since.
func revertSeries(ctx context.Context, tx pgx.Tx, userID, seriesID string, prev SeriesPrev, rev models.Revision) error {
	tag, err := tx.Exec(ctx, `
		UPDATE user_to_content utc SET status = @status, status_updated_at = @status_at, revision = @rev
		FROM content s
		WHERE s.id = @series_id AND utc.user_id = @user_id AND utc.library_id = s.library_id
			AND utc.uri = s.uri AND utc.revision IS NOT DISTINCT FROM @expected
	`, pgx.NamedArgs{"status": prev.Status, "status_at": prev.StatusUpdatedAt, "rev": rev,
		"series_id": seriesID, "user_id": userID, "expected": prev.Revision})
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return scrobble.Touch(ctx, userID, seriesID)
}

// startSeries moves a series without a status, or planned, to reading, and with reopen a
// completed one too. It returns the series' previous status and new revision, or nil when the
// series kept its status.
func startSeries(ctx context.Context, tx pgx.Tx, userID, seriesID string, revision models.Revision, now time.Time,
	reopen bool) (*SeriesPrev, error) {
	var prev SeriesPrev
	err := tx.QueryRow(ctx, `
		SELECT utc.status, utc.status_updated_at FROM content c`+db.UTCJoin+` WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": userID, "id": seriesID}).Scan(&prev.Status, &prev.StatusUpdatedAt)
	if err != nil {
		return nil, err
	}
	switch {
	case prev.Status == nil || *prev.Status == models.StatusPlanToRead:
	case reopen && *prev.Status == models.StatusCompleted:
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
	if err != nil {
		return nil, err
	}
	prev.Revision = new(string(revision))
	return &prev, scrobble.Touch(ctx, userID, seriesID)
}

// clearContent resets the user's status, position and last read time on the content and on the
// children of any series among it. Rows are written even where none existed, so every cleared item
// carries the clear's revision and a reader's write from before it conflicts.
func clearContent(ctx context.Context, tx pgx.Tx, userID string, ids []string, revision models.Revision) error {
	args := pgx.NamedArgs{"ids": ids, "user_id": userID, "rev": revision}
	targets, err := db.SelectScalars[string](ctx, tx,
		"SELECT id FROM content WHERE id = ANY(@ids) OR parent_id = ANY(@ids)", args)
	if err != nil {
		return err
	}
	args["targets"], args["series_types"] = targets, metadata.SeriesTypes
	changed, err := db.SelectScalars[string](ctx, tx, `
		SELECT c.id FROM content c`+db.UTCJoin+`
		WHERE c.id = ANY(@targets) AND (utc.status = 'completed' OR c.type = ANY(@series_types) AND utc.status IS NOT NULL)
	`, args)
	if err != nil {
		return err
	}
	if err := scrobble.Touch(ctx, userID, changed...); err != nil {
		return err
	}
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
func markCompleted(ctx context.Context, tx pgx.Tx, userID string, rows []completionRow, revision models.Revision, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	utcIDs, ids, progress := make([]string, len(rows)), make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		utcIDs[i], ids[i] = models.MakeUserToContentID(), r.ID
		progress[i] = string(endProgress(r.Type, r.Pages, nil))
	}
	args := pgx.NamedArgs{"user_id": userID, "now": now, "utc_ids": utcIDs, "ids": ids, "progress": progress,
		"rev": revision}
	changed, err := db.SelectScalars[string](ctx, tx, `
		SELECT c.id FROM content c`+db.UTCJoin+` WHERE c.id = ANY(@ids) AND utc.status IS DISTINCT FROM 'completed'
	`, args)
	if err != nil {
		return err
	}
	if err := scrobble.Touch(ctx, userID, changed...); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
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
	`, args)
	return err
}

// unfinishedChildRows selects the valid children of the series @ids that the user hasn't
// completed or dropped, as completionRows.
const unfinishedChildRows = `SELECT ` + completionColumns + `
	FROM content c` + db.UTCJoin + `
	WHERE c.parent_id = ANY(@ids) AND c.valid
		AND (utc.status IS NULL OR utc.status NOT IN ('completed', 'dropped'))`
