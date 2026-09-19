package scanner

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"
)

func leafFP(id, path, part, parent string) Fingerprint {
	f := Fingerprint{ID: id, Path: path, URIPart: part, Valid: true, Mtime: &baseTime, Size: new(10)}
	if parent != "" {
		f.ParentID = new(parent)
	}
	return f
}

func seriesRefOf(id, part, dir string) SeriesRef {
	ref := SeriesRef{ID: id, URI: "comic/" + part, URIPart: part, Type: "comic_series"}
	if dir != "" {
		ref.FileURI = new(dir)
	}
	return ref
}

func testWriter(fps []Fingerprint, refs []SeriesRef) *writer {
	return newWriter(ScanInput{LibraryID: "library"}, nil, nil, newResolver(), fps, refs)
}

func listedEvent(t *testing.T, w *writer, dir string, names ...string) Event {
	t.Helper()
	return Event{Kind: Listed, Path: dir, Dir: mustResolveDir(t, w.res, dir), Names: names}
}

func comicResult(path, part, series, dir string) Result {
	item := &ParsedItem{
		File:        fsFile(path, baseTime, 10),
		URIPrefix:   "comic",
		ContentType: "comic",
		URIPart:     part,
		OrderParts:  []*float32{new(float32(1))},
	}
	if series != "" {
		item.Series = &ParsedSeries{URIPrefix: "comic", URIPart: series, ContentType: "comic_series", Title: series}
		if dir != "" {
			item.Series.FileURI = new(dir)
		}
	}
	return Result{File: item.File, Item: item}
}

func writeIDs(s *SeriesChanges) []string {
	ids := make([]string, len(s.Writes))
	for i, w := range s.Writes {
		ids[i] = w.id
	}
	return ids
}

func TestWriterPlacesOwnIDAndReclaims(t *testing.T) {
	w := testWriter(
		[]Fingerprint{leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1"), leafFP("l2", "/lib/S/ch2.cbz", "ch2", "p1")},
		[]SeriesRef{seriesRefOf("p1", "S", "/lib/S")},
	)

	w.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	set := w.sets["p1"]
	if !slices.Equal(writeIDs(set), []string{"l1"}) || set.Writes[0].added {
		t.Fatalf("own id: writes = %+v", set.Writes)
	}

	w.gone["l2"] = true
	w.place(comicResult("/lib/S/ch2 (new).cbz", "ch2", "S", "/lib/S"))
	if !slices.Equal(writeIDs(set), []string{"l1", "l2"}) || !set.Writes[1].added {
		t.Fatalf("reclaim: writes = %+v", set.Writes)
	}
	if w.gone["l2"] {
		t.Fatal("reclaimed id must leave gone")
	}
	if w.prog.Failed != 0 {
		t.Fatalf("failed = %d, want 0", w.prog.Failed)
	}
}

func TestWriterReclaimKeepsOwnID(t *testing.T) {
	w := testWriter(
		[]Fingerprint{leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1"), leafFP("l2", "/lib/S/ch2.cbz", "ch2", "p1")},
		[]SeriesRef{seriesRefOf("p1", "S", "/lib/S")},
	)

	w.gone["l1"] = true
	w.place(comicResult("/lib/S/ch2.cbz", "ch1", "S", "/lib/S"))

	set := w.sets["p1"]
	if !slices.Equal(writeIDs(set), []string{"l2"}) || set.Writes[0].added {
		t.Fatalf("writes = %+v", set.Writes)
	}
	if !slices.Equal(set.Deletes, []string{"l1"}) {
		t.Fatalf("deletes = %v, want [l1]", set.Deletes)
	}
	if w.gone["l1"] {
		t.Fatal("displaced id must leave gone")
	}
	if w.keys[Key{"p1", "ch2"}] != "" {
		t.Fatal("previous key of the moved row must be released")
	}
	if w.keys[Key{"p1", "ch1"}] != "l2" {
		t.Fatalf("claimed key = %q", w.keys[Key{"p1", "ch1"}])
	}
}

func TestWriterPlacementConflict(t *testing.T) {
	w := testWriter(
		[]Fingerprint{leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1")},
		[]SeriesRef{seriesRefOf("p1", "S", "/lib/S")},
	)

	w.place(comicResult("/lib/S/other.cbz", "ch1", "S", "/lib/S"))

	if w.prog.Failed != 1 {
		t.Fatalf("failed = %d, want 1", w.prog.Failed)
	}
	if set := w.sets["p1"]; len(set.Writes) != 0 || len(set.Deletes) != 0 {
		t.Fatalf("conflicting placement wrote %+v", set)
	}
	if w.keys[Key{"p1", "ch1"}] != "l1" {
		t.Fatal("key must stay with its holder")
	}
}

func TestWriterDepartureMarksBothParents(t *testing.T) {
	w := testWriter(
		[]Fingerprint{leafFP("l1", "/lib/A/ch1.cbz", "ch1", "p1")},
		[]SeriesRef{seriesRefOf("p1", "A", "/lib/A"), seriesRefOf("p2", "B", "/lib/B")},
	)

	w.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/B"))

	if !slices.Equal(slices.Sorted(maps.Keys(w.sets)), []string{"p1", "p2"}) {
		t.Fatalf("sets = %v, want both parents", slices.Sorted(maps.Keys(w.sets)))
	}
	if len(w.sets["p1"].Writes) != 0 {
		t.Fatalf("old parent = %+v, want no writes", w.sets["p1"])
	}
	if !slices.Equal(writeIDs(w.sets["p2"]), []string{"l1"}) {
		t.Fatalf("new parent writes = %+v", w.sets["p2"].Writes)
	}
	if w.keys[Key{"p1", "ch1"}] != "" || w.keys[Key{"p2", "ch1"}] != "l1" {
		t.Fatalf("keys = %v", w.keys)
	}
}

func TestWriterStandaloneMembership(t *testing.T) {
	w := testWriter([]Fingerprint{leafFP("l1", "/lib/solo.cbz", "solo", "")}, nil)

	w.place(comicResult("/lib/solo.cbz", "solo", "", ""))

	set, ok := w.sets[""]
	if !ok || set.Ref.ID != "" || !slices.Equal(writeIDs(set), []string{"l1"}) {
		t.Fatalf("standalone set = %+v", set)
	}
}

func TestWriterSeriesMatchesURIAndFileURI(t *testing.T) {
	w := testWriter(nil, []SeriesRef{seriesRefOf("p1", "S", "/lib/S")})

	w.place(comicResult("/lib/Moved/ch1.cbz", "ch1", "S", "/lib/Moved"))
	if got := w.series["p1"]; got.URI != "comic/S" || deref(got.FileURI) != "/lib/Moved" {
		t.Fatalf("uri match = %+v, want p1 found by uri alone and moved by its member", got)
	}
	if w.sets["p1"].OldURI != "" || w.sets["p1"].New {
		t.Fatalf("unchanged series recorded a rename: %+v", w.sets["p1"])
	}
	if _, ok := w.dirSeries("/lib/S"); ok {
		t.Fatal("the previous directory must leave the index")
	}

	w.place(comicResult("/lib/Moved/ch2.cbz", "ch2", "S_2019", "/lib/Moved"))
	if got := w.series["p1"]; got.URI != "comic/S_2019" {
		t.Fatalf("file uri match = %+v", got)
	}
	if w.sets["p1"].OldURI != "comic/S" {
		t.Fatalf("old uri = %q", w.sets["p1"].OldURI)
	}
	if w.keys[Key{"", "S"}] != "" || w.keys[Key{"", "S_2019"}] != "p1" {
		t.Fatalf("series keys = %v", w.keys)
	}

	w.place(comicResult("/lib/Moved/ch3.cbz", "ch3", "S_2020", "/lib/Moved"))
	if got := w.series["p1"]; got.URI != "comic/S_2020" {
		t.Fatalf("second rename = %+v", got)
	}
	if w.sets["p1"].OldURI != "comic/S" {
		t.Fatalf("old uri recorded twice: %q", w.sets["p1"].OldURI)
	}
	if w.prog.Failed != 0 || len(w.sets) != 1 {
		t.Fatalf("progress = %+v, sets = %v", w.prog, slices.Sorted(maps.Keys(w.sets)))
	}
}

func TestWriterRenameRecordedOncePerFlush(t *testing.T) {
	w := testWriter(nil, []SeriesRef{seriesRefOf("p1", "S", "/lib/S")})

	w.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	w.place(comicResult("/lib/S/ch2.cbz", "ch2", "S_2019", "/lib/S"))
	if w.sets["p1"].OldURI != "comic/S" {
		t.Fatalf("old uri = %q", w.sets["p1"].OldURI)
	}

	f := w.take(false)
	if f.sets["p1"].OldURI != "comic/S" {
		t.Fatalf("flushed old uri = %q", f.sets["p1"].OldURI)
	}

	w.place(comicResult("/lib/S/ch3.cbz", "ch3", "S_2019", "/lib/S"))
	if w.sets["p1"].OldURI != "" {
		t.Fatalf("rename repeated in the next flush: %q", w.sets["p1"].OldURI)
	}
}

func TestWriterSeriesKeyHeldByLeaf(t *testing.T) {
	w := testWriter([]Fingerprint{leafFP("l1", "/lib/Foo.cbz", "Foo", "")}, nil)

	if _, ok := w.resolveSeries(&ParsedSeries{URIPrefix: "comic", URIPart: "Foo", ContentType: "comic_series"}); ok {
		t.Fatal("a standalone leaf must block the series key")
	}
	if len(w.sets) != 0 {
		t.Fatalf("sets = %v, want none", w.sets)
	}

	w.place(comicResult("/lib/Foo/ch1.cbz", "ch1", "Foo", "/lib/Foo"))
	if w.prog.Failed != 1 {
		t.Fatalf("failed = %d, want 1", w.prog.Failed)
	}
}

func TestWriterNewSeries(t *testing.T) {
	w := testWriter(nil, nil)

	w.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))

	id := w.byURI["comic/S"]
	if id == "" {
		t.Fatal("new series was not indexed")
	}
	set := w.sets[id]
	if !set.New || set.Ref.URI != "comic/S" || set.Ref.Type != "comic_series" {
		t.Fatalf("set = %+v", set)
	}
	if !set.Writes[0].added || w.keys[Key{"", "S"}] != id {
		t.Fatalf("writes = %+v keys = %v", set.Writes, w.keys)
	}
}

func TestWriterInvalidatesUnparsableFile(t *testing.T) {
	w := testWriter([]Fingerprint{leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1")},
		[]SeriesRef{seriesRefOf("p1", "S", "/lib/S")})

	w.place(Result{File: fsFile("/lib/S/ch1.cbz", baseTime, 10)})
	w.place(Result{File: fsFile("/lib/S/unknown.cbz", baseTime, 10)})

	if w.prog.Failed != 2 || w.prog.Processed != 2 {
		t.Fatalf("progress = %+v", w.prog)
	}
	if !slices.Equal(w.sets["p1"].Invalid, []string{"l1"}) {
		t.Fatalf("invalid = %v", w.sets["p1"].Invalid)
	}
	if len(w.sets) != 1 {
		t.Fatalf("sets = %v, want only the known parent", slices.Sorted(maps.Keys(w.sets)))
	}
}

func TestWriterEventsQueueChangedFiles(t *testing.T) {
	w := testWriter([]Fingerprint{
		leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1"),
		leafFP("l2", "/lib/S/ch2.cbz", "ch2", "p1"),
	}, []SeriesRef{seriesRefOf("p1", "S", "/lib/S")})

	w.event(listedEvent(t, w, "/lib/S", "ch1.cbz"))
	w.event(Event{Kind: Seen, File: fsFile("/lib/S/ch1.cbz", baseTime, 10)})
	w.event(Event{Kind: Seen, File: fsFile("/lib/S/ch3.cbz", baseTime, 10)})

	if !w.gone["l2"] || len(w.gone) != 1 {
		t.Fatalf("gone = %v", w.gone)
	}
	if w.prog.Found != 2 || w.prog.Total != 1 || w.prog.Unchanged != 1 {
		t.Fatalf("progress = %+v", w.prog)
	}
	if len(w.queue) != 1 || w.queue[0].Path != "/lib/S/ch3.cbz" {
		t.Fatalf("queue = %+v", w.queue)
	}
}

func TestWriterTakeMovesGoneIntoDeletes(t *testing.T) {
	w := testWriter([]Fingerprint{
		leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1"),
		leafFP("l2", "/lib/solo.cbz", "solo", ""),
	}, []SeriesRef{seriesRefOf("p1", "S", "/lib/S")})

	w.gone["l1"] = true
	w.gone["l2"] = true

	f := w.take(false)
	if len(f.sets) != 0 {
		t.Fatalf("non-final sets = %v", f.sets)
	}
	if !maps.Equal(f.gone, map[string]bool{"l1": true, "l2": true}) {
		t.Fatalf("cloned gone = %v", f.gone)
	}
	f.gone["l1"] = false
	if !w.gone["l1"] {
		t.Fatal("flush must carry a clone of gone")
	}
	if f.seq != 1 || f.final {
		t.Fatalf("flush = %+v", f)
	}

	final := w.take(true)
	if final.gone != nil || final.seq != 2 || !final.final {
		t.Fatalf("final flush = %+v", final)
	}
	if !slices.Equal(final.sets["p1"].Deletes, []string{"l1"}) {
		t.Fatalf("series deletes = %v", final.sets["p1"].Deletes)
	}
	if !slices.Equal(final.sets[""].Deletes, []string{"l2"}) {
		t.Fatalf("standalone deletes = %v", final.sets[""].Deletes)
	}
	if w.gone != nil {
		t.Fatalf("gone = %v, want nil after the final take", w.gone)
	}
}

func TestWriterArrivalPermutations(t *testing.T) {
	results := []Result{
		comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"),
		comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"),
		comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"),
	}
	orders := [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {2, 0, 1}}
	want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2", "comic/T", "comic/T/ch1"}

	for _, idx := range orders {
		w := testWriter(nil, nil)
		for _, j := range idx {
			w.place(results[j])
		}
		var got []string
		for _, id := range slices.Sorted(maps.Keys(w.sets)) {
			set := w.sets[id]
			got = append(got, set.Ref.URI)
			for _, wr := range set.Writes {
				got = append(got, set.Ref.URI+"/"+wr.item.URIPart)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("order %v = %v, want %v", idx, got, want)
		}
		if w.prog.Failed != 0 {
			t.Fatalf("order %v failed %d placements", idx, w.prog.Failed)
		}
	}
}

const rigDeadline = 5 * time.Second

func rigSend[T any](t *testing.T, ch chan T, v T, what string) {
	t.Helper()
	select {
	case ch <- v:
	case <-time.After(rigDeadline):
		t.Fatalf("timed out sending %s", what)
	}
}

func rigRecv[T any](t *testing.T, ch chan T, what string) (T, bool) {
	t.Helper()
	select {
	case v, open := <-ch:
		return v, open
	case <-time.After(rigDeadline):
		var zero T
		t.Fatalf("timed out receiving %s", what)
		return zero, false
	}
}

type writerRig struct {
	t        *testing.T
	w        *writer
	ctx      context.Context
	cancel   context.CancelFunc
	joined   bool
	events   chan Event
	walkDone chan error
	jobs     chan FSFile
	results  chan Result
	flushes  chan flush
	done     chan committed
	err      chan error
}

func newRig(t *testing.T, w *writer) *writerRig {
	ctx, cancel := context.WithCancel(context.Background())
	r := &writerRig{
		t:        t,
		w:        w,
		ctx:      ctx,
		cancel:   cancel,
		events:   make(chan Event, 16),
		walkDone: make(chan error, 1),
		jobs:     make(chan FSFile, 4),
		results:  make(chan Result, 4),
		flushes:  make(chan flush, 1),
		done:     make(chan committed, 1),
		err:      make(chan error, 1),
	}
	t.Cleanup(r.stop)
	return r
}

func (r *writerRig) stop() {
	r.cancel()
	if r.joined {
		return
	}
	r.joined = true
	select {
	case <-r.err:
	case <-time.After(rigDeadline):
		r.t.Error("timed out joining the writer")
	}
}

func fastFlushes(t *testing.T) time.Duration {
	prev := FlushSpacing
	FlushSpacing = 150 * time.Millisecond
	t.Cleanup(func() { FlushSpacing = prev })
	return FlushSpacing
}

func (r *writerRig) start() {
	go func() { r.err <- r.w.run(r.ctx, r.events, r.walkDone, r.jobs, r.results, r.flushes, r.done) }()
}

func (r *writerRig) walkOver() {
	close(r.events)
	r.walkDone <- nil
}

func (r *writerRig) seen(file FSFile) {
	r.t.Helper()
	rigSend(r.t, r.events, Event{Kind: Seen, File: file}, "a seen event")
}

func (r *writerRig) job() FSFile {
	r.t.Helper()
	f, open := rigRecv(r.t, r.jobs, "a job")
	if !open {
		r.t.Fatal("jobs closed before the expected job")
	}
	return f
}

func (r *writerRig) jobsClosed() {
	r.t.Helper()
	if _, open := rigRecv(r.t, r.jobs, "the jobs channel to close"); open {
		r.t.Fatal("jobs must be closed")
	}
}

func (r *writerRig) result(res Result) {
	r.t.Helper()
	rigSend(r.t, r.results, res, "a result")
}

func (r *writerRig) commit(c committed) {
	r.t.Helper()
	rigSend(r.t, r.done, c, "a commit acknowledgement")
}

func (r *writerRig) takeFlush(t *testing.T) flush {
	t.Helper()
	f, open := rigRecv(t, r.flushes, "a flush")
	if !open {
		t.Fatal("flushes closed before the expected flush")
	}
	return f
}

func (r *writerRig) finish(t *testing.T) error {
	t.Helper()
	r.joined = true
	err, _ := rigRecv(t, r.err, "the writer")
	return err
}

func TestWriterRunEmptyScanStillFlushes(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()
	rig.walkOver()

	f := rig.takeFlush(t)
	if !f.final || len(f.sets) != 0 || f.seq != 1 {
		t.Fatalf("flush = %+v", f)
	}
	rig.jobsClosed()
	rig.commit(committed{seq: f.seq, counts: Counts{Removed: 2}})

	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if w.prog.Phase != "done" || w.prog.CommitSeq != 1 || w.prog.Saved.Removed != 2 {
		t.Fatalf("progress = %+v", w.prog)
	}
	if _, open := rigRecv(t, rig.flushes, "the flushes channel to close"); open {
		t.Fatal("flushes must be closed")
	}
}

func TestWriterRunDrainsOutstandingResultsAndCommits(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()

	rig.seen(fsFile("/lib/S/ch1.cbz", baseTime, 10))
	rig.walkOver()

	job := rig.job()
	if job.Path != "/lib/S/ch1.cbz" {
		t.Fatalf("job = %+v", job)
	}
	rig.jobsClosed()
	rig.result(comicResult(job.Path, "ch1", "S", "/lib/S"))

	first := rig.takeFlush(t)
	if first.final || len(first.sets) != 1 {
		t.Fatalf("first flush = %+v", first)
	}
	rig.commit(committed{seq: first.seq, counts: Counts{Added: 1}})

	final := rig.takeFlush(t)
	if !final.final || len(final.sets) != 0 {
		t.Fatalf("final flush = %+v", final)
	}
	rig.commit(committed{seq: final.seq})

	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if w.prog.Saved.Added != 1 || w.prog.Processed != 1 || w.prog.CommitSeq != final.seq {
		t.Fatalf("progress = %+v", w.prog)
	}
}

func TestWriterRunPropagatesCommitError(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()
	rig.walkOver()

	f := rig.takeFlush(t)
	boom := errors.New("boom")
	rig.commit(committed{seq: f.seq, err: boom})

	if err := rig.finish(t); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestWriterRunCancellationDuringFinalCommit(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()
	rig.walkOver()

	rig.takeFlush(t)
	rig.cancel()

	if err := rig.finish(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if w.prog.Phase != "saving" {
		t.Fatalf("phase = %q", w.prog.Phase)
	}
}

func TestWriterRunSpacesIntermediateFlushesOnly(t *testing.T) {
	if FlushSpacing != 5*time.Second {
		t.Fatalf("FlushSpacing = %v, want 5s by default", FlushSpacing)
	}
	spacing := fastFlushes(t)

	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()
	names := []string{"ch1", "ch2", "ch3"}
	for _, name := range names {
		rig.seen(fsFile("/lib/S/"+name+".cbz", baseTime, 10))
	}
	rig.walkOver()

	jobs := []FSFile{rig.job(), rig.job(), rig.job()}

	var written []string
	var prev flush
	var final flush
	for i := 0; !final.final; i++ {
		if i < len(names) {
			rig.result(comicResult(jobs[i].Path, names[i], "S", "/lib/S"))
		}
		f := rig.takeFlush(t)
		if gap := f.at.Sub(prev.at); prev.seq != 0 && !f.final && gap < spacing {
			t.Fatalf("flush %d taken %v after flush %d, want at least %v", f.seq, gap, prev.seq, spacing)
		}
		for _, s := range f.sets {
			for _, wr := range s.Writes {
				written = append(written, wr.item.URIPart)
			}
		}
		rig.commit(committed{seq: f.seq})
		prev, final = f, f
	}

	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	slices.Sort(written)
	if !slices.Equal(written, names) {
		t.Fatalf("writes = %v, want %v across the spaced flushes", written, names)
	}
	if w.prog.Processed != 3 || w.prog.CommitSeq != final.seq {
		t.Fatalf("progress = %+v", w.prog)
	}
}

func TestWriterRunDoesNotSpaceTheFinalFlush(t *testing.T) {
	prev := FlushSpacing
	FlushSpacing = 3 * time.Second
	t.Cleanup(func() { FlushSpacing = prev })

	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.start()
	rig.seen(fsFile("/lib/S/ch1.cbz", baseTime, 10))
	rig.walkOver()

	job := rig.job()
	rig.result(comicResult(job.Path, "ch1", "S", "/lib/S"))

	first := rig.takeFlush(t)
	if first.final || len(first.sets) != 1 {
		t.Fatalf("first flush = %+v", first)
	}
	rig.commit(committed{seq: first.seq, counts: Counts{Added: 1}})

	start := time.Now()
	final := rig.takeFlush(t)
	waited := time.Since(start)
	if !final.final || len(final.sets) != 0 || final.seq != first.seq+1 {
		t.Fatalf("final flush = %+v", final)
	}
	if waited >= FlushSpacing {
		t.Fatalf("final flush waited %v after the last commit, want parsing to end the scan", waited)
	}
	rig.commit(committed{seq: final.seq})

	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, open := rigRecv(t, rig.flushes, "the flushes channel to close"); open {
		t.Fatal("flushes must be closed after the final one")
	}
	if w.prog.Phase != "done" || w.prog.CommitSeq != final.seq || w.prog.Saved.Added != 1 {
		t.Fatalf("progress = %+v", w.prog)
	}
}

func TestWriterDirectoryKeepsSmallestSeriesID(t *testing.T) {
	sharedDir := func(t *testing.T, w *writer, want string) {
		t.Helper()
		w.place(comicResult("/lib/D/ch2.cbz", "ch2", "C", "/lib/D"))
		got := w.sets[want]
		if got == nil || len(got.Writes) != 1 || got.Ref.URI != "comic/C" {
			t.Fatalf("directory match = %v, want it under %s", slices.Sorted(maps.Keys(w.sets)), want)
		}
		if got.New || w.prog.Failed != 0 {
			t.Fatalf("set = %+v, progress = %+v, want an existing series reused", got, w.prog)
		}
	}

	t.Run("uri match does not steal the directory", func(t *testing.T) {
		w := testWriter(nil, []SeriesRef{seriesRefOf("s1", "A", "/lib/D"), seriesRefOf("s2", "B", "/lib/D")})

		w.place(comicResult("/lib/D/ch1.cbz", "ch1", "B", "/lib/D"))
		if len(w.sets["s2"].Writes) != 1 || deref(w.series["s2"].FileURI) != "/lib/D" {
			t.Fatalf("uri match = %+v", w.sets["s2"])
		}
		sharedDir(t, w, "s1")
	})

	t.Run("departure restores the next smallest", func(t *testing.T) {
		w := testWriter(nil, []SeriesRef{seriesRefOf("s1", "A", "/lib/D"), seriesRefOf("s2", "B", "/lib/D")})

		w.place(comicResult("/lib/E/ch1.cbz", "ch1", "A", "/lib/E"))
		if deref(w.series["s1"].FileURI) != "/lib/E" {
			t.Fatalf("moved series = %+v", w.series["s1"])
		}
		sharedDir(t, w, "s2")
	})
}

func TestWriterRejectedPlacementContributesNoDirectory(t *testing.T) {
	w := testWriter(
		[]Fingerprint{leafFP("l1", "/lib/Z/ch1.cbz", "ch1", "p1"), leafFP("l2", "/lib/M/other.cbz", "other", "p2")},
		[]SeriesRef{seriesRefOf("p1", "S", "/lib/A"), seriesRefOf("p2", "T", "/lib/M")},
	)
	w.seed()
	if deref(w.series["p1"].FileURI) != "/lib/Z" {
		t.Fatalf("seeded series = %+v, want its only member directory", w.series["p1"])
	}

	w.place(comicResult("/lib/M/other.cbz", "ch1", "S", "/lib/M"))

	if w.prog.Failed != 1 {
		t.Fatalf("progress = %+v, want the uri conflict rejected", w.prog)
	}
	if got := deref(w.series["p1"].FileURI); got != "/lib/Z" {
		t.Fatalf("series file_uri = %v, want the rejected placement to contribute nothing", got)
	}
	if id, ok := w.dirSeries("/lib/M"); !ok || id != "p2" {
		t.Fatalf("directory index for /lib/M = %q, %v, want only its own series", id, ok)
	}
}

func TestWriterRunFailsWithWorkStillOutstanding(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.jobs = make(chan FSFile)
	rig.start()
	for _, name := range []string{"ch1", "ch2", "ch3"} {
		rig.seen(fsFile("/lib/S/"+name+".cbz", baseTime, 10))
	}
	rig.walkOver()

	job := rig.job()
	rig.result(comicResult(job.Path, "ch1", "S", "/lib/S"))
	f := rig.takeFlush(t)
	boom := errors.New("boom")
	rig.commit(committed{seq: f.seq, err: boom})

	if err := rig.finish(t); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(w.queue) == 0 {
		t.Fatal("the queue must still hold work when the committer fails")
	}
}

func TestWriterRunKeepsPlacingWhileACommitIsOutstanding(t *testing.T) {
	fastFlushes(t)
	w := testWriter(nil, nil)
	rig := newRig(t, w)
	rig.results = make(chan Result)
	rig.start()
	for _, name := range []string{"ch1", "ch2", "ch3"} {
		rig.seen(fsFile("/lib/S/"+name+".cbz", baseTime, 10))
	}
	rig.walkOver()

	jobs := []FSFile{rig.job(), rig.job(), rig.job()}
	rig.result(comicResult(jobs[0].Path, "ch1", "S", "/lib/S"))
	first := rig.takeFlush(t)

	rig.result(comicResult(jobs[1].Path, "ch2", "S", "/lib/S"))
	rig.result(comicResult(jobs[2].Path, "ch3", "S", "/lib/S"))
	rig.commit(committed{seq: first.seq, counts: Counts{Added: 1}})

	placed := 0
	for {
		f := rig.takeFlush(t)
		for _, set := range f.sets {
			placed += len(set.Writes)
		}
		rig.commit(committed{seq: f.seq})
		if f.final {
			break
		}
	}
	if placed != 2 {
		t.Fatalf("placed %d writes while the commit was outstanding, want 2", placed)
	}
	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if w.prog.Processed != 3 {
		t.Fatalf("progress = %+v", w.prog)
	}
}
