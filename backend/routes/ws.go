package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"voltis/models"

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
}

type userConn struct {
	conn socket
	user string
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func NewHub() *WebSocketHub {
	return &WebSocketHub{conns: make(map[*userConn]struct{})}
}

func (c *userConn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (h *WebSocketHub) serve(conn socket, user string) {
	c := &userConn{conn: conn, user: user, out: make(chan []byte, queueDepth), done: make(chan struct{})}

	h.mu.Lock()
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

func (h *WebSocketHub) broadcast(user *string, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("[ws] failed to marshal event", "err", err)
		return
	}

	var slow []*userConn
	h.mu.Lock()
	for c := range h.conns {
		if user != nil && *user != c.user {
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

func (h *WebSocketHub) BroadcastTaskEvent(task *models.Task, progress json.RawMessage, logDelta *string) {
	h.broadcast(task.UserID, map[string]any{
		"type": "task_update",
		"task": map[string]any{
			"id":     task.ID,
			"status": task.Status,
			"input":  task.Input,
			"output": task.Output,
			"logs":   logDelta,
		},
		"progress": progress,
	})
}

func (h *WebSocketHub) BroadcastScanQueue(libraryIDs []string) {
	h.broadcast(nil, map[string]any{
		"type":        "scan_queue_update",
		"library_ids": libraryIDs,
	})
}

func wsHandler(pool *pgxpool.Pool, hub *WebSocketHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, err := resolveUser(c, pool)
		if err != nil || user == nil {
			return c.NoContent(http.StatusUnauthorized)
		}

		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}

		hub.serve(ws, user.ID)
		return nil
	}
}
