package routes

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"voltis/models"

	"github.com/gorilla/websocket"
)

func (c *testClient) dialWS(t *testing.T) *websocket.Conn {
	t.Helper()
	u, _ := url.Parse(c.server.URL)
	header := http.Header{}
	for _, ck := range c.http.Jar.Cookies(u) {
		header.Add("Cookie", ck.Name+"="+ck.Value)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/ws", header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial ws: %v (status %d)", err, status)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func waitConns(t *testing.T, h *WebSocketHub, n int) {
	t.Helper()
	for range 500 {
		if h.count() == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub has %d conns, want %d", h.count(), n)
}

func (c *testClient) toUser(user, id string) {
	c.hub.broadcast(func(uc *userConn) bool { return uc.user == user },
		map[string]any{"type": "ping", "id": id})
}

func (c *testClient) broadcastTaskAndQueue() {
	logs := "chunk\n"
	c.hub.BroadcastTaskEvent(&models.Task{ID: "task_1"}, nil, &logs)
	c.hub.BroadcastScanQueue([]string{"lib_1"})
}

func readMessage(t *testing.T, conn *websocket.Conn, within time.Duration) (string, error) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(within))
	_, data, err := conn.ReadMessage()
	return string(data), err
}

func expectWSMessage(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	data, err := readMessage(t, conn, 5*time.Second)
	if err != nil {
		t.Fatalf("waiting for %q: %v", want, err)
	}
	if !strings.Contains(data, want) {
		t.Fatalf("got %q, want it to contain %q", data, want)
	}
}

func expectWSClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := readMessage(t, conn, time.Until(deadline)); err != nil {
			if isTimeout(err) {
				t.Fatalf("socket stayed open: %v", err)
			}
			return
		}
	}
	t.Fatal("socket stayed open")
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	te, ok := err.(timeout)
	return ok && te.Timeout()
}

func adminAndMember(t *testing.T, perms []string) (*testClient, *testClient, string) {
	t.Helper()
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)

	user := admin.Post("/api/users/new", map[string]any{
		"username": "member", "password": "memberpass123", "permissions": perms,
	}).Assert(t, 200).JSON()

	member := admin.newSession(t)
	member.Post("/api/auth/login", map[string]any{
		"username": "member", "password": "memberpass123",
	}).Assert(t, 200)

	return admin, member, s(user["id"])
}

func TestSocketRevocationOnDemotion(t *testing.T) {
	admin, member, memberID := adminAndMember(t, []string{"ADMIN"})

	conn := member.dialWS(t)
	waitConns(t, admin.hub, 1)
	admin.broadcastTaskAndQueue()
	expectWSMessage(t, conn, `"task_update"`)
	expectWSMessage(t, conn, `"scan_queue_update"`)

	admin.Post("/api/users/"+memberID, map[string]any{
		"username": "member", "permissions": []string{},
	}).Assert(t, 200)
	expectWSClosed(t, conn)
	waitConns(t, admin.hub, 0)

	reconnected := member.dialWS(t)
	waitConns(t, admin.hub, 1)
	admin.broadcastTaskAndQueue()
	admin.toUser(memberID, "still-live")
	expectWSMessage(t, reconnected, `"still-live"`)
}

func TestSocketRevocationOnDelete(t *testing.T) {
	admin, member, memberID := adminAndMember(t, []string{})

	conn := member.dialWS(t)
	waitConns(t, admin.hub, 1)

	admin.Delete("/api/users/"+memberID).Assert(t, 200)
	expectWSClosed(t, conn)
	waitConns(t, admin.hub, 0)
}

func TestSocketRevocationOnLogout(t *testing.T) {
	admin, member, _ := adminAndMember(t, []string{})

	conn := member.dialWS(t)
	waitConns(t, admin.hub, 1)

	member.Post("/api/auth/logout", nil).Assert(t, 200)
	expectWSClosed(t, conn)
	waitConns(t, admin.hub, 0)
}

func TestSocketSurvivesRolledBackUpsert(t *testing.T) {
	admin, member, memberID := adminAndMember(t, []string{"ADMIN"})

	conn := member.dialWS(t)
	waitConns(t, admin.hub, 1)

	resp := admin.Post("/api/users/"+memberID, map[string]any{
		"username": "admin", "permissions": []string{"ADMIN"},
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected the duplicate username upsert to fail: %s", resp.Body)
	}
	for _, u := range admin.Get("/api/users").Assert(t, 200).JSONArray() {
		if s(u["id"]) == memberID && s(u["username"]) != "member" {
			t.Fatalf("the failed upsert was not rolled back: %v", u)
		}
	}

	admin.broadcastTaskAndQueue()
	expectWSMessage(t, conn, `"task_update"`)
	waitConns(t, admin.hub, 1)
}

func TestSocketRevocationOnUnchangedUpsert(t *testing.T) {
	admin, member, memberID := adminAndMember(t, []string{"ADMIN"})

	conn := member.dialWS(t)
	waitConns(t, admin.hub, 1)

	admin.Post("/api/users/"+memberID, map[string]any{
		"username": "member", "permissions": []string{"ADMIN"},
	}).Assert(t, 200)
	expectWSClosed(t, conn)
	waitConns(t, admin.hub, 0)
}
