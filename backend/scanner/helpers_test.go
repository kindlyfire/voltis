package scanner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var baseTime = time.Unix(1700000000, 0).UTC()

func newTestPool(t *testing.T) *pgxpool.Pool {
	return dbtest.Pool(t)
}

func fsFile(path string, mtime time.Time, size int64) FSFile {
	return FSFile{Path: path, Mtime: mtime, Size: size}
}

func rawMeta(m models.Metadata) metaraw.MetadataRaw {
	return metaraw.MetadataRaw{File: &metaraw.RawContainer[models.Metadata]{Raw: m}}
}

func comicItem(dir, number string, meta models.Metadata) Result {
	meta.Number = number
	file := fsFile(dir+"/ch"+number+".cbz", baseTime, 10)
	return Result{File: file, Item: classifyComic(file, meta, 0, testPages)}
}

type recordingQuerier struct {
	db.Querier
	sql []string
}

func (q *recordingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.sql = append(q.sql, sql)
	return q.Querier.Query(ctx, sql, args...)
}

type recordingTx struct {
	pgx.Tx
	queries []recordedQuery
}

type recordedQuery struct {
	sql  string
	args []any
}

func (tx *recordingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, recordedQuery{sql: sql, args: args})
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *recordingTx) idsFor(t *testing.T, fragment string) []string {
	t.Helper()
	for _, q := range tx.queries {
		if !strings.Contains(q.sql, fragment) {
			continue
		}
		for _, arg := range q.args {
			if ids, ok := arg.([]string); ok {
				out := slices.Clone(ids)
				slices.Sort(out)
				return slices.Compact(out)
			}
		}
		t.Fatalf("query %q carries no id list: %v", fragment, q.args)
	}
	return nil
}

func selectedColumns(t *testing.T, sql string) []string {
	t.Helper()
	start, end := strings.Index(sql, "SELECT"), strings.Index(sql, "FROM")
	if start < 0 || end < start {
		t.Fatalf("not a select: %s", sql)
	}
	cols := strings.Split(sql[start+len("SELECT"):end], ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	slices.Sort(cols)
	return cols
}

func newTestLibrary(t *testing.T, pool *pgxpool.Pool, libType string) string {
	t.Helper()
	id := models.MakeLibraryID()
	exec(t, pool, "INSERT INTO libraries (id, name, type, sources) VALUES ($1, $2, $3, '[]')", id, "test", libType)
	return id
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func exec(t *testing.T, q db.Querier, sql string, args ...any) {
	t.Helper()
	if _, err := q.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func seedContent(t *testing.T, pool *pgxpool.Pool, rows ...models.Content) {
	t.Helper()
	err := db.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		for _, c := range rows {
			if c.CreatedAt.IsZero() {
				c.CreatedAt = baseTime
			}
			c.UpdatedAt = c.CreatedAt
			if err := upsertContent(context.Background(), tx, c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed content: %v", err)
	}
}

func seedMetadata(t *testing.T, q db.Querier, libraryID, uri string, mr metaraw.MetadataRaw) {
	t.Helper()
	merged, _ := json.Marshal(mr.Merge())
	exec(t, q, `
		INSERT INTO content_metadata (uri, library_id, data, data_raw, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (uri, library_id) DO UPDATE SET data = EXCLUDED.data, data_raw = EXCLUDED.data_raw
	`, uri, libraryID, merged, mr.Dump())
}

func readContent(t *testing.T, pool *pgxpool.Pool, id string) models.Content {
	t.Helper()
	c, err := db.SelectOne[models.Content](context.Background(), pool, "SELECT * FROM content WHERE id = $1", id)
	if err != nil {
		t.Fatalf("read content %s: %v", id, err)
	}
	return c
}

func readMeta(t *testing.T, pool *pgxpool.Pool, libraryID, uri string) metaraw.MetadataRaw {
	t.Helper()
	var raw json.RawMessage
	err := pool.QueryRow(context.Background(),
		"SELECT data_raw FROM content_metadata WHERE library_id = $1 AND uri = $2", libraryID, uri).Scan(&raw)
	if err != nil {
		t.Fatalf("read metadata %s: %v", uri, err)
	}
	return metaraw.From(raw)
}

func contentIDByURI(t *testing.T, pool *pgxpool.Pool, libraryID, uri string) string {
	t.Helper()
	id, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT id FROM content WHERE library_id = $1 AND uri = $2", libraryID, uri)
	if err != nil {
		t.Fatalf("read id for %s: %v", uri, err)
	}
	return id
}

func contentURIs(t *testing.T, pool *pgxpool.Pool, libraryID string) []string {
	t.Helper()
	uris, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM content WHERE library_id = $1 ORDER BY uri", libraryID)
	if err != nil {
		t.Fatalf("read uris: %v", err)
	}
	return uris
}

func assertCatalog(t *testing.T, pool *pgxpool.Pool, libraryID string, want []string) {
	t.Helper()
	if got := contentURIs(t, pool, libraryID); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
}

func assertSeriesLocation(t *testing.T, pool *pgxpool.Pool, id, dir string) {
	t.Helper()
	got := readContent(t, pool, id)
	if deref(got.FileURI) != dir {
		t.Fatalf("series file_uri = %v, want %s", deref(got.FileURI), dir)
	}
	if cover := filepath.Join(dir, "cover.jpg"); deref(got.CoverURI) != cover {
		t.Fatalf("series cover_uri = %v, want %s", deref(got.CoverURI), cover)
	}
}

func annotationURIs(t *testing.T, pool *pgxpool.Pool, libraryID string) []string {
	t.Helper()
	got, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM user_to_content WHERE library_id = $1 ORDER BY uri", libraryID)
	if err != nil {
		t.Fatalf("read annotations: %v", err)
	}
	return got
}

func assertAnnotations(t *testing.T, pool *pgxpool.Pool, libraryID string, want []string) {
	t.Helper()
	if got := annotationURIs(t, pool, libraryID); !slices.Equal(got, want) {
		t.Fatalf("annotations = %v, want %v", got, want)
	}
}

type scanRun struct {
	t    *testing.T
	pool *pgxpool.Pool
	lib  string
	fs   FileScanner
	w    *writer
}

func newTestScan(t *testing.T, libType string) *scanRun {
	t.Helper()
	pool := newTestPool(t)
	return newScanRun(t, pool, newTestLibrary(t, pool, libType), newFileScanner(ScanInput{LibraryType: libType}))
}

func newScanRun(t *testing.T, pool *pgxpool.Pool, libraryID string, s FileScanner) *scanRun {
	r := &scanRun{t: t, pool: pool, lib: libraryID, fs: s}
	r.reload()
	return r
}

func (r *scanRun) reload() {
	r.t.Helper()
	ctx := context.Background()
	res := newResolver()
	fps, err := loadFingerprints(ctx, r.pool, r.lib)
	if err != nil {
		r.t.Fatalf("load fingerprints: %v", err)
	}
	refs, err := loadSeries(ctx, r.pool, r.lib)
	if err != nil {
		r.t.Fatalf("load series: %v", err)
	}
	r.w = newWriter(ScanInput{LibraryID: r.lib}, nil, nil, res, fps, refs)
	r.w.seed()
}

func (r *scanRun) place(results ...Result) {
	for _, res := range results {
		r.w.place(res)
	}
}

func (r *scanRun) commit(final bool) Counts {
	r.t.Helper()
	_, counts, err := r.recordCommit(final)
	if err != nil {
		r.t.Fatalf("commit: %v", err)
	}
	return counts
}

func (r *scanRun) recordCommit(final bool) (*recordingTx, Counts, error) {
	r.t.Helper()
	f := r.w.take(final)
	rec := &recordingTx{}
	var counts Counts
	err := db.WithTx(context.Background(), r.pool, func(tx pgx.Tx) error {
		rec.Tx = tx
		var txErr error
		counts, txErr = commit(context.Background(), rec, r.fs, r.lib, f, time.Now().UTC())
		return txErr
	})
	return rec, counts, err
}
