package routes

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/models"

	"github.com/gorilla/websocket"
)

var errFakeClosed = errors.New("fake socket closed")

type fakeWrite struct {
	data     []byte
	deadline time.Time
	at       time.Time
}

type fakeSocket struct {
	writes        chan fakeWrite
	ready         chan struct{}
	readyOnce     sync.Once
	entered       chan struct{}
	blocking      bool
	closed        chan struct{}
	closeOnce     sync.Once
	mu            sync.Mutex
	writeDeadline time.Time
}

func newFakeSocket(blocking bool) *fakeSocket {
	return &fakeSocket{
		writes:   make(chan fakeWrite, 256),
		ready:    make(chan struct{}),
		entered:  make(chan struct{}, 1),
		blocking: blocking,
		closed:   make(chan struct{}),
	}
}

func (f *fakeSocket) ReadMessage() (int, []byte, error) {
	<-f.closed
	return 0, nil, errFakeClosed
}

func (f *fakeSocket) WriteMessage(kind int, data []byte) error {
	select {
	case <-f.closed:
		return errFakeClosed
	default:
	}
	if kind != websocket.TextMessage {
		return nil
	}
	if f.blocking {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		<-f.closed
		return errFakeClosed
	}
	f.mu.Lock()
	deadline := f.writeDeadline
	f.mu.Unlock()
	f.writes <- fakeWrite{data: data, deadline: deadline, at: time.Now()}
	return nil
}

func (f *fakeSocket) SetReadLimit(int64) {
	f.readyOnce.Do(func() { close(f.ready) })
}

func (f *fakeSocket) SetReadDeadline(time.Time) error   { return nil }
func (f *fakeSocket) SetPongHandler(func(string) error) {}

func (f *fakeSocket) SetWriteDeadline(t time.Time) error {
	f.mu.Lock()
	f.writeDeadline = t
	f.mu.Unlock()
	return nil
}

func (f *fakeSocket) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (h *WebSocketHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func serveFake(t *testing.T, h *WebSocketHub, s *fakeSocket, user string, admin bool) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serve(s, user, admin, h.dropGen())
	}()
	t.Cleanup(func() {
		_ = s.Close()
		waitFor(t, done, "serve to return")
	})
	waitFor(t, s.ready, "the connection to register")
	return done
}

func noPings(t *testing.T) {
	t.Helper()
	old := pingPeriod
	pingPeriod = time.Hour
	t.Cleanup(func() { pingPeriod = old })
}

func mustBroadcast(t *testing.T, h *WebSocketHub, id string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.BroadcastScanQueue([]string{id})
	}()
	waitFor(t, done, fmt.Sprintf("broadcast %q to return", id))
}

func expectMessage(t *testing.T, f *fakeSocket, want ...string) {
	t.Helper()
	select {
	case got := <-f.writes:
		for _, w := range want {
			if !strings.Contains(string(got.data), w) {
				t.Fatalf("got %q, want it to contain %q", got.data, w)
			}
		}
		if d := got.deadline.Sub(got.at); d <= writeWait-time.Second || d > writeWait {
			t.Fatalf("write deadline was %v away, want ~%v", d, writeWait)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for a message containing %q", want)
	}
}

func TestHubStalledClientDoesNotBlockOthers(t *testing.T) {
	noPings(t)

	h := NewHub()
	stalled := newFakeSocket(true)
	healthy := newFakeSocket(false)
	stalledDone := serveFake(t, h, stalled, "u1", true)
	serveFake(t, h, healthy, "u1", true)

	mustBroadcast(t, h, "m0")
	waitFor(t, stalled.entered, "the stalled client to block mid-write")
	expectMessage(t, healthy, `"m0"`)

	for i := 1; i <= queueDepth; i++ {
		id := fmt.Sprintf("m%d", i)
		mustBroadcast(t, h, id)
		expectMessage(t, healthy, fmt.Sprintf(`"%s"`, id))
	}

	select {
	case <-stalled.closed:
		t.Fatal("stalled client closed before its queue overflowed")
	default:
	}
	if n := h.count(); n != 2 {
		t.Fatalf("hub has %d conns, want 2", n)
	}

	mustBroadcast(t, h, "overflow")
	expectMessage(t, healthy, `"overflow"`)
	waitFor(t, stalledDone, "the stalled client to be disconnected")

	if n := h.count(); n != 1 {
		t.Fatalf("hub has %d conns after overflow, want 1", n)
	}

	mustBroadcast(t, h, "after")
	expectMessage(t, healthy, `"after"`)
}

func expectNoMessage(t *testing.T, f *fakeSocket) {
	t.Helper()
	select {
	case got := <-f.writes:
		t.Fatalf("got unexpected message %q", got.data)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTaskAudienceIsAdminOnly(t *testing.T) {
	noPings(t)

	h := NewHub()
	admin := newFakeSocket(false)
	plain := newFakeSocket(false)
	serveFake(t, h, admin, "u1", true)
	serveFake(t, h, plain, "u2", false)

	owner := "u2"
	logs := "chunk\n"
	h.BroadcastTaskEvent(&models.Task{ID: "task_1", UserID: &owner}, nil, &logs)
	h.BroadcastTaskEvent(&models.Task{ID: "task_2", UserID: nil}, nil, &logs)
	mustBroadcast(t, h, "queue")

	expectMessage(t, admin, `"task_update"`, `"task_1"`, `"chunk\n"`)
	expectMessage(t, admin, `"task_update"`, `"task_2"`)
	expectMessage(t, admin, `"scan_queue_update"`, `"queue"`)
	expectNoMessage(t, plain)

	h.broadcast(func(c *userConn) bool { return c.user == "u2" },
		map[string]any{"type": "ping", "id": "still-live"})
	expectMessage(t, plain, `"still-live"`)
}

func TestHubUserFilter(t *testing.T) {
	noPings(t)

	h := NewHub()
	a := newFakeSocket(false)
	b := newFakeSocket(false)
	serveFake(t, h, a, "u1", true)
	serveFake(t, h, b, "u2", false)

	h.broadcast(func(c *userConn) bool { return c.user == "u2" }, map[string]any{"type": "ping", "id": "only-b"})

	expectMessage(t, b, `"only-b"`)
	expectNoMessage(t, a)
}

func TestDropClosesOnlyTheNamedUser(t *testing.T) {
	noPings(t)

	h := NewHub()
	a1 := newFakeSocket(false)
	a2 := newFakeSocket(false)
	other := newFakeSocket(false)
	a1Done := serveFake(t, h, a1, "u1", true)
	a2Done := serveFake(t, h, a2, "u1", false)
	serveFake(t, h, other, "u2", true)

	h.Drop("u1")
	waitFor(t, a1Done, "u1 first socket to close")
	waitFor(t, a2Done, "u1 second socket to close")

	select {
	case <-other.closed:
		t.Fatal("u2 socket was closed")
	default:
	}
	if n := h.count(); n != 1 {
		t.Fatalf("hub has %d conns after drop, want 1", n)
	}

	mustBroadcast(t, h, "after")
	expectMessage(t, other, `"after"`)
}

func TestDropUnknownUserIsNoop(t *testing.T) {
	noPings(t)

	h := NewHub()
	a := newFakeSocket(false)
	serveFake(t, h, a, "u1", true)

	h.Drop("nobody")

	if n := h.count(); n != 1 {
		t.Fatalf("hub has %d conns after no-op drop, want 1", n)
	}
	select {
	case <-a.closed:
		t.Fatal("drop of an unknown user closed a live socket")
	default:
	}

	mustBroadcast(t, h, "after")
	expectMessage(t, a, `"after"`)
}

type lockingSocket struct {
	*fakeSocket
	h *WebSocketHub
}

func (s *lockingSocket) Close() error {
	s.h.count()
	return s.fakeSocket.Close()
}

func TestDropClosesOutsideHubLock(t *testing.T) {
	noPings(t)

	h := NewHub()
	f := newFakeSocket(false)
	served := make(chan struct{})
	go func() {
		defer close(served)
		h.serve(&lockingSocket{fakeSocket: f, h: h}, "u1", true, 0)
	}()
	waitFor(t, f.ready, "the connection to register")

	dropped := make(chan struct{})
	go func() {
		defer close(dropped)
		h.Drop("u1")
	}()
	waitFor(t, dropped, "Drop to return")
	waitFor(t, served, "serve to return")
}

func TestDropDuringUpgradeRefusesRegistration(t *testing.T) {
	noPings(t)

	h := NewHub()
	live := newFakeSocket(false)
	liveDone := serveFake(t, h, live, "u1", true)

	upgrading := newFakeSocket(false)
	snapped := make(chan struct{})
	resolved := make(chan struct{})
	served := make(chan struct{})
	go func() {
		defer close(served)
		gen := h.dropGen()
		close(snapped)
		<-resolved
		h.serve(upgrading, "u1", true, gen)
	}()

	waitFor(t, snapped, "the handler to snapshot the drop generation")
	h.Drop("u1")
	close(resolved)

	waitFor(t, served, "the refused upgrade to return")
	waitFor(t, liveDone, "the dropped socket to close")
	waitFor(t, upgrading.closed, "the refused socket to close")
	if n := h.count(); n != 0 {
		t.Fatalf("hub has %d conns after a refused upgrade, want 0", n)
	}

	mustBroadcast(t, h, "after")
	expectNoMessage(t, upgrading)
}

func TestDropOfAnotherUserDoesNotRefuseUpgrade(t *testing.T) {
	noPings(t)

	h := NewHub()
	gen := h.dropGen()
	h.Drop("u2")

	s := newFakeSocket(false)
	served := make(chan struct{})
	go func() {
		defer close(served)
		h.serve(s, "u1", true, gen)
	}()
	t.Cleanup(func() {
		_ = s.Close()
		waitFor(t, served, "serve to return")
	})
	waitFor(t, s.ready, "the connection to register")

	mustBroadcast(t, h, "after")
	expectMessage(t, s, `"after"`)
}

func dialWS(t *testing.T, h *WebSocketHub) (*websocket.Conn, chan struct{}) {
	t.Helper()
	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.serve(ws, "u1", true, 0)
	}))
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		defer srv.Close()
		_ = c.Close()
		waitFor(t, served, "serve to return")
	})
	return c, served
}

func readErrors(c *websocket.Conn) chan error {
	errCh := make(chan error, 1)
	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				errCh <- err
				return
			}
		}
	}()
	return errCh
}

func TestWSPingPongKeepalive(t *testing.T) {
	oldPing, oldPong := pingPeriod, pongWait
	pingPeriod, pongWait = 5*time.Millisecond, 500*time.Millisecond
	t.Cleanup(func() { pingPeriod, pongWait = oldPing, oldPong })

	t.Run("client that never pongs is disconnected", func(t *testing.T) {
		c, served := dialWS(t, NewHub())
		pings := make(chan struct{}, 64)
		c.SetPingHandler(func(string) error {
			select {
			case pings <- struct{}{}:
			default:
			}
			return nil
		})
		errCh := readErrors(c)

		waitFor(t, pings, "a ping from the server")
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Fatal("server did not close the unresponsive client")
		}
		waitFor(t, served, "serve to return")
	})

	t.Run("client that pongs stays connected", func(t *testing.T) {
		c, _ := dialWS(t, NewHub())
		pings := make(chan struct{}, 256)
		c.SetPingHandler(func(data string) error {
			select {
			case pings <- struct{}{}:
			default:
			}
			return c.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
		})
		errCh := readErrors(c)

		for i := 0; i < int(pongWait/pingPeriod)+10; i++ {
			waitFor(t, pings, "a ping from the server")
		}
		select {
		case err := <-errCh:
			t.Fatalf("responsive client was disconnected: %v", err)
		default:
		}
	})
}
