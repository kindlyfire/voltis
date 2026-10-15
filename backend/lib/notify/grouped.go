package notify

import (
	"cmp"
	"context"
	"slices"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Grouped is a user's single item for a group key. ContentID and Unit may be nil.
type Grouped struct {
	UserID, Kind, GroupKey string
	ContentID              *string
	Count                  int
	Unit                   *string
}

// UpsertGrouped writes the rows in one statement. With add, an unread item keeps accumulating
// its count; otherwise (and for a seen item) the count restarts. Either way the item is alerted
// again, so an in-flight seen mark of the older version doesn't apply.
func UpsertGrouped(ctx context.Context, tx db.Querier, rows []Grouped, add bool, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	// A fixed lock order against Clear and MarkSeen.
	slices.SortFunc(rows, func(a, b Grouped) int {
		return cmp.Or(cmp.Compare(a.UserID, b.UserID), cmp.Compare(a.GroupKey, b.GroupKey))
	})
	n := len(rows)
	ids, users, kinds, keys := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	contents, units, counts := make([]*string, n), make([]*string, n), make([]int, n)
	for i, r := range rows {
		ids[i], users[i], kinds[i], keys[i] = models.MakeNotificationID(), r.UserID, r.Kind, r.GroupKey
		contents[i], units[i], counts[i] = r.ContentID, r.Unit, r.Count
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO notifications (id, user_id, kind, group_key, content_id, data, created_at, updated_at, alerted_at)
		SELECT r.id, r.user_id, r.kind, r.group_key, r.content_id,
		       jsonb_strip_nulls(jsonb_build_object('count', r.count, 'unit', r.unit)),
		       $8::timestamptz, $8::timestamptz, $8::timestamptz
		FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::int[], $7::text[])
		     AS r(id, user_id, kind, group_key, content_id, count, unit)
		ON CONFLICT (user_id, group_key) WHERE group_key IS NOT NULL DO UPDATE SET
		  data = CASE WHEN $9::bool AND notifications.alerted_at IS NOT NULL
		                   AND notifications.seen_at IS DISTINCT FROM notifications.alerted_at
		    THEN jsonb_build_object(
		           'count', (notifications.data->>'count')::int + (excluded.data->>'count')::int,
		           'unit',  CASE WHEN notifications.data->>'unit' = excluded.data->>'unit'
		                         THEN excluded.data->>'unit' ELSE 'item' END)
		    ELSE excluded.data END,
		  alerted_at = excluded.alerted_at, updated_at = excluded.updated_at
	`, ids, users, kinds, keys, contents, counts, units, now, add)
	return err
}

// MetadataReview syncs the admins' review item to count, the number of series waiting in review.
// entered is the number that entered review in the pass that just ended: only then is the item
// (re)created and alerted. Otherwise it only follows the count, quietly.
func MetadataReview(ctx context.Context, pool *pgxpool.Pool, count, entered int) (users []string, err error) {
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		switch {
		case count == 0:
			users, err = db.SelectScalars[string](ctx, tx,
				"DELETE FROM notifications WHERE kind = 'metadata_review' RETURNING user_id")
		case entered > 0:
			users, err = db.SelectScalars[string](ctx, tx,
				"SELECT id FROM users WHERE permissions @> ARRAY['ADMIN'] ORDER BY id")
			if err != nil {
				return err
			}
			rows := make([]Grouped, len(users))
			for i, u := range users {
				rows[i] = Grouped{UserID: u, Kind: models.NotificationMetadataReview, GroupKey: "metadata_review", Count: count}
			}
			err = UpsertGrouped(ctx, tx, rows, false, time.Now().UTC())
		default:
			users, err = db.SelectScalars[string](ctx, tx, `
				UPDATE notifications SET data = jsonb_set(data, '{count}', to_jsonb($1::int))
				WHERE kind = 'metadata_review' AND (data->>'count')::int <> $1::int
				RETURNING user_id
			`, count)
		}
		return err
	})
	return users, err
}
