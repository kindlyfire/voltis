package linking

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"voltis/metadata"
)

func TestLinkTransitions(t *testing.T) {
	now := time.Unix(1700000000, 0)
	from := map[State]Link{
		StateNone:      {State: StateNone},
		StateReview:    {State: StateReview, Candidates: []Candidate{{}}, Rejected: []string{"9"}},
		StateUnmatched: {State: StateUnmatched, RetryAt: &now, Attempts: 2, LastError: new("timeout")},
		StateLinked:    {State: StateLinked, ExternalID: new("1"), Origin: new(OriginAuto), Rejected: []string{"9"}},
		StateIgnored:   {State: StateIgnored, Rejected: []string{"3"}},
	}
	type want struct {
		state    State
		id       string
		rejected []string
		err      error
	}
	for _, c := range []struct {
		action string
		do     func(*Link) error
		want   map[State]want
	}{
		{"link 2", func(l *Link) error { l.LinkTo("2", OriginManual); return nil }, map[State]want{
			StateNone:      {StateLinked, "2", nil, nil},
			StateReview:    {StateLinked, "2", []string{"9"}, nil},
			StateUnmatched: {StateLinked, "2", nil, nil},
			StateLinked:    {StateLinked, "2", []string{"9", "1"}, nil},
			StateIgnored:   {StateLinked, "2", []string{"3"}, nil},
		}},
		{"link 1", func(l *Link) error { l.LinkTo("1", OriginManual); return nil }, map[State]want{
			StateLinked: {StateLinked, "1", []string{"9"}, nil},
		}},
		{"ignore", func(l *Link) error { l.Ignore(); return nil }, map[State]want{
			StateNone:      {StateIgnored, "", nil, nil},
			StateReview:    {StateIgnored, "", []string{"9"}, nil},
			StateUnmatched: {StateIgnored, "", nil, nil},
			StateLinked:    {StateIgnored, "", []string{"9", "1"}, nil},
			StateIgnored:   {StateIgnored, "", []string{"3"}, nil},
		}},
		{"reject 2, 9", func(l *Link) error { l.Reject([]string{"2", "9"}, now); return nil }, map[State]want{
			StateNone:      {StateUnmatched, "", []string{"2", "9"}, nil},
			StateReview:    {StateUnmatched, "", []string{"9", "2"}, nil},
			StateUnmatched: {StateUnmatched, "", []string{"2", "9"}, nil},
			StateLinked:    {StateUnmatched, "", []string{"9", "2", "1"}, nil},
			StateIgnored:   {StateUnmatched, "", []string{"3", "2", "9"}, nil},
		}},
		{"match 2", func(l *Link) error {
			return l.Match(MatchDecision{Link: &metadata.EntryKey{Provider: "fake", ID: "2"}}, now)
		}, map[State]want{
			StateNone:      {StateLinked, "2", nil, nil},
			StateReview:    {StateLinked, "2", []string{"9"}, nil},
			StateUnmatched: {StateLinked, "2", nil, nil},
			StateLinked:    {StateLinked, "1", []string{"9"}, ErrLinked},
			StateIgnored:   {StateLinked, "2", []string{"3"}, nil},
		}},
		{"match nothing", func(l *Link) error { return l.Match(MatchDecision{}, now) }, map[State]want{
			StateNone:      {StateUnmatched, "", nil, nil},
			StateReview:    {StateUnmatched, "", []string{"9"}, nil},
			StateUnmatched: {StateUnmatched, "", nil, nil},
			StateLinked:    {StateLinked, "1", []string{"9"}, ErrLinked},
			StateIgnored:   {StateUnmatched, "", []string{"3"}, nil},
		}},
	} {
		for s, w := range c.want {
			l := from[s]
			l.Rejected = slices.Clone(l.Rejected)
			err := c.do(&l)
			id := ""
			if l.ExternalID != nil {
				id = *l.ExternalID
			}
			if l.State != w.state || id != w.id || !reflect.DeepEqual(l.Rejected, w.rejected) || !errors.Is(err, w.err) {
				t.Errorf("%s from %s: got %s %q %v %v, want %s %q %v %v",
					c.action, s, l.State, id, l.Rejected, err, w.state, w.id, w.rejected, w.err)
			}
			if err == nil && (l.Candidates != nil || l.LastError != nil || l.Attempts != 0 ||
				(l.RetryAt != nil) != (l.State == StateUnmatched)) {
				t.Errorf("%s from %s left matcher state: %+v", c.action, s, l)
			}
		}
	}
}

func TestMatchOutcomes(t *testing.T) {
	now := time.Unix(1700000000, 0)
	l := Link{State: StateReview, Candidates: []Candidate{{}}, Rejected: []string{"9"}}
	if err := l.Match(MatchDecision{Candidates: []Candidate{{}, {}}}, now); err != nil ||
		l.State != StateReview || len(l.Candidates) != 2 || l.RetryAt != nil {
		t.Fatalf("review = %+v (%v)", l, err)
	}
	if err := l.Match(MatchDecision{}, now); err != nil || *l.RetryAt != now.Add(30*24*time.Hour) {
		t.Fatalf("nothing = %+v (%v)", l, err)
	}
	if l.Reject(nil, now.Add(time.Hour)); *l.RetryAt != now.Add(time.Hour+30*24*time.Hour) {
		t.Fatalf("rejected = %+v", l)
	}
	// Failures back off 1h·2ⁿ, at most a week; any other outcome starts over.
	for n, wait := range []time.Duration{1, 2, 4, 8, 16, 32, 64, 128, 168, 168} {
		if err := l.MatchFailed(errors.New("down"), now); err != nil || l.State != StateUnmatched || l.Attempts != n+1 ||
			*l.LastError != "down" || *l.RetryAt != now.Add(wait*time.Hour) || !slices.Equal(l.Rejected, []string{"9"}) {
			t.Fatalf("failure %d = %+v (%v)", n+1, l, err)
		}
	}
	if err := l.Match(MatchDecision{}, now); err != nil || l.Attempts != 0 || l.LastError != nil {
		t.Fatalf("after failures = %+v (%v)", l, err)
	}
	linked := Link{State: StateLinked, ExternalID: new("1"), Origin: new(OriginManual)}
	if err := linked.MatchFailed(errors.New("down"), now); !errors.Is(err, ErrLinked) {
		t.Fatalf("failure on a linked series: err = %v", err)
	}
}
