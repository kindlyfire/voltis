// Package reading owns a user's reading state: the transitions of an item's status and progress,
// the series they start, and the writes that persist them.
package reading

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"voltis/lib/fp"
	"voltis/models"
)

// Op is an event or command a Request applies.
type Op string

const (
	OpPosition      Op = "position"
	OpFinish        Op = "finish"
	OpSetStatus     Op = "set_status"
	OpMarkCompleted Op = "mark_completed"
	OpClear         Op = "clear"
	OpRestore       Op = "restore"
	OpSeriesStatus  Op = "series_status"
)

// Outcome says what a write did.
type Outcome string

const (
	OutcomeNone           Outcome = "none"
	OutcomeStarted        Outcome = "started"
	OutcomeMovedToReading Outcome = "moved_to_reading"
	OutcomeSaved          Outcome = "saved"
	OutcomeCompleted      Outcome = "completed"
	OutcomeStatusSet      Outcome = "status_set"
	OutcomeCleared        Outcome = "cleared"
	OutcomeRestored       Outcome = "restored"
	OutcomeSeriesStatus   Outcome = "series_status"
)

// Snapshot is what Undo restores: the item's state before an event changed it.
type Snapshot struct {
	Status     *models.ReadingStatus `json:"status"`
	Progress   json.RawMessage       `json:"progress"`
	LastReadAt *time.Time            `json:"last_read_at"`
}

// SeriesPrev is a series' status before a child started it, with its revision after the start.
type SeriesPrev struct {
	Revision        *string               `json:"revision"`
	Status          *models.ReadingStatus `json:"status"`
	StatusUpdatedAt *time.Time            `json:"status_updated_at"`
}

// Request is an event or command on one item.
type Request struct {
	Op           Op
	BaseRevision *string
	// WriterID and Seq identify a reader's write, which carries the reader's revision. Without a
	// writer, the write carries Revision.
	WriterID string
	Seq      int64
	Revision models.Revision
	Progress json.RawMessage
	Status   *models.ReadingStatus
	Snapshot *Snapshot
	Series   *SeriesPrev
	// IDs are the content a series clear is expected to affect. The result's items include them,
	// wherever they live now, besides the series' current children.
	IDs []string
}

// State is one content's reading state.
type State struct {
	Revision          *string               `json:"revision"            db:"revision"`
	Status            *models.ReadingStatus `json:"status"              db:"status"`
	StatusUpdatedAt   *time.Time            `json:"status_updated_at"   db:"status_updated_at"`
	Progress          json.RawMessage       `json:"progress"            db:"progress"`
	ProgressUpdatedAt *time.Time            `json:"progress_updated_at" db:"progress_updated_at"`
	LastReadAt        *time.Time            `json:"last_read_at"        db:"last_read_at"`
	// ReadingSeq orders this state against the content's other states; 0 for content the user has
	// no row for. It serializes as a decimal string.
	ReadingSeq models.ReadingSeq `json:"reading_seq" db:"reading_seq"`
}

func (s State) snapshot() Snapshot {
	return Snapshot{Status: s.Status, Progress: s.Progress, LastReadAt: s.LastReadAt}
}

// Item is one content's complete reading state.
type Item struct {
	ID string `json:"id" db:"id"`
	State
}

// SeriesInfo is a series' state with its children's counts.
type SeriesInfo struct {
	ID string `json:"id" db:"id"`
	State
	CaughtUp               bool `json:"caught_up"                db:"caught_up"`
	ChildrenCount          int  `json:"children_count"           db:"children_count"`
	CompletedChildrenCount int  `json:"completed_children_count" db:"completed_children_count"`
	DroppedChildrenCount   int  `json:"dropped_children_count"   db:"dropped_children_count"`
	// The reader that wrote the series row, if one did.
	Writer *string `json:"writer" db:"-"`
}

// Envelope is an item's state with its series', read together, and the reader that wrote the
// state, if any.
type Envelope struct {
	State  State       `json:"state"`
	Series *SeriesInfo `json:"series"`
	Writer *string     `json:"writer"`
}

// Result is what a write did, with the item's state and its series' as of the write.
type Result struct {
	Envelope
	Outcome  Outcome   `json:"outcome"`
	Previous *Snapshot `json:"previous"`
	// The series' status before this write started it, and its revision after.
	SeriesPrevious *SeriesPrev `json:"series_previous"`
	// Items holds the current state of every child a series clear covers, also when the request was
	// delivered before. Only set for a clear of a series.
	Items []Item `json:"items,omitempty"`
}

// SeriesRequest is a series-wide action.
type SeriesRequest struct {
	Action        SeriesAction
	UntilID       string
	IncludeUnread bool
	// A reader's request: its writes carry the reader's revision.
	WriterID string
	Seq      int64
	// IDs are the content the client expects the action to affect; the result's items include them
	// besides the current scope, so a catalog change since doesn't hide an affected volume.
	IDs []string
}

// SeriesAction is a series-wide action.
type SeriesAction string

const (
	ActionMarkThrough         SeriesAction = "mark_through"
	ActionMarkSeriesCompleted SeriesAction = "mark_series_completed"
	ActionClear               SeriesAction = "clear"
)

// SeriesResult is the series' state and that of every volume the action covers, as of the end of
// the write.
type SeriesResult struct {
	Count  int    `json:"count"`
	Series Item   `json:"series"`
	Items  []Item `json:"items"`
}

// BulkRequest sets or clears the status of several content at once.
type BulkRequest struct {
	IDs             []string
	Reset           bool // clear the content and the children of selected series
	Status          *models.ReadingStatus
	IncludeChildren bool
}

var writerIDPattern = regexp.MustCompile(`^t[0-9a-z]{16}$`)

// validateWriter checks a writer id and seq, which go together.
func validateWriter(id string, seq int64) error {
	if id != "" && !writerIDPattern.MatchString(id) {
		return InvalidError{"invalid writer_id"}
	}
	if (id == "") != (seq == 0) {
		return InvalidError{"writer_id and seq go together"}
	}
	return nil
}

// validate checks what the request holds. Whether the op needs a writer is the caller's rule.
func (r Request) validate() error {
	if err := validateWriter(r.WriterID, r.Seq); err != nil {
		return err
	}
	if r.Op == OpPosition && !models.IsJSONObject(r.Progress) {
		return InvalidError{"progress must be a JSON object"}
	}
	if r.Op == OpRestore && (r.Snapshot == nil ||
		r.Snapshot.Progress != nil && string(r.Snapshot.Progress) != "null" && !models.IsJSONObject(r.Snapshot.Progress)) {
		return InvalidError{"restore needs a snapshot"}
	}
	if r.Snapshot != nil && r.Snapshot.Status != nil && !slices.Contains(models.ReadingStatuses, *r.Snapshot.Status) {
		return InvalidError{"invalid snapshot status"}
	}
	if r.Series != nil && r.Series.Status != nil && !slices.Contains(models.ReadingStatuses, *r.Series.Status) {
		return InvalidError{"invalid series status"}
	}
	if r.Series != nil && r.Op != OpRestore && r.Op != OpSeriesStatus {
		return InvalidError{"series is only for restore and series_status"}
	}
	return nil
}

// revisionWriter is the reader that wrote revision, if a reader did.
func revisionWriter(revision *string) *string {
	if writer, _, ok := models.ParseRevision(revision); ok && writerIDPattern.MatchString(writer) {
		return &writer
	}
	return nil
}

// delivered says whether rev was written by the writer at or above seq: a request of the writer at
// that seq was delivered already, or overtaken.
func delivered(rev *string, writerID string, seq int64) bool {
	writer, revSeq, ok := models.ParseRevision(rev)
	return ok && writerID != "" && writer == writerID && seq <= revSeq
}

// checkRevision decides whether a reader's write applies to a row at revision cur. A request of
// the writer that wrote cur, at or below its seq, was delivered already (or overtaken): it changes
// nothing. Otherwise the request must be based on cur, the writer's own earlier writes included.
func checkRevision(cur *string, req Request) (apply bool, conflict bool) {
	if req.WriterID == "" {
		return true, false
	}
	if delivered(cur, req.WriterID, req.Seq) {
		return false, false
	}
	based := fp.PtrEq(cur, req.BaseRevision)
	return based, !based
}

// endProgress is the progress of finished content: the last page of a comic, or the end of a book
// (at the reader's finishing locator when it supplies one). A series has no progress.
func endProgress(typ string, pages int, supplied json.RawMessage) json.RawMessage {
	var end map[string]any
	switch typ {
	case "comic":
		end = map[string]any{"current_page": max(pages-1, 0), "progress_percent": 100, "at_end": true}
	case "book":
		end = map[string]any{"progress_percent": 100, "at_end": true}
		var s struct {
			Book json.RawMessage `json:"book"`
		}
		if json.Unmarshal(supplied, &s) == nil && s.Book != nil && string(s.Book) != "null" {
			end["book"] = s.Book
		}
	default:
		return json.RawMessage("{}")
	}
	raw, _ := json.Marshal(end)
	return raw
}

func isCompleted(status *models.ReadingStatus) bool {
	return status != nil && *status == models.StatusCompleted
}

func isHeld(status *models.ReadingStatus) bool {
	return status != nil &&
		(*status == models.StatusPlanToRead || *status == models.StatusOnHold || *status == models.StatusDropped)
}

// transition applies an event or command to an item's state. previous is the state that Undo
// restores, set only when reading overrode a deliberate status.
func transition(cur Snapshot, req Request, end json.RawMessage, now time.Time,
) (next Snapshot, outcome Outcome, previous *Snapshot, write bool) {
	next = cur
	switch req.Op {
	case OpPosition:
		next.Progress, next.LastReadAt = req.Progress, &now
		switch {
		case cur.Status == nil:
			next.Status, outcome = new(models.StatusReading), OutcomeStarted
		case isHeld(cur.Status):
			next.Status, outcome, previous = new(models.StatusReading), OutcomeMovedToReading, &cur
		default: // reading keeps its status, and so does a completed item being read again
			outcome = OutcomeSaved
		}
	case OpFinish:
		if isCompleted(cur.Status) {
			return cur, OutcomeNone, nil, false
		}
		next.Status, next.Progress, next.LastReadAt, outcome = new(models.StatusCompleted), end, &now, OutcomeCompleted
		if isHeld(cur.Status) {
			previous = &cur
		}
	case OpSetStatus:
		if isCompleted(req.Status) {
			return transition(cur, Request{Op: OpMarkCompleted}, end, now)
		}
		next.Status, outcome = req.Status, OutcomeStatusSet
	case OpMarkCompleted:
		next.Status, next.Progress, outcome = new(models.StatusCompleted), end, OutcomeCompleted
	case OpClear:
		next, outcome = Snapshot{Progress: json.RawMessage("{}")}, OutcomeCleared
	case OpRestore:
		next, outcome = *req.Snapshot, OutcomeRestored
	}
	if next.Progress == nil || string(next.Progress) == "null" {
		next.Progress = json.RawMessage("{}")
	}
	return next, outcome, previous, true
}

// startsSeries says whether an outcome moves the item into reading or completed, which starts its
// series.
func startsSeries(req Request, outcome Outcome) bool {
	switch outcome {
	case OutcomeStarted, OutcomeMovedToReading, OutcomeCompleted:
		return true
	case OutcomeStatusSet:
		return req.Status != nil && *req.Status == models.StatusReading
	}
	return false
}
