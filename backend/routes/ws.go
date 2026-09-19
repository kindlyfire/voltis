package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"voltis/lib/tasks"
	"voltis/scanner"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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

func (h *WebSocketHub) serve(conn socket, user string, admin bool, gen uint64) {
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

func (h *WebSocketHub) TaskUpdate(s tasks.Snapshot) {
	h.broadcast(toAdmins, map[string]any{
		"type":     "task_update",
		"task":     s,
		"progress": s.Progress,
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

func (h *WebSocketHub) BroadcastScanQueue(libraryIDs []string) {
	h.broadcast(toAdmins, map[string]any{
		"type":        "scan_queue_update",
		"library_ids": libraryIDs,
	})
}

func wsHandler(pool *pgxpool.Pool, hub *WebSocketHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		gen := hub.dropGen()
		user, err := resolveUser(c, pool)
		if err != nil || user == nil {
			return c.NoContent(http.StatusUnauthorized)
		}

		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}

		hub.serve(ws, user.ID, slices.Contains(user.Permissions, "ADMIN"), gen)
		return nil
	}
}
