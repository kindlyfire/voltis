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

func serveFake(t *testing.T, h *WebSocketHub, s *fakeSocket, user string) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serve(s, user)
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
	stalledDone := serveFake(t, h, stalled, "u1")
	serveFake(t, h, healthy, "u1")

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

func TestHubUserFilter(t *testing.T) {
	noPings(t)

	h := NewHub()
	a := newFakeSocket(false)
	b := newFakeSocket(false)
	serveFake(t, h, a, "u1")
	serveFake(t, h, b, "u2")

	user := "u1"
	logs := "chunk\n"
	h.BroadcastTaskEvent(&models.Task{ID: "task_1", UserID: &user}, nil, &logs)
	mustBroadcast(t, h, "queue")

	expectMessage(t, a, `"task_1"`, `"chunk\n"`)
	expectMessage(t, a, `"queue"`)
	expectMessage(t, b, `"queue"`)
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
		h.serve(ws, "u1")
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
