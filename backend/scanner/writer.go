package scanner

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"voltis/lib/tasks"
	"voltis/models"

	"golang.org/x/text/cases"
)

type committed struct {
	seq    int
	counts Counts
	recent []RecentEntry
	err    error
}

var FlushSpacing = 2 * time.Second

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
	byNorm   map[string][]string
	byDir    map[string][]string
	cov      *coverage
	dirs     map[string]*dirPick
	seeded   map[string]string
	indexed  map[string]string
	behind   map[string][]string
	gone     map[string]bool
	trust    bool
	queue    []FSFile
	inflight int
	sets     map[string]*SeriesChanges
	prog     Progress
	seq      int
	tick     int
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
		byNorm:  map[string][]string{},
		byDir:   map[string][]string{},
		dirs:    map[string]*dirPick{},
		seeded:  map[string]string{},
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
		w.addNorm(ref)
		w.dirs[ref.ID] = &dirPick{stored: ref.FileURI}
		if ref.FileURI != nil {
			w.addDir(*ref.FileURI, ref.ID)
		}
	}
	return w
}

func (w *writer) run(ctx context.Context, events <-chan Event, walkDone <-chan error, jobs chan<- FSFile,
	results <-chan Result, flushes chan<- flush, done <-chan committed) error {
	closeJobs := sync.OnceFunc(func() { close(jobs) })
	closeFlushes := sync.OnceFunc(func() { close(flushes) })
	defer closeJobs()
	defer closeFlushes()

	for ev := range events {
		w.event(ev)
		w.publish()
	}
	if err := <-walkDone; err != nil {
		return err
	}
	w.seed()

	w.prog.Phase = "parsing"
	if len(w.queue) == 0 {
		closeJobs()
	}

	tick := time.NewTicker(FlushSpacing)
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
		if !busy && len(w.sets) > 0 && time.Since(w.last) >= FlushSpacing {
			flushes <- w.take(false)
			busy = true
		}
		w.publish()
	}

	w.prog.Phase = "saving"
	w.publish()
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

	parent, ok := w.resolveSeries(r.Item.Series)
	if !ok && r.Item.Series.Inferred {
		slog.Warn("[scanner] inferred series conflicts, keeping the book standalone", "path", r.File.Path,
			"uri_prefix", r.Item.Series.URIPrefix, "uri_part", r.Item.Series.URIPart)
		r.Item.Series, parent, ok = nil, "", true
	}
	if !ok {
		w.prog.Failed++
		slog.Warn("[scanner] series key conflict, skipping", "path", r.File.Path,
			"uri_prefix", r.Item.Series.URIPrefix, "uri_part", r.Item.Series.URIPart)
		w.logf("Series key conflict for file %s, skipping (uri: %s/%s)\n", r.File.Path, r.Item.Series.URIPrefix, r.Item.Series.URIPart)
		return
	}

	own := old.ID
	k := Key{parent, r.Item.URIPart}
	occ := w.keys[k]
	// A book named like a book series joins it instead of conflicting with it.
	if r.Item.Series == nil && r.Item.ContentType == "book" && w.series[occ].Type == "book_series" {
		parent, k = occ, Key{occ, r.Item.URIPart}
		occ = w.keys[k]
		r.Item.OrderParts = []*float32{nil} // a volume inferred for another series means nothing here
	}
	s := w.set(parent)

	var id string
	w.tick++
	switch {
	case occ == "" || occ == own:
		id = cmp.Or(own, models.MakeContentID())
	case w.gone[occ] && own == "":
		id = occ
		delete(w.gone, occ)
	case w.gone[occ]:
		id = own
		delete(w.gone, occ)
		s.Deletes = append(s.Deletes, deletion{occ, w.tick})
	default:
		w.prog.Failed++
		slog.Warn("[scanner] URI conflict, skipping", "path", r.File.Path, "uri_part", r.Item.URIPart, "parent_id", parent)
		w.logf("URI conflict for file %s, skipping (uri_part: %s, parent_id: %s)\n", r.File.Path, r.Item.URIPart, parent)
		return
	}

	if r.Item.Series != nil {
		w.aim(parent, r.Item.Series.FileURI)
	}
	if prev, ok := w.byID[id]; ok {
		if pk := (Key{deref(prev.ParentID), prev.URIPart}); pk != k && w.keys[pk] == id {
			delete(w.keys, pk)
		}
		if oldParent := deref(prev.ParentID); oldParent != parent {
			w.set(oldParent)
			w.unseed(id)
		}
	}
	w.keys[k] = id
	s.Writes = append(s.Writes, write{id: id, item: r.Item, tick: w.tick})
}

func (w *writer) resolveSeries(p *ParsedSeries) (string, bool) {
	if p == nil {
		return "", true
	}

	uri := p.URIPrefix + "/" + p.URIPart
	key := Key{"", p.URIPart}

	id, found := w.byURI[uri]
	if !found && p.FileURI != nil {
		id, found = w.dirSeries(*p.FileURI)
	}
	// A normalized match reuses the series as named: renaming it would flip between spellings.
	if !found && p.ContentType == "book_series" {
		switch ids := w.byNorm[normKey(p.URIPart)]; len(ids) {
		case 0:
		case 1:
			return ids[0], true
		default:
			slog.Warn("[scanner] series name matches several series", "uri", uri, "ids", ids)
			return "", false
		}
	}

	if !found {
		if _, taken := w.keys[key]; taken {
			return "", false
		}
		ref := SeriesRef{ID: models.MakeContentID(), URI: uri, URIPart: p.URIPart, Type: p.ContentType}
		w.series[ref.ID] = ref
		w.byURI[uri] = ref.ID
		w.keys[key] = ref.ID
		w.addNorm(ref)
		w.set(ref.ID).New = true
		return ref.ID, true
	}

	ref := w.series[id]
	s := w.set(id)
	if ref.URI != uri {
		if occ, taken := w.keys[key]; taken && occ != id {
			return "", false
		}
		s.OldURI = cmp.Or(s.OldURI, ref.URI)
		delete(w.byURI, ref.URI)
		if prev := (Key{"", ref.URIPart}); w.keys[prev] == id {
			delete(w.keys, prev)
		}
		ref.URI = uri
		w.byURI[uri] = id
	}
	ref.URIPart = p.URIPart
	w.keys[key] = id
	w.series[id] = ref
	s.Ref = ref
	return id, true
}

// addNorm indexes book series by normalized name, so "Foo’s" finds "Foo's". Book series have no
// directory to be renamed through, and empty series are only deleted at commit, so entries are only added.
func (w *writer) addNorm(ref SeriesRef) {
	if k := normKey(ref.URIPart); ref.Type == "book_series" && k != "" {
		w.byNorm[k] = append(w.byNorm[k], ref.ID)
	}
}

func normKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, cases.Fold().String(s))
}

func (w *writer) seed() {
	for id, f := range w.byID {
		if f.ParentID == nil || w.gone[id] {
			continue
		}
		d, ok := w.dirs[*f.ParentID]
		if !ok || d.stored == nil {
			continue
		}
		dir := filepath.Dir(f.Path)
		d.add(dir)
		w.seeded[id] = dir
	}
	for id := range w.series {
		w.aim(id, nil)
	}
}

func (w *writer) unseed(id string) {
	dir, ok := w.seeded[id]
	if !ok {
		return
	}
	delete(w.seeded, id)
	parent := deref(w.byID[id].ParentID)
	if d, ok := w.dirs[parent]; ok {
		d.drop(dir)
		w.aim(parent, nil)
	}
}

func (w *writer) aim(id string, dir *string) {
	d, ok := w.dirs[id]
	if !ok {
		d = &dirPick{}
		w.dirs[id] = d
	}
	if dir != nil {
		d.add(*dir)
	}
	ref := w.series[id]
	picked := d.pick()
	if ptrEq(ref.FileURI, picked) {
		return
	}
	if ref.FileURI != nil {
		w.dropDir(*ref.FileURI, id)
	}
	if picked != nil {
		w.addDir(*picked, id)
	}
	ref.FileURI = picked
	w.series[id] = ref
	w.set(id).Ref = ref
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
			w.tick++
			s.Deletes = append(s.Deletes, deletion{id, w.tick})
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
	w.prog.Saved.add(c.counts)
	w.prog.Recent = mergeRecent(c.recent, w.prog.Recent)
	w.prog.CommitSeq = c.seq
	if w.notify != nil {
		w.notify.CatalogChanged(CatalogChanged{LibraryID: w.in.LibraryID, TaskID: w.taskID(), CommitSeq: c.seq})
	}
}

// mergeRecent returns a new ring, newest first, where an ID seen again sums its counts at its newest place.
func mergeRecent(fresh, old []RecentEntry) []RecentEntry {
	out := make([]RecentEntry, 0, RecentCap)
	at := map[string]int{}
	for _, e := range slices.Concat(fresh, old) {
		if i, ok := at[e.ID]; ok {
			out[i].add(e.Counts)
		} else if len(out) < RecentCap {
			at[e.ID] = len(out)
			out = append(out, e)
		}
	}
	return out
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
