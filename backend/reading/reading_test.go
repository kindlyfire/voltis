package reading

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"voltis/lib/fp"
	"voltis/models"
)

// TestReadingTransition runs the transition table that the Android app's port runs too.
func TestReadingTransition(t *testing.T) {
	type state struct {
		Status   *models.ReadingStatus `json:"status"`
		Progress json.RawMessage       `json:"progress"`
		LastRead *string               `json:"last_read"`
	}
	var cases []struct {
		Name string `json:"name"`
		Cur  state  `json:"cur"`
		Op   struct {
			Op       Op                    `json:"op"`
			Status   *models.ReadingStatus `json:"status"`
			Progress json.RawMessage       `json:"progress"`
			Snapshot *state                `json:"snapshot"`
		} `json:"op"`
		End  json.RawMessage `json:"end"`
		Want struct {
			state
			Outcome  Outcome `json:"outcome"`
			Previous bool    `json:"previous"`
			Write    bool    `json:"write"`
		} `json:"want"`
	}
	raw, err := os.ReadFile("testdata/reading_transitions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		t.Fatal("no cases", err)
	}
	now := time.Now()
	earlier := now.Add(-time.Hour)
	str := func(p *models.ReadingStatus) string {
		if p == nil {
			return ""
		}
		return string(*p)
	}
	at := func(label *string) *time.Time {
		if label == nil {
			return nil
		}
		switch *label {
		case "now":
			return &now
		case "earlier":
			return &earlier
		}
		return nil
	}
	snapshot := func(s state) Snapshot {
		return Snapshot{Status: s.Status, Progress: s.Progress, LastReadAt: at(s.LastRead)}
	}
	sameJSON := func(a, b json.RawMessage) bool {
		var x, y any
		return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
	}
	for _, tc := range cases {
		req := Request{Op: tc.Op.Op, Status: tc.Op.Status, Progress: tc.Op.Progress}
		if tc.Op.Snapshot != nil {
			req.Snapshot = new(snapshot(*tc.Op.Snapshot))
		}
		next, outcome, previous, write := transition(snapshot(tc.Cur), req, tc.End, now)
		w := tc.Want
		if !fp.PtrEq(next.Status, w.Status) || outcome != w.Outcome || (previous != nil) != w.Previous ||
			write != w.Write || !sameJSON(next.Progress, w.Progress) {
			t.Errorf("%s: got %v %s %v %v %s", tc.Name, str(next.Status), outcome, previous != nil, write,
				next.Progress)
		}
		if wantAt := at(w.LastRead); (next.LastReadAt == nil) != (wantAt == nil) ||
			wantAt != nil && !next.LastReadAt.Equal(*wantAt) {
			t.Errorf("%s: last read %v", tc.Name, next.LastReadAt)
		}
		if previous != nil && !fp.PtrEq(previous.Status, tc.Cur.Status) {
			t.Errorf("%s: previous %v", tc.Name, str(previous.Status))
		}
	}

	// Numeric, not lexical: 9 was delivered before 10.
	apply, conflict := checkRevision(new("tabcdefghijklmnop:10"), Request{WriterID: "tabcdefghijklmnop", Seq: 9})
	if apply || conflict {
		t.Fatal("seq 9 after 10 was not dropped")
	}
}
