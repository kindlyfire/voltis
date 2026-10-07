package linking

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"
	"voltis/providers"

	"github.com/jackc/pgx/v5"
)

// reviewTabs select the links each tab of the review page lists.
// Keep in sync with the query in Summary.
var reviewTabs = map[string]string{
	"review":    "l.state = 'review'",
	"unmatched": "l.state = 'unmatched'",
	"auto":      "l.state = 'linked' AND l.origin = 'auto'",
	"ignored":   "l.state = 'ignored'",
}

type ReviewQuery struct {
	LibraryID string // empty = all
	Tab       string
	Search    string // words of the series' titles
	Failed    bool   // only links whose last attempt failed, or whose snapshot does not decode
	Limit     int
	Offset    int
}

// ReviewItem is a series and one of its links.
type ReviewItem struct {
	Content    models.Content
	Data       json.RawMessage // merged metadata
	LocalTitle string          // the title matching reads
	Link       LinkView
}

type ReviewPage struct {
	Items []ReviewItem
	Total int
}

// Review lists the links of a tab, the most relevant to the search first, then the most recently
// changed.
func (s *Service) Review(ctx context.Context, q ReviewQuery) (ReviewPage, error) {
	cond, ok := reviewTabs[q.Tab]
	if !ok {
		return ReviewPage{}, &metadata.ValidationError{Field: "tab", Msg: fmt.Sprintf("unknown tab %q", q.Tab)}
	}
	search := strings.TrimSpace(q.Search)
	args := pgx.NamedArgs{"library_id": q.LibraryID, "providers": s.providerNames(), "search": search,
		"limit": q.Limit, "offset": q.Offset}
	from := "FROM metadata_links l JOIN content c ON c.id = l.content_id"
	where := "WHERE " + cond + " AND l.provider = ANY(@providers) AND (@library_id = '' OR l.library_id = @library_id)"
	order := "l.updated_at DESC, l.library_id, l.content_id, l.provider"
	count := "FROM metadata_links l"
	if search != "" {
		where += " AND " + metadata.TitleMatch("c", true) // links are on series, which are root-level
		order = metadata.TitleScore("c") + " DESC, " + order
		count = from
	}
	if q.Failed {
		where += " AND l.last_error IS NOT NULL"
	}
	var page ReviewPage
	var err error
	if page.Total, err = db.SelectScalar[int](ctx, s.pool, "SELECT count(*) "+count+" "+where, args); err != nil {
		return page, err
	}

	type row struct {
		Link
		Data      json.RawMessage `db:"data"`
		Doc       metadata.Doc    `db:"doc"`
		EntryID   *string         `db:"entry_id"`
		Raw       json.RawMessage `db:"raw"`
		FetchedAt *time.Time      `db:"fetched_at"`
		Deleted   *bool           `db:"deleted"`
		RefreshAt *time.Time      `db:"refresh_at"`
		Attempts  *int            `db:"entry_attempts"`
		Error     *string         `db:"entry_error"`
	}
	rows, err := db.Select[row](ctx, s.pool, `
		SELECT l.*, c.data, c.data_raw AS doc,
			e.external_id AS entry_id, e.raw, e.fetched_at, e.deleted, e.refresh_at,
			e.attempts AS entry_attempts, e.last_error AS entry_error
		`+from+`
		`+metadata.LinkedEntry("LEFT JOIN")+`
		`+where+`
		ORDER BY `+order+`
		LIMIT @limit OFFSET @offset
	`, args)
	if err != nil {
		return page, err
	}
	contents, err := db.Select[models.Content](ctx, s.pool, "SELECT "+models.ContentColumns("")+" FROM content WHERE id = ANY($1)",
		fp.Map(rows, func(r row) string { return r.ContentID }))
	if err != nil {
		return page, err
	}
	byID := map[string]models.Content{}
	for _, c := range contents {
		byID[c.ID] = c
	}
	finals, err := canonical(ctx, s.pool, slices.Concat(fp.Map(rows, func(r row) []metadata.EntryKey { return candidateKeys(r.Link) })...))
	if err != nil {
		return page, err
	}
	page.Items = []ReviewItem{}
	for _, r := range rows {
		p, _ := s.reg.Get(r.Provider)
		var e *linkedEntry
		if r.State == StateLinked {
			e = &linkedEntry{ID: *r.EntryID, Raw: r.Raw, FetchedAt: *r.FetchedAt, Deleted: *r.Deleted,
				RefreshAt: *r.RefreshAt, Attempts: *r.Attempts, LastError: r.Error}
		}
		page.Items = append(page.Items, ReviewItem{Content: byID[r.ContentID], Data: r.Data,
			LocalTitle: r.Doc.Local().Title.V, Link: linkView(p, r.Link, e, finals)})
	}
	return page, nil
}

// ReviewSummary counts a library's links on each tab of the review page.
type ReviewSummary struct {
	LibraryID string `json:"library_id" db:"library_id"`
	Review    int    `json:"review"     db:"review"`
	Unmatched int    `json:"unmatched"  db:"unmatched"`
	Auto      int    `json:"auto"       db:"auto"`
	Ignored   int    `json:"ignored"    db:"ignored"`
}

// ProviderHealth tells how refreshing a provider's entries goes.
type ProviderHealth struct {
	Provider    string     `json:"provider"     db:"provider"`
	Failing     int        `json:"failing"      db:"failing"`      // fetched entries, not tombstones, whose last refresh failed
	Undecodable int        `json:"undecodable"  db:"undecodable"`  // linked series whose snapshot does not decode
	LastFetched *time.Time `json:"last_fetched" db:"last_fetched"` // the latest snapshot
}

type Summary struct {
	Libraries []ReviewSummary  `json:"libraries"`
	Providers []ProviderHealth `json:"providers"`
	Worker    WorkerStatus     `json:"worker"`
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	out := Summary{Libraries: []ReviewSummary{}}
	libs, err := db.Select[ReviewSummary](ctx, s.pool, `
		SELECT l.library_id, count(*) FILTER (WHERE l.state = 'review') AS review,
			count(*) FILTER (WHERE l.state = 'unmatched') AS unmatched,
			count(*) FILTER (WHERE l.state = 'linked' AND l.origin = 'auto') AS auto,
			count(*) FILTER (WHERE l.state = 'ignored') AS ignored
		FROM metadata_links l WHERE l.provider = ANY($1)
		GROUP BY l.library_id ORDER BY l.library_id
	`, s.providerNames())
	if err != nil {
		return out, err
	}
	out.Libraries = append(out.Libraries, libs...)
	out.Providers, err = db.Select[ProviderHealth](ctx, s.pool, `
		SELECT p AS provider,
			(SELECT count(*) FROM provider_entries WHERE provider = p AND attempts > 0 AND merged_into IS NULL
				AND fetched_at > '-infinity') AS failing,
			(SELECT count(*) FROM metadata_links l WHERE l.provider = p AND l.state = 'linked'
				AND l.last_error IS NOT NULL) AS undecodable,
			(SELECT max(fetched_at) FROM provider_entries WHERE provider = p AND fetched_at > '-infinity') AS last_fetched
		FROM unnest($1::text[]) p
	`, s.providerNames())
	if err != nil {
		return out, err
	}
	// Read after the queries, which narrows the window where a slower HTTP response could
	// overwrite a newer WS push with a stale status.
	out.Worker = s.Status()
	return out, nil
}

type ReviewAction struct {
	ContentID   string   `json:"content_id"`
	Provider    string   `json:"provider"`
	Action      string   `json:"action"`       // link | reject | ignore | undo
	ExternalID  string   `json:"external_id"`  // link
	ExternalIDs []string `json:"external_ids"` // reject
	ExpectRev   *int64   `json:"expect_rev"`   // undo: the revision the decision saved
}

type ReviewResult struct {
	ContentID string `json:"content_id"`
	Provider  string `json:"provider"`
	OK        bool   `json:"ok"`
	Rev       *int64 `json:"rev,omitempty"` // the revision saved, to undo it
	Error     string `json:"error,omitempty"`
}

// ResolveReview applies each item on its own, reporting how each went. The links go first: their
// entries are fetched and published together.
func (s *Service) ResolveReview(ctx context.Context, items []ReviewAction) []ReviewResult {
	revs, errs := make([]int64, len(items)), make([]error, len(items))
	var links []LinkRequest
	var linkItems []int
	for i, it := range items {
		if it.Action == "link" {
			links, linkItems = append(links, LinkRequest{it.ContentID, it.Provider, it.ExternalID, it.ExpectRev}), append(linkItems, i)
		}
	}
	linkRevs, linkErrs := s.LinkAll(ctx, links)
	for k, i := range linkItems {
		revs[i], errs[i] = linkRevs[k], linkErrs[k]
	}
	for i, it := range items {
		switch it.Action {
		case "link":
		case "reject":
			revs[i], errs[i] = s.Reject(ctx, it.ContentID, it.Provider, it.ExternalIDs, it.ExpectRev)
		case "ignore":
			revs[i], errs[i] = s.Ignore(ctx, it.ContentID, it.Provider, it.ExpectRev)
		case "undo":
			if it.ExpectRev == nil {
				errs[i] = &metadata.ValidationError{Field: "expect_rev", Msg: "required"}
			} else {
				errs[i] = s.Undo(ctx, it.ContentID, it.Provider, *it.ExpectRev)
			}
		default:
			errs[i] = &metadata.ValidationError{Field: "action", Msg: fmt.Sprintf("unknown action %q", it.Action)}
		}
	}
	out := make([]ReviewResult, len(items))
	for i, it := range items {
		out[i] = ReviewResult{ContentID: it.ContentID, Provider: it.Provider, OK: errs[i] == nil}
		switch {
		case errs[i] != nil:
			out[i].Error = errs[i].Error()
		case revs[i] != 0:
			out[i].Rev = &revs[i]
		}
	}
	return out
}

func (s *Service) providerNames() []string {
	return fp.Map(s.reg.All(), providers.Provider.Name)
}
