package scrobble

import (
	"context"
	"strconv"

	"voltis/db"

	"github.com/jackc/pgx/v5"
)

// Compute reads the user's current values for the series, from their valid non-special items:
//   - comics: the highest finished chapter, and the highest volume whose items are all finished;
//   - books: the highest finished series index, as the volume.
//
// Gaps are ignored, and the state follows the series status.
func Compute(ctx context.Context, q db.Querier, userID string, seriesIDs []string) (map[string]Values, error) {
	rows, err := q.Query(ctx, `
		WITH k AS (
			SELECT c.parent_id, c.order_parts[1] AS vol, c.order_parts[2] AS num,
				utc.status IS NOT DISTINCT FROM 'completed' AS done
			FROM content c
			LEFT JOIN user_to_content utc ON utc.user_id = @user_id AND utc.library_id = c.library_id AND utc.uri = c.uri
			WHERE c.parent_id = ANY(@ids) AND c.valid AND NOT c.special
		), v AS (
			SELECT parent_id, vol, bool_and(done) AS all_done, bool_or(done) AS any_done
			FROM k WHERE vol IS NOT NULL GROUP BY parent_id, vol
		)
		SELECT s.id, utc.status,
			CASE WHEN s.type = 'comic_series' THEN (SELECT max(num) FILTER (WHERE done) FROM k WHERE k.parent_id = s.id) END,
			CASE WHEN s.type = 'comic_series'
				THEN (SELECT max(vol) FILTER (WHERE all_done) FROM v WHERE v.parent_id = s.id)
				ELSE (SELECT max(vol) FILTER (WHERE any_done) FROM v WHERE v.parent_id = s.id) END
		FROM content s
		LEFT JOIN user_to_content utc ON utc.user_id = @user_id AND utc.library_id = s.library_id AND utc.uri = s.uri
		WHERE s.id = ANY(@ids)
	`, pgx.NamedArgs{"user_id": userID, "ids": seriesIDs})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Values, len(seriesIDs))
	for rows.Next() {
		var id string
		var status *string
		var chapter, volume *float32
		if err := rows.Scan(&id, &status, &chapter, &volume); err != nil {
			return nil, err
		}
		out[id] = Values{Chapter: widen(chapter), Volume: widen(volume), State: MapState(status)}
	}
	return out, rows.Err()
}

// widen turns a REAL into the float64 it was written as, so 10.1 isn't sent as 10.100000381.
func widen(f *float32) *float64 {
	if f == nil {
		return nil
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(*f), 'g', -1, 32), 64)
	return &v
}
