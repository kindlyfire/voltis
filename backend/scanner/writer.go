package scanner

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"voltis/lib/tasks"
	"voltis/models"
)

type committed struct {
	seq    int
	counts Counts
	err    error
}

var flushSpacing = 5 * time.Second

type writer struct {
	in       ScanInput
	tc       *tasks.TaskContext
	notify   Notifier
	res      *resolver
	fps      map[string]Fingerprint
	byID     map[string]Fingerprint
	keys     map[Key]string
	series   map[string]SeriesRef
	byURI    map[string]string
	byDir    map[string][]string
	cov      *coverage
	indexed  map[string]string
	behind   map[string][]string
	gone     map[string]bool
	trust    bool
	queue    []FSFile
	inflight int
	sets     map[string]*SeriesChanges
	prog     Progress
	seq      int
	last     time.Time
}

func newWriter(in ScanInput, tc *tasks.TaskContext, notify Notifier, res *resolver,
	fps []Fingerprint, refs []SeriesRef) *writer {
	w := &writer{
		in:      in,
		tc:      tc,
		notify:  notify,
		res:     res,
		fps:     make(map[string]Fingerprint, len(fps)),
		byID:    make(map[string]Fingerprint, len(fps)),
		keys:    make(map[Key]string, len(fps)+len(refs)),
		series:  make(map[string]SeriesRef, len(refs)),
		byURI:   make(map[string]string, len(refs)),
		byDir:   map[string][]string{},
		indexed: map[string]string{},
		behind:  map[string][]string{},
		gone:    map[string]bool{},
		trust:   true,
		sets:    map[string]*SeriesChanges{},
		prog:    Progress{Phase: "walking"},
	}
	for _, f := range fps {
		w.byID[f.ID] = f
		w.keys[Key{deref(f.ParentID), f.URIPart}] = f.ID
		if key, ok := res.file(f.Path); ok {
			w.fps[key] = f
			w.indexed[f.ID] = key
		} else if at, ok := res.blocker(f.Path); ok {
			w.behind[at] = append(w.behind[at], f.ID)
		}
	}
	w.cov = newCoverage(w.fps)
	for _, ref := range refs {
		w.series[ref.ID] = ref
		w.byURI[ref.URI] = ref.ID
		w.keys[Key{"", ref.URIPart}] = ref.ID
		if ref.FileURI != nil {
			w.addDir(*ref.FileURI, ref.ID)
		}
	}
	return w
}

func (w *writer) run(ctx context.Context, events <-chan Event, walkDone <-chan error, jobs chan<- FSFile,
	results <-chan Result, flushes chan<- flush, done <-chan committed) error {
	jobsOpen, flushesOpen := true, true
	closeJobs := func() {
		if jobsOpen {
			jobsOpen = false
			close(jobs)
		}
	}
	closeFlushes := func() {
		if flushesOpen {
			flushesOpen = false
			close(flushes)
		}
	}
	defer closeJobs()
	defer closeFlushes()

	for ev := range events {
		w.event(ev)
		w.publish()
	}
	if err := <-walkDone; err != nil {
		return err
	}

	w.prog.Phase = "parsing"
	if len(w.queue) == 0 {
		closeJobs()
	}

	tick := time.NewTicker(flushSpacing)
	defer tick.Stop()
	busy := false
	for w.inflight > 0 || len(w.queue) > 0 || busy {
		var send chan<- FSFile
		var next FSFile
		if len(w.queue) > 0 {
			send = jobs
			next = w.queue[0]
		}
		select {
		case send <- next:
			w.queue = w.queue[1:]
			w.inflight++
			if len(w.queue) == 0 {
				closeJobs()
			}
		case r := <-results:
			w.inflight--
			w.place(r)
		case c := <-done:
			busy = false
			if c.err != nil {
				return c.err
			}
			w.saved(c)
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		if !busy && len(w.sets) > 0 && time.Since(w.last) >= flushSpacing {
			flushes <- w.take(false)
			busy = true
		}
		w.publish()
	}

	w.prog.Phase = "saving"
	w.publish()
	if delay := flushSpacing - time.Since(w.last); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case flushes <- w.take(true):
	case <-ctx.Done():
		return ctx.Err()
	}
	closeFlushes()

	var c committed
	select {
	case c = <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if c.err != nil {
		return c.err
	}
	w.saved(c)
	w.prog.Phase = "done"
	w.publish()
	return nil
}

func (w *writer) event(ev Event) {
	switch ev.Kind {
	case Listed:
		parent := filepath.Clean(ev.Path)
		if ev.Dir == "" {
			w.res.reject(parent)
			w.distrust("a directory identity could not be verified", ev.Path)
			return
		}
		at, _ := w.res.dir(parent)
		w.res.verified(parent, ev.Dir)
		if at != ev.Dir {
			w.distrust("a directory indexed as "+at+" listed as "+ev.Dir, ev.Path)
			return
		}
		if !w.trust {
			return
		}
		w.absent(w.cov.listed(ev.Dir, ev.Names))
		for _, name := range ev.Files {
			file := filepath.Join(ev.Dir, name)
			w.absent(w.cov.listed(file, nil))
			w.beneath(file)
		}
	case Seen:
		w.prog.Found++
		old, exists := w.at(ev.File.Path)
		if changed(ev.File, old, exists, w.in.Force) {
			w.queue = append(w.queue, ev.File)
			w.prog.Total++
		} else {
			w.prog.Unchanged++
		}
	case Failed:
		slog.Warn("[scanner] failed to read path, retaining missing entries at or below it",
			"path", ev.Path, "err", ev.Err, "library", w.in.LibraryID)
		w.logf("Failed to read %s: %v; missing entries at or below it were retained\n", ev.Path, ev.Err)
	}
}

func (w *writer) absent(ids []string) {
	if len(ids) == 0 || !w.trust {
		return
	}
	now := newResolver()
	for _, id := range ids {
		f := w.byID[id]
		if key, ok := now.file(f.Path); !ok || key != w.indexed[id] {
			w.distrust("a missing entry now resolves elsewhere", f.Path)
			return
		}
	}
	w.mark(ids)
}

func (w *writer) beneath(file string) {
	ids := w.behind[file]
	if len(ids) == 0 || !w.trust {
		return
	}
	if !plainFile(file) {
		w.distrust("the file that replaced a directory is no longer a plain file", file)
		return
	}
	now := newResolver()
	for _, id := range ids {
		f := w.byID[id]
		if at, ok := now.blocker(f.Path); !ok || at != file {
			w.distrust("a missing entry no longer sits under the file that replaced its directory", f.Path)
			return
		}
	}
	w.mark(ids)
}

func (w *writer) mark(ids []string) {
	for _, id := range ids {
		w.gone[id] = true
	}
}

func (w *writer) distrust(reason, path string) {
	if !w.trust {
		return
	}
	w.trust = false
	clear(w.gone)
	slog.Warn("[scanner] the filesystem changed under the scan, removals suppressed",
		"reason", reason, "path", path, "library", w.in.LibraryID)
	w.logf("The filesystem changed under the scan (%s, at %s); nothing was removed.\n", reason, path)
}

func (w *writer) place(r Result) {
	w.prog.Processed++
	old, hadOld := w.at(r.File.Path)

	if r.Item == nil {
		w.prog.Failed++
		slog.Warn("[scanner] failed to parse file", "path", r.File.Path)
		w.logf("Failed to parse %s\n", r.File.Path)
		if hadOld {
			s := w.set(deref(old.ParentID))
			s.Invalid = append(s.Invalid, old.ID)
		}
		return
	}

	ref, ok := w.resolveSeries(r.Item.Series)
	if !ok {
		w.prog.Failed++
		slog.Warn("[scanner] series key conflict, skipping", "path", r.File.Path)
		w.logf("Series key conflict for %s, skipping\n", r.File.Path)
		return
	}

	parent := ""
	if ref != nil {
		parent = ref.ID
	}
	s := w.set(parent)

	own := ""
	if hadOld {
		own = old.ID
	}
	k := Key{parent, r.Item.URIPart}
	occ := w.keys[k]

	var id string
	switch {
	case occ == "" || occ == own:
		id = cmp.Or(own, models.MakeContentID())
	case w.gone[occ] && own == "":
		id = occ
		delete(w.gone, occ)
	case w.gone[occ]:
		id = own
		delete(w.gone, occ)
		s.Deletes = append(s.Deletes, occ)
	default:
		w.prog.Failed++
		slog.Warn("[scanner] URI conflict, skipping", "path", r.File.Path, "uri_part", r.Item.URIPart, "parent_id", parent)
		w.logf("URI conflict for file %s, skipping (uri_part: %s, parent_id: %s)\n", r.File.Path, r.Item.URIPart, parent)
		return
	}

	if prev, ok := w.byID[id]; ok {
		if pk := (Key{deref(prev.ParentID), prev.URIPart}); pk != k && w.keys[pk] == id {
			delete(w.keys, pk)
		}
		if oldParent := deref(prev.ParentID); oldParent != parent {
			w.set(oldParent)
		}
	}
	w.keys[k] = id
	s.Writes = append(s.Writes, write{id: id, item: r.Item, added: own == ""})
}

func (w *writer) resolveSeries(p *ParsedSeries) (*SeriesRef, bool) {
	if p == nil {
		return nil, true
	}

	uri := p.URIPrefix + "/" + p.URIPart
	key := Key{"", p.URIPart}

	id, found := w.byURI[uri]
	if !found && p.FileURI != nil {
		id, found = w.dirSeries(*p.FileURI)
	}

	if !found {
		if _, taken := w.keys[key]; taken {
			return nil, false
		}
		ref := SeriesRef{ID: models.MakeContentID(), URI: uri, URIPart: p.URIPart, Type: p.ContentType, FileURI: p.FileURI}
		w.series[ref.ID] = ref
		w.byURI[uri] = ref.ID
		if p.FileURI != nil {
			w.addDir(*p.FileURI, ref.ID)
		}
		w.keys[key] = ref.ID
		s := w.set(ref.ID)
		s.New = true
		s.Ref = ref
		return &ref, true
	}

	ref := w.series[id]
	s := w.set(id)
	if ref.URI != uri {
		if occ, taken := w.keys[key]; taken && occ != id {
			return nil, false
		}
		if s.OldURI == "" {
			s.OldURI = ref.URI
		}
		delete(w.byURI, ref.URI)
		if prev := (Key{"", ref.URIPart}); w.keys[prev] == id {
			delete(w.keys, prev)
		}
		ref.URI = uri
		w.byURI[uri] = id
	}
	if ref.FileURI != nil && (p.FileURI == nil || *p.FileURI != *ref.FileURI) {
		w.dropDir(*ref.FileURI, id)
	}
	if p.FileURI != nil {
		w.addDir(*p.FileURI, id)
	}
	ref.URIPart = p.URIPart
	ref.FileURI = p.FileURI
	w.keys[key] = id
	w.series[id] = ref
	s.Ref = ref
	return &ref, true
}

func (w *writer) at(path string) (Fingerprint, bool) {
	key, ok := w.res.file(path)
	if !ok {
		return Fingerprint{}, false
	}
	f, ok := w.fps[key]
	return f, ok
}

func (w *writer) addDir(path, id string) {
	dir, ok := w.res.dir(path)
	if !ok {
		return
	}
	ids := w.byDir[dir]
	if i, ok := slices.BinarySearch(ids, id); !ok {
		w.byDir[dir] = slices.Insert(ids, i, id)
	}
}

func (w *writer) dropDir(path, id string) {
	dir, ok := w.res.dir(path)
	if !ok {
		return
	}
	ids := w.byDir[dir]
	i, ok := slices.BinarySearch(ids, id)
	if !ok {
		return
	}
	if ids = slices.Delete(ids, i, i+1); len(ids) == 0 {
		delete(w.byDir, dir)
		return
	}
	w.byDir[dir] = ids
}

func (w *writer) dirSeries(path string) (string, bool) {
	dir, ok := w.res.dir(path)
	if !ok {
		return "", false
	}
	ids := w.byDir[dir]
	if len(ids) == 0 {
		return "", false
	}
	return ids[0], true
}

func (w *writer) set(seriesID string) *SeriesChanges {
	if s, ok := w.sets[seriesID]; ok {
		return s
	}
	s := &SeriesChanges{Ref: w.series[seriesID]}
	w.sets[seriesID] = s
	return s
}

func (w *writer) take(final bool) flush {
	if final && w.trust {
		for _, id := range slices.Sorted(maps.Keys(w.gone)) {
			s := w.set(deref(w.byID[id].ParentID))
			s.Deletes = append(s.Deletes, id)
		}
	}

	w.seq++
	w.last = time.Now()
	f := flush{seq: w.seq, at: w.last, sets: w.sets, final: final, keep: !w.trust}
	if final {
		w.gone = nil
	} else {
		f.gone = maps.Clone(w.gone)
	}
	w.sets = map[string]*SeriesChanges{}
	return f
}

func (w *writer) saved(c committed) {
	w.prog.Saved.Added += c.counts.Added
	w.prog.Saved.Updated += c.counts.Updated
	w.prog.Saved.Removed += c.counts.Removed
	w.prog.CommitSeq = c.seq
	if w.notify != nil {
		w.notify.CatalogChanged(CatalogChanged{LibraryID: w.in.LibraryID, TaskID: w.taskID(), CommitSeq: c.seq})
	}
}

func (w *writer) publish() {
	if w.tc != nil {
		w.tc.Progress(w.prog)
	}
}

func (w *writer) logf(format string, args ...any) {
	if w.tc != nil {
		w.tc.Log(format, args...)
	}
}

func (w *writer) taskID() string {
	if w.tc == nil {
		return ""
	}
	return w.tc.ID()
}
