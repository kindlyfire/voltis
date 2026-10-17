package scrobble

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"voltis/providers/mangabaka"
)

func num(f float64) *float64 { return &f }

func TestDecide(t *testing.T) {
	for _, c := range []struct {
		name           string
		ours           Values
		remote         *mangabaka.Entry
		force, private bool
		want           string // the Write's JSON, or "" for nothing to send
		create         bool
		ahead          string // the fields Ahead lists
	}{
		{"no entry", Values{Chapter: num(2)}, nil, false, false, `{"state":"reading","progress_chapter":2}`, true, ""},
		{"no entry, nothing to send", Values{}, nil, false, false, "", false, ""},
		{"no entry, private", Values{Volume: num(1), State: new("paused")}, nil, false, true,
			`{"state":"paused","progress_volume":1,"is_private":true}`, true, ""},
		{"remote ahead on the chapter", Values{Chapter: num(41), Volume: num(5)},
			&mangabaka.Entry{State: "reading", Chapter: num(52)}, false, false, `{"progress_volume":5}`, false, "chapter"},
		{"equal", Values{Chapter: num(3), State: new("reading")},
			&mangabaka.Entry{State: "reading", Chapter: num(3)}, false, false, "", false, ""},
		{"completed isn't replaced", Values{State: new("reading")}, &mangabaka.Entry{State: "completed"}, false, false, "", false, "state"},
		{"completed replaces", Values{State: new("completed")}, &mangabaka.Entry{State: "reading"}, false, false,
			`{"state":"completed"}`, false, ""},
		{"rereading isn't replaced", Values{State: new("reading")}, &mangabaka.Entry{State: "rereading"}, false, false, "", false, "state"},
		{"force lowers", Values{Chapter: num(3), State: new("reading")},
			&mangabaka.Entry{State: "completed", Chapter: num(9)}, true, false, `{"state":"reading","progress_chapter":3}`, false, "chapter state"},
		{"remote ahead on the volume", Values{Chapter: num(3), Volume: num(1)},
			&mangabaka.Entry{State: "reading", Chapter: num(3), Volume: num(2)}, false, false, "", false, "volume"},
		{"chapter 0 is empty", Values{Chapter: num(0)}, &mangabaka.Entry{State: "reading"}, false, false, "", false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.Join(Ahead(c.ours, c.remote), " "); got != c.ahead {
				t.Errorf("Ahead = %q, want %q", got, c.ahead)
			}
			w, create, ok := Decide(c.ours, c.remote, c.force, c.private)
			got := ""
			if ok {
				raw, _ := json.Marshal(w)
				got = string(raw)
			}
			if got != c.want || create != c.create {
				t.Fatalf("got %s (create %v), want %s (create %v)", got, create, c.want, c.create)
			}
		})
	}

	merged := Merge(Values{Chapter: num(4), State: new("paused")}, Values{Volume: num(2), State: new("completed")},
		Values{Chapter: num(7), State: new("reading")})
	if raw, _ := json.Marshal(merged); string(raw) != `{"chapter":7,"volume":2,"state":"completed"}` {
		t.Errorf("Merge = %s", raw)
	}

	if msg := OutOfRange(Values{Chapter: num(20190)}); msg != "Chapter 20190 is out of range" {
		t.Errorf("OutOfRange = %q", msg)
	}
	if msg := OutOfRange(Values{Chapter: num(10000), Volume: num(12.5)}); msg != "" {
		t.Errorf("OutOfRange = %q", msg)
	}

	for status, want := range map[string]string{"reading": "reading", "completed": "completed", "on_hold": "paused",
		"dropped": "dropped", "plan_to_read": "plan_to_read"} {
		if got := MapState(&status); got == nil || *got != want {
			t.Errorf("MapState(%s) = %v", status, got)
		}
	}
	if MapState(nil) != nil {
		t.Error("MapState(nil) isn't nil")
	}

	apiErr := func(status int, code string) error {
		return &mangabaka.Error{Status: status, Code: code, Message: "x"}
	}
	for _, c := range []struct {
		err    error
		method string
		want   mangabaka.Outcome
	}{
		{apiErr(401, ""), http.MethodPatch, mangabaka.Rejected},
		{apiErr(400, "rate_limited"), http.MethodPost, mangabaka.Transient},
		{apiErr(400, "duplicate_entry"), http.MethodPost, mangabaka.Conflict},
		{apiErr(404, ""), http.MethodPatch, mangabaka.Conflict},
		{apiErr(400, ""), http.MethodPatch, mangabaka.Failed},
		{fmt.Errorf("dial: %w", http.ErrHandlerTimeout), http.MethodGet, mangabaka.Transient},
	} {
		if got := mangabaka.Classify(c.err, c.method); got != c.want {
			t.Errorf("Classify(%v, %s) = %v, want %v", c.err, c.method, got, c.want)
		}
	}
}
