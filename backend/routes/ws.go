package routes

import (
	"encoding/json"
	"net/http"
	"sync"

	"voltis/models"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub manages WebSocket connections and broadcasts events.
type Hub struct {
	mu    sync.RWMutex
	conns map[*userConn]struct{}
}

type userConn struct {
	user  *models.User
	conn  *websocket.Conn
	mu    sync.Mutex
	tasks map[string]*models.Task // tracked tasks for diffing
}

func NewHub() *Hub {
	return &Hub{conns: make(map[*userConn]struct{})}
}

func (h *Hub) register(uc *userConn) {
	h.mu.Lock()
	h.conns[uc] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(uc *userConn) {
	h.mu.Lock()
	delete(h.conns, uc)
	h.mu.Unlock()
}

func (h *Hub) broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for uc := range h.conns {
		uc.mu.Lock()
		err := uc.conn.WriteMessage(websocket.TextMessage, msg)
		uc.mu.Unlock()
		if err != nil {
			_ = uc.conn.Close()
		}
	}
}

// BroadcastTaskEvent sends a task_update to all connected users.
// Per-user filtering: skips users that don't own the task when user_id is set.
func (h *Hub) BroadcastTaskEvent(task *models.Task, progress json.RawMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for uc := range h.conns {
		if task.UserID != nil && *task.UserID != uc.user.ID {
			continue
		}

		uc.mu.Lock()
		old := uc.tasks[task.ID]
		copied := *task
		uc.tasks[task.ID] = &copied

		diff := taskDiff(old, task)
		msg, _ := json.Marshal(map[string]any{
			"type":     "task_update",
			"task":     diff,
			"progress": progress,
		})
		err := uc.conn.WriteMessage(websocket.TextMessage, msg)
		uc.mu.Unlock()

		if err != nil {
			_ = uc.conn.Close()
		}

		// Clean up completed/failed tasks
		if task.Status != models.TaskStatusInProgress {
			uc.mu.Lock()
			delete(uc.tasks, task.ID)
			uc.mu.Unlock()
		}
	}
}

// BroadcastScanQueue sends a scan_queue_update with queued library IDs.
func (h *Hub) BroadcastScanQueue(libraryIDs []string) {
	msg, _ := json.Marshal(map[string]any{
		"type":        "scan_queue_update",
		"library_ids": libraryIDs,
	})
	h.broadcast(msg)
}

func taskDiff(old, new *models.Task) map[string]any {
	diff := map[string]any{"id": new.ID}

	if old == nil || old.Status != new.Status {
		diff["status"] = new.Status
	}
	if old == nil || string(old.Output) != string(new.Output) {
		diff["output"] = json.RawMessage(new.Output)
	}
	if old == nil || string(old.Input) != string(new.Input) {
		diff["input"] = json.RawMessage(new.Input)
	}
	if old == nil || ptrStr(old.Logs) != ptrStr(new.Logs) {
		if old == nil || old.Logs == nil {
			diff["logs"] = new.Logs
		} else if new.Logs != nil && len(*new.Logs) >= len(*old.Logs) && (*new.Logs)[:len(*old.Logs)] == *old.Logs {
			appended := (*new.Logs)[len(*old.Logs):]
			diff["logs"] = &appended
		} else {
			diff["logs"] = new.Logs
		}
	}

	return diff
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func wsHandler(pool *pgxpool.Pool, hub *Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, err := resolveUser(c, pool)
		if err != nil || user == nil {
			return c.NoContent(http.StatusUnauthorized)
		}

		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}

		uc := &userConn{
			user:  user,
			conn:  ws,
			tasks: make(map[string]*models.Task),
		}
		hub.register(uc)
		defer func() {
			hub.unregister(uc)
			_ = ws.Close()
		}()

		// Read loop — just keep connection alive and detect disconnect
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				break
			}
		}
		return nil
	}
}
