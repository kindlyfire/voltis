package routes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"voltis/db"
	"voltis/lib/tasks"
	"voltis/linking"
	"voltis/scanner"
	"voltis/settings"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func newUpgrader(st *settings.Store) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return originAllowed(r, st) },
	}
}

func originAllowed(r *http.Request, st *settings.Store) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	want := authority(u.Scheme, u.Host)
	if want == authority(u.Scheme, r.Host) || isDevOrigin(origin) {
		return true
	}
	public, err := url.Parse(st.String(settings.AppPublicURL))
	return err == nil && public.Host != "" && want == authority(public.Scheme, public.Host)
}

func authority(scheme, host string) string {
	scheme, host = strings.ToLower(scheme), strings.ToLower(host)
	if strings.HasSuffix(host, ":80") && scheme == "http" || strings.HasSuffix(host, ":443") && scheme == "https" {
		host = host[:strings.LastIndex(host, ":")]
	}
	return host
}

var (
	queueDepth       = 32
	writeWait        = 5 * time.Second
	pingPeriod       = 20 * time.Second
	pongWait         = 60 * time.Second
	readLimit  int64 = 1024
)

type socket interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	SetReadLimit(int64)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	SetPongHandler(func(string) error)
	Close() error
}

type WebSocketHub struct {
	mu    sync.Mutex
	conns map[*userConn]struct{}
	seq   uint64
	gens  map[string]uint64
}

type userConn struct {
	conn  socket
	user  string
	admin bool
	out   chan []byte
	done  chan struct{}
	once  sync.Once
}

func NewHub() *WebSocketHub {
	return &WebSocketHub{conns: make(map[*userConn]struct{}), gens: make(map[string]uint64)}
}

func (h *WebSocketHub) dropGen() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

func (c *userConn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// still runs once the connection is visible to Drop, closing the gap between
// the caller's check and registration.
func (h *WebSocketHub) serve(conn socket, user string, admin bool, gen uint64, still func() bool) {
	c := &userConn{conn: conn, user: user, admin: admin, out: make(chan []byte, queueDepth), done: make(chan struct{})}

	h.mu.Lock()
	if h.gens[user] > gen {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, c)
		h.mu.Unlock()
	}()

	if still != nil && !still() {
		_ = conn.Close()
		return
	}

	written := make(chan struct{})
	go func() {
		defer close(written)
		defer c.close()
		c.writeLoop()
	}()
	defer func() {
		c.close()
		<-written
	}()

	c.readLoop()
}

func (c *userConn) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		var kind int
		var data []byte
		select {
		case <-c.done:
			return
		case data = <-c.out:
			kind = websocket.TextMessage
		case <-ticker.C:
			kind = websocket.PingMessage
		}
		if c.conn.SetWriteDeadline(time.Now().Add(writeWait)) != nil {
			return
		}
		if c.conn.WriteMessage(kind, data) != nil {
			return
		}
	}
}

func (c *userConn) readLoop() {
	c.conn.SetReadLimit(readLimit)
	if c.conn.SetReadDeadline(time.Now().Add(pongWait)) != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *WebSocketHub) broadcast(to func(*userConn) bool, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("[ws] failed to marshal event", "err", err)
		return
	}

	var slow []*userConn
	h.mu.Lock()
	for c := range h.conns {
		if !to(c) {
			continue
		}
		select {
		case c.out <- data:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.Unlock()

	for _, c := range slow {
		slog.Warn("[ws] disconnecting slow client", "user", c.user)
		c.close()
	}
}

func toAdmins(c *userConn) bool { return c.admin }

func toEveryone(*userConn) bool { return true }

func (h *WebSocketHub) Drop(user string) {
	var drop []*userConn
	h.mu.Lock()
	h.seq++
	h.gens[user] = h.seq
	for c := range h.conns {
		if c.user == user {
			drop = append(drop, c)
		}
	}
	h.mu.Unlock()

	for _, c := range drop {
		c.close()
	}
}

const liveSession = "expires_at > NOW() AND (absolute_expires_at IS NULL OR absolute_expires_at > NOW())"

func hasLiveSession(ctx context.Context, q db.Querier, userID string) bool {
	live, err := db.SelectScalar[bool](ctx, q,
		"SELECT EXISTS (SELECT 1 FROM sessions WHERE user_id = $1 AND "+liveSession+")", userID)
	return err == nil && live
}

func (h *WebSocketHub) DropSessionless(ctx context.Context, q db.Querier) {
	h.mu.Lock()
	var users []string
	for c := range h.conns {
		if !slices.Contains(users, c.user) {
			users = append(users, c.user)
		}
	}
	h.mu.Unlock()
	if len(users) == 0 {
		return
	}

	live, err := db.SelectScalars[string](ctx, q,
		"SELECT DISTINCT user_id FROM sessions WHERE user_id = ANY($1) AND "+liveSession, users)
	if err != nil {
		// Fail closed: nothing retries this scan, and a needless drop only
		// costs a reconnect.
		slog.Error("[ws] dropping unverifiable connections", "err", err)
		live = nil
	}
	for _, user := range users {
		if !slices.Contains(live, user) {
			h.Drop(user)
		}
	}
}

func (h *WebSocketHub) TaskUpdate(s tasks.Snapshot) {
	h.broadcast(toAdmins, map[string]any{
		"type": "task_update",
		"task": s,
	})
}

func (h *WebSocketHub) CatalogChanged(ev scanner.CatalogChanged) {
	h.broadcast(toEveryone, map[string]any{
		"type":       "catalog_changed",
		"library_id": ev.LibraryID,
		"task_id":    ev.TaskID,
		"commit_seq": ev.CommitSeq,
	})
}

// LibraryChanged tells clients a library's catalog changed outside a scan.
func (h *WebSocketHub) LibraryChanged(libraryID string) {
	h.CatalogChanged(scanner.CatalogChanged{LibraryID: libraryID})
}

// MetadataStatus tells admins what matching and refreshing with metadata providers is doing.
func (h *WebSocketHub) MetadataStatus(st linking.WorkerStatus) {
	h.broadcast(toAdmins, map[string]any{"type": "metadata_status", "status": st})
}

func wsHandler(r *resolver) echo.HandlerFunc {
	upgrader := newUpgrader(r.st)
	return func(c echo.Context) error {
		gen := r.hub.dropGen()
		user, err := r.resolve(c)
		if err != nil || user == nil {
			return c.NoContent(http.StatusUnauthorized)
		}

		// Upgrade drops the response headers, so carry any new session cookie over.
		var header http.Header
		if cookies := c.Response().Header().Values("Set-Cookie"); len(cookies) > 0 {
			header = http.Header{"Set-Cookie": cookies}
		}

		ws, err := upgrader.Upgrade(c.Response(), c.Request(), header)
		if err != nil {
			return err
		}

		r.hub.serve(ws, user.ID, slices.Contains(user.Permissions, "ADMIN"), gen, func() bool {
			return hasLiveSession(reqCtx(c), r.pool, user.ID)
		})
		return nil
	}
}
