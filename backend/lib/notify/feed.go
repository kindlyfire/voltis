package notify

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"
)

// Item is a feed entry as the client gets it.
type Item struct {
	ID        string       `db:"id" json:"id"`
	Kind      string       `db:"kind" json:"kind"`
	CreatedAt time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt time.Time    `db:"updated_at" json:"updated_at"`
	AlertedAt *time.Time   `db:"alerted_at" json:"alerted_at"`
	Unread    bool         `db:"unread" json:"unread"`
	TaskID    *string      `db:"task_id" json:"task_id"`
	Content   models.JSONB `db:"content" json:"content"`
	Data      models.JSONB `db:"data" json:"data"`
}

type Page struct {
	Active     []Item  `json:"active"`
	NeedsInput []Item  `json:"needs_input"`
	Data       []Item  `json:"data"`
	Next       *string `json:"next"`
}

// Cursor is the (updated_at, id) of the last history row of a page.
type Cursor struct {
	At time.Time
	ID string
}

func (c Cursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.At.Format(time.RFC3339Nano) + "|" + c.ID))
}

var ErrBadCursor = errors.New("invalid cursor")

func ParseCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return nil, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, ErrBadCursor
	}
	return &Cursor{At: t, ID: id}, nil
}

// $1 user, $2 admin; the scan overlay shows the library's live name and whether it still exists.
const feedSelect = `
	SELECT n.id, n.kind, n.created_at, n.updated_at, n.alerted_at, (` + unread + `) AS unread, n.task_id,
	       CASE WHEN c.id IS NOT NULL THEN jsonb_build_object('id', c.id, 'type', c.type,
	            'title', COALESCE(NULLIF(c.data->>'title', ''), c.uri_part)) END AS content,
	       CASE WHEN n.kind = 'scan' THEN n.data || jsonb_build_object(
	            'library_name', COALESCE(l.name, n.data->>'library_name'), 'library_deleted', l.id IS NULL)
	            ELSE n.data END AS data
	FROM notifications n
	LEFT JOIN content c ON c.id = n.content_id
	LEFT JOIN tasks t ON t.id = n.task_id
	LEFT JOIN libraries l ON l.id = n.data->>'library_id'
	WHERE n.user_id = $1 AND (n.kind IN ('new_content', 'scrobble_auth') OR $2::bool) AND `

const activeScan = `n.kind = 'scan' AND COALESCE(t.status IN (0, 1), false)`

// needsInput items stay on top of the feed while unresolved.
const needsInput = `n.kind IN ('metadata_review', 'scrobble_auth')`

// Feed reads a user's items. Admin-only kinds are hidden from non-admins here, so a demotion needs
// no cleanup. Active and needs-input items come with the first page only, and never in history.
func Feed(ctx context.Context, q db.Querier, userID string, admin bool, cur *Cursor, limit int) (Page, error) {
	p := Page{Active: []Item{}, NeedsInput: []Item{}}
	var err error
	if cur == nil {
		if p.Active, err = db.Select[Item](ctx, q, feedSelect+activeScan+" ORDER BY n.created_at, n.id", userID, admin); err != nil {
			return p, err
		}
		if p.NeedsInput, err = db.Select[Item](ctx, q,
			feedSelect+needsInput+" ORDER BY n.updated_at DESC, n.id DESC", userID, admin); err != nil {
			return p, err
		}
	}
	history := feedSelect + "NOT (" + needsInput + ") AND NOT (" + activeScan + ")"
	args := []any{userID, admin, limit + 1}
	if cur != nil {
		history += " AND (n.updated_at, n.id) < ($4, $5)"
		args = append(args, cur.At, cur.ID)
	}
	if p.Data, err = db.Select[Item](ctx, q, history+" ORDER BY n.updated_at DESC, n.id DESC LIMIT $3", args...); err != nil {
		return p, err
	}
	if len(p.Data) > limit {
		p.Data = p.Data[:limit]
		next := Cursor{p.Data[limit-1].UpdatedAt, p.Data[limit-1].ID}.String()
		p.Next = &next
	}
	return p, nil
}

// ForTask reads the user's items of one task.
func ForTask(ctx context.Context, q db.Querier, userID string, admin bool, taskID string) ([]Item, error) {
	return db.Select[Item](ctx, q, feedSelect+"n.task_id = $3 ORDER BY n.updated_at DESC, n.id DESC", userID, admin, taskID)
}

// Unread counts each user's unread items, 0 included, by their own permissions.
func Unread(ctx context.Context, q db.Querier, userIDs []string) (map[string]int, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, count(n.id) FILTER (WHERE `+unread+` AND (n.kind IN ('new_content', 'scrobble_auth') OR 'ADMIN' = ANY(u.permissions)))
		FROM users u LEFT JOIN notifications n ON n.user_id = u.id
		WHERE u.id = ANY($1::text[]) GROUP BY u.id
	`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int, len(userIDs))
	for _, id := range userIDs {
		out[id] = 0
	}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ClearScrobbleAuth deletes the user's rejected-token item, locking in Clear's order. It reports
// whether there was one.
func ClearScrobbleAuth(ctx context.Context, q db.Querier, userID string) (bool, error) {
	tag, err := q.Exec(ctx, `
		WITH l AS (SELECT id FROM notifications WHERE user_id = $1 AND group_key = 'scrobble_auth'
		            ORDER BY group_key NULLS FIRST, id FOR UPDATE)
		DELETE FROM notifications n USING l WHERE n.id = l.id
	`, userID)
	return tag.RowsAffected() > 0, err
}
