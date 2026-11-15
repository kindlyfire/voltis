package reading

import (
	"context"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"
	"voltis/scrobble"

	"github.com/jackc/pgx/v5"
)

// revisionFor is the reader's revision, else fallback, else a server revision.
func revisionFor(writer string, seq int64, fallback models.Revision) models.Revision {
	switch {
	case writer != "":
		return models.ReaderRevision(writer, seq)
	case fallback != "":
		return fallback
	}
	return models.ServerRevision()
}

// Apply runs a reading event or command through the transition service, under the user's
// library locks, and returns the item's state with its series as of the write. It queues the
// touched series on tx (the context carries scrobble.WithTouches); the caller notifies after
// commit.
func Apply(ctx context.Context, tx pgx.Tx, userID, contentID string, req Request, now time.Time,
) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if err := db.LockContentUserData(ctx, tx, userID, contentID); err != nil {
		return Result{}, err
	}
	res, err := apply(ctx, tx, userID, contentID, req, now)
	if err != nil {
		return res, err
	}
	return res, scrobble.Flush(ctx, tx, now)
}

// apply is Apply under the caller's locks, without flushing.
func apply(ctx context.Context, tx pgx.Tx, userID, contentID string, req Request, now time.Time,
) (Result, error) {
	t, err := LoadTarget(ctx, tx, userID, contentID)
	if err != nil {
		return Result{}, err
	}
	switch req.Op {
	case OpPosition, OpFinish, OpRestore, OpSeriesStatus:
		if t.IsSeries() {
			return Result{}, InvalidError{string(req.Op) + " applies to items only"}
		}
	}
	rev := revisionFor(req.WriterID, req.Seq, req.Revision)
	res := Result{Outcome: OutcomeNone}
	state := t.State
	if req.Op == OpSeriesStatus {
		err = setSeriesStatus(ctx, tx, userID, t, req, rev, now, &res)
	} else if ok, conflict := checkRevision(t.Revision, req); conflict {
		current, err := envelope(ctx, tx, userID, t.ParentID, state)
		if err != nil {
			return res, err
		}
		return res, ConflictError{current}
	} else if ok {
		state, err = writeReading(ctx, tx, userID, t, req, rev, now, &res)
	}
	if err != nil {
		return res, err
	}
	res.Envelope, err = envelope(ctx, tx, userID, t.ParentID, state)
	if err == nil && t.IsSeries() && req.Op == OpClear {
		res.Items, err = readingItems(ctx, tx, `SELECT id FROM content WHERE parent_id = @id`,
			pgx.NamedArgs{"user_id": userID, "id": t.ID}, req.IDs)
	}
	return res, err
}

// setSeriesStatus sets the status of the item's series for a reader, last writer wins: the series'
// revision isn't checked, but a request delivered again while its write is the last does nothing.
func setSeriesStatus(ctx context.Context, tx pgx.Tx, userID string, t Target, req Request,
	rev models.Revision, now time.Time, res *Result) error {
	if t.ParentID == nil {
		return InvalidError{"Not in a series"}
	}
	series, err := LoadTarget(ctx, tx, userID, *t.ParentID)
	if err != nil {
		return err
	}
	if delivered(series.Revision, req.WriterID, req.Seq) {
		return nil
	}
	res.Outcome = OutcomeSeriesStatus
	if req.Series != nil {
		return revertSeries(ctx, tx, userID, *t.ParentID, *req.Series, rev)
	}
	_, err = apply(ctx, tx, userID, *t.ParentID, Request{Op: OpSetStatus, Status: req.Status, Revision: rev}, now)
	return err
}

// ApplySeries applies a series-wide action: completing the volumes up to one, completing the
// series (and optionally its unread volumes), or clearing the series and its volumes.
func ApplySeries(ctx context.Context, tx pgx.Tx, userID, seriesID string, req SeriesRequest, now time.Time,
) (SeriesResult, error) {
	res := SeriesResult{Items: []Item{}}
	if err := validateWriter(req.WriterID, req.Seq); err != nil {
		return res, err
	}
	rev := revisionFor(req.WriterID, req.Seq, "")
	args := pgx.NamedArgs{"user_id": userID, "ids": []string{seriesID}, "series_id": seriesID, "until_id": req.UntilID}
	if err := db.LockContentUserData(ctx, tx, userID, seriesID); err != nil {
		return res, err
	}
	t, err := LoadTarget(ctx, tx, userID, seriesID)
	if err != nil {
		return res, err
	}
	if !t.IsSeries() {
		return res, InvalidError{"Not a series"}
	}
	// A reader's request delivered already, or overtaken by its later write to the series or a
	// volume, changes nothing. Its response still covers the same scope.
	replay := false
	if req.WriterID != "" {
		// The series row's revision survives changes to its volumes, so it is among these.
		revs, err := db.SelectScalars[*string](ctx, tx, `
			SELECT utc.revision FROM content c`+db.UTCJoin+`
			WHERE c.id = @series_id OR c.parent_id = @series_id`, args)
		if err != nil {
			return res, err
		}
		replay = slices.ContainsFunc(revs, func(rev *string) bool { return delivered(rev, req.WriterID, req.Seq) })
	}
	scope, err := seriesScopeSQL(ctx, tx, req, args, replay)
	if err != nil {
		return res, err
	}
	if !replay {
		if res.Count, err = applySeriesAction(ctx, tx, userID, t, req, args, rev, now); err != nil {
			return res, err
		}
	}
	items, err := readingItems(ctx, tx, scope, args, req.IDs)
	if err != nil {
		return res, err
	}
	for _, it := range items {
		if it.ID == seriesID {
			res.Series = it
		} else {
			res.Items = append(res.Items, it)
		}
	}
	return res, scrobble.Flush(ctx, tx, now)
}

// rankedVolumes selects (taking @series_id) the valid volumes of a series with their position.
const rankedVolumes = `
	SELECT c.id, row_number() OVER (ORDER BY c."order" ASC NULLS LAST, c.id) AS pos
	FROM content c WHERE c.parent_id = @series_id AND c.valid`

// seriesScopeSQL selects (taking @series_id and @until_id) the ids of the content a series action
// covers: the series, plus the volumes it can change. A superset of what it did, so that a
// delivered-again request reports the same scope.
func seriesScopeSQL(ctx context.Context, tx pgx.Tx, req SeriesRequest, args pgx.NamedArgs,
	replay bool) (string, error) {
	self := `SELECT @series_id::text`
	switch req.Action {
	case ActionClear:
		return self + ` UNION SELECT id FROM content WHERE parent_id = @series_id`, nil
	case ActionMarkSeriesCompleted:
		if req.IncludeUnread {
			return self + ` UNION SELECT id FROM content WHERE parent_id = @series_id AND valid`, nil
		}
		return self, nil
	}
	// A delivered-again request needs no valid cutoff: it reports, whatever the cutoff is now.
	if !replay {
		exists, err := db.SelectScalar[bool](ctx, tx,
			"SELECT EXISTS (SELECT 1 FROM content WHERE id = @until_id AND parent_id = @series_id AND valid)", args)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", ErrVolumeNotFound
		}
	}
	return self + ` UNION SELECT k.id FROM (` + rankedVolumes + `) k
		WHERE k.pos <= (SELECT pos FROM (` + rankedVolumes + `) u WHERE u.id = @until_id)`, nil
}

// applySeriesAction writes a series action and returns how many rows it changed.
func applySeriesAction(ctx context.Context, tx pgx.Tx, userID string, t Target, req SeriesRequest,
	args pgx.NamedArgs, rev models.Revision, now time.Time) (int, error) {
	seriesID := t.ID
	var rows []completionRow
	var err error
	switch req.Action {
	case ActionClear:
		count, err := db.SelectScalar[int](ctx, tx,
			"SELECT COUNT(*) FROM content WHERE id = ANY(@ids) OR parent_id = ANY(@ids)", args)
		if err != nil {
			return 0, err
		}
		return count, clearContent(ctx, tx, userID, []string{seriesID}, rev)
	case ActionMarkThrough:
		rows, err = db.Select[completionRow](ctx, tx, `
			WITH k AS (`+rankedVolumes+`)
			SELECT u.* FROM (`+unfinishedChildRows+`) u JOIN k ON k.id = u.id
			WHERE k.pos <= (SELECT pos FROM k WHERE id = @until_id)
		`, args)
		if err != nil {
			return 0, err
		}
	case ActionMarkSeriesCompleted:
		if req.IncludeUnread {
			if rows, err = db.Select[completionRow](ctx, tx, unfinishedChildRows, args); err != nil {
				return 0, err
			}
		}
		rows = append(rows, completionRow{ID: seriesID, Type: t.Type})
	}
	count := len(rows)
	if err := markCompleted(ctx, tx, userID, rows, rev, now); err != nil {
		return 0, err
	}
	if req.Action == ActionMarkThrough && count > 0 {
		if _, err = startSeries(ctx, tx, userID, seriesID, rev, now, false); err != nil {
			return 0, err
		}
	}
	if req.Action == ActionMarkThrough && req.WriterID != "" {
		// The series row carries the reader's revision even when its status didn't change, so a retry
		// is recognized as delivered whatever has happened to the volumes since.
		_, err = tx.Exec(ctx, `
			INSERT INTO user_to_content (id, user_id, library_id, uri, revision)
			SELECT @utc_id, @user_id, library_id, uri, @rev FROM content WHERE id = @series_id
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET revision = EXCLUDED.revision
		`, pgx.NamedArgs{"utc_id": models.MakeUserToContentID(), "user_id": userID, "rev": rev,
			"series_id": seriesID})
	}
	return count, err
}

// Bulk sets or clears the user's status on the given content, as the reading commands do.
// Completing also ends the items, and with IncludeChildren the unfinished volumes of selected
// series; Reset clears the content and the children of selected series. It returns how many
// content it covered.
func Bulk(ctx context.Context, tx pgx.Tx, userID string, req BulkRequest, now time.Time) (int, error) {
	req.IDs = fp.Dedup(req.IDs)
	if err := db.LockContentUserData(ctx, tx, userID, req.IDs...); err != nil {
		return 0, err
	}
	count, err := bulk(ctx, tx, userID, req, now)
	if err != nil {
		return 0, err
	}
	return count, scrobble.Flush(ctx, tx, now)
}

func bulk(ctx context.Context, tx pgx.Tx, userID string, req BulkRequest, now time.Time) (int, error) {
	ids, rev := req.IDs, models.ServerRevision()
	args := pgx.NamedArgs{"user_id": userID, "ids": ids, "status": req.Status, "now": now, "rev": rev}
	rows, err := db.Select[completionRow](ctx, tx,
		"SELECT "+completionColumns+" FROM content c WHERE c.id = ANY(@ids)", args)
	if err != nil {
		return 0, err
	}
	count := len(rows)

	switch {
	case req.Reset:
		return count, clearContent(ctx, tx, userID, ids, rev)
	case isCompleted(req.Status):
		if req.IncludeChildren {
			children, err := db.Select[completionRow](ctx, tx, unfinishedChildRows, args)
			if err != nil {
				return 0, err
			}
			// A selected child is also a child of a selected series.
			rows = fp.Dedup(append(rows, children...))
		}
		if err := markCompleted(ctx, tx, userID, rows, rev, now); err != nil {
			return 0, err
		}
	default:
		// Completion changes on items that were completed, and the status on series.
		args["series_types"] = metadata.SeriesTypes
		changed, err := db.SelectScalars[string](ctx, tx, `
			SELECT c.id FROM content c`+db.UTCJoin+`
			WHERE c.id = ANY(@ids) AND CASE WHEN c.type = ANY(@series_types)
				THEN utc.status IS DISTINCT FROM @status ELSE utc.status = 'completed' END
		`, args)
		if err != nil {
			return 0, err
		}
		if err := scrobble.Touch(ctx, userID, changed...); err != nil {
			return 0, err
		}
		args["utc_ids"] = fp.Map(ids, func(string) string { return models.MakeUserToContentID() })
		_, err = tx.Exec(ctx, `
			INSERT INTO user_to_content AS utc (id, user_id, library_id, uri, status, status_updated_at, revision)
			SELECT r.id, @user_id, c.library_id, c.uri, @status, @now, @rev
			FROM unnest(@utc_ids::text[], @ids::text[]) r(id, cid)
			JOIN content c ON c.id = r.cid
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status,
				status_updated_at = CASE WHEN utc.status IS DISTINCT FROM EXCLUDED.status
					THEN EXCLUDED.status_updated_at ELSE utc.status_updated_at END,
				revision = EXCLUDED.revision
		`, args)
		if err != nil {
			return 0, err
		}
		if req.Status == nil || *req.Status != models.StatusReading {
			return count, nil
		}
	}
	// Reading or completed volumes start their series, after any series status set above.
	parents, err := db.SelectScalars[string](ctx, tx,
		"SELECT DISTINCT parent_id FROM content WHERE id = ANY(@ids) AND parent_id IS NOT NULL", args)
	if err != nil {
		return 0, err
	}
	for _, p := range parents {
		if _, err := startSeries(ctx, tx, userID, p, rev, now, false); err != nil {
			return 0, err
		}
	}
	return count, nil
}
