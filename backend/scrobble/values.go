// Package scrobble sends users' reading progress on linked series to MangaBaka.
package scrobble

import (
	"strconv"

	"voltis/providers/mangabaka"
)

// maxProgress is the highest chapter or volume MangaBaka accepts.
const maxProgress = 10000

// Values are what Voltis has for a series, in MangaBaka's terms. Nil is nothing to send.
type Values struct {
	Chapter *float64 `json:"chapter,omitempty"`
	Volume  *float64 `json:"volume,omitempty"`
	State   *string  `json:"state,omitempty"`
}

// MapState maps a series status to a MangaBaka state.
func MapState(seriesStatus *string) *string {
	if seriesStatus == nil {
		return nil
	}
	switch *seriesStatus {
	case "reading", "completed", "dropped", "plan_to_read":
		return new(*seriesStatus)
	case "on_hold":
		return new("paused")
	}
	return nil
}

var stateRank = map[string]int{"plan_to_read": 1, "dropped": 2, "paused": 3, "reading": 4, "completed": 5}

// Merge combines the values of series linked to the same MangaBaka id: the highest numbers, and
// the most advanced state.
func Merge(vs ...Values) Values {
	var out Values
	for _, v := range vs {
		out.Chapter, out.Volume = maxPtr(out.Chapter, v.Chapter), maxPtr(out.Volume, v.Volume)
		if v.State != nil && (out.State == nil || stateRank[*v.State] > stateRank[*out.State]) {
			out.State = v.State
		}
	}
	return out
}

func maxPtr(a, b *float64) *float64 {
	if a == nil || b != nil && *b > *a {
		return b
	}
	return a
}

// OutOfRange is the failure, shown to the user, of values MangaBaka would refuse, or "". A number
// that high is usually misread.
func OutOfRange(v Values) string {
	if v.Chapter != nil && *v.Chapter > maxProgress {
		return "Chapter " + fmtNum(*v.Chapter) + " is out of range"
	}
	if v.Volume != nil && *v.Volume > maxProgress {
		return "Volume " + fmtNum(*v.Volume) + " is out of range"
	}
	return ""
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Decide picks the fields to write over remote, the user's entry or nil, so that MangaBaka is
// never lowered unless force. With no entry it creates one, private if asked. ok is false when
// nothing is left to send.
func Decide(ours Values, remote *mangabaka.Entry, force, private bool) (w mangabaka.Write, create bool, ok bool) {
	// MangaBaka stores 0 as null, so 0 is nothing to send.
	chapter, volume := positive(ours.Chapter), positive(ours.Volume)
	if remote == nil {
		if chapter == nil && volume == nil && ours.State == nil {
			return w, false, false
		}
		w = mangabaka.Write{Chapter: chapter, Volume: volume, State: ours.State}
		if w.State == nil {
			w.State = new("reading")
		}
		if private {
			w.Private = new(true)
		}
		return w, true, true
	}
	if chapter != nil && (force || remote.Chapter == nil || *chapter > *remote.Chapter) {
		w.Chapter = chapter
	}
	if volume != nil && (force || remote.Volume == nil || *volume > *remote.Volume) {
		w.Volume = volume
	}
	if s := ours.State; s != nil && *s != remote.State {
		kept := remote.State == "completed" && *s != "completed" || remote.State == "rereading" && *s == "reading"
		if force || !kept {
			w.State = s
		}
	}
	return w, false, w != mangabaka.Write{}
}

// Ahead lists the fields, as "chapter", "volume" or "state", that MangaBaka keeps over ours: the ones
// force would write over the user's entry remote but a normal send doesn't.
func Ahead(ours Values, remote *mangabaka.Entry) []string {
	out := []string{}
	if remote == nil {
		return out
	}
	forced, _, _ := Decide(ours, remote, true, false)
	kept, _, _ := Decide(ours, remote, false, false)
	if forced.Chapter != nil && kept.Chapter == nil && !equal(forced.Chapter, remote.Chapter) {
		out = append(out, "chapter")
	}
	if forced.Volume != nil && kept.Volume == nil && !equal(forced.Volume, remote.Volume) {
		out = append(out, "volume")
	}
	if forced.State != nil && kept.State == nil {
		out = append(out, "state")
	}
	return out
}

func equal(a, b *float64) bool { return a != nil && b != nil && *a == *b }

func positive(f *float64) *float64 {
	if f == nil || *f <= 0 {
		return nil
	}
	return f
}
