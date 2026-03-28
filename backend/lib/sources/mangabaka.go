package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"voltis/config"
	"voltis/models/contentmeta"
)

const (
	defaultBaseURL = "https://api.mangabaka.dev"
	defaultTimeout = 15 * time.Second

	maxRetries    = 3
	retryMinDelay = 1 * time.Second
	retryMaxDelay = 10 * time.Second
)

type SeriesType = string

const (
	SeriesTypeManga  SeriesType = "manga"
	SeriesTypeNovel  SeriesType = "novel"
	SeriesTypeManhwa SeriesType = "manhwa"
	SeriesTypeManhua SeriesType = "manhua"
	SeriesTypeOEL    SeriesType = "oel"
	SeriesTypeOther  SeriesType = "other"
)

type SeriesStatus = string

const (
	SeriesStatusCancelled SeriesStatus = "cancelled"
	SeriesStatusCompleted SeriesStatus = "completed"
	SeriesStatusHiatus    SeriesStatus = "hiatus"
	SeriesStatusReleasing SeriesStatus = "releasing"
	SeriesStatusUnknown   SeriesStatus = "unknown"
	SeriesStatusUpcoming  SeriesStatus = "upcoming"
)

type ContentRating = string

const (
	ContentRatingSafe         ContentRating = "safe"
	ContentRatingSuggestive   ContentRating = "suggestive"
	ContentRatingErotica      ContentRating = "erotica"
	ContentRatingPornographic ContentRating = "pornographic"
)

type SeriesState = string

const (
	SeriesStateActive  SeriesState = "active"
	SeriesStateMerged  SeriesState = "merged"
	SeriesStateDeleted SeriesState = "deleted"
)

type CoverRaw struct {
	URL       *string `json:"url"`
	Size      *int    `json:"size,omitempty"`
	Height    *int    `json:"height,omitempty"`
	Width     *int    `json:"width,omitempty"`
	Blurhash  *string `json:"blurhash,omitempty"`
	Thumbhash *string `json:"thumbhash,omitempty"`
	Format    *string `json:"format,omitempty"`
}

type CoverScaled struct {
	X1 *string `json:"x1"`
	X2 *string `json:"x2"`
	X3 *string `json:"x3"`
}

type Cover struct {
	Raw  CoverRaw    `json:"raw"`
	X150 CoverScaled `json:"x150"`
	X250 CoverScaled `json:"x250"`
	X350 CoverScaled `json:"x350"`
}

type Publisher struct {
	Name *string `json:"name"`
	Type *string `json:"type"`
	Note *string `json:"note"`
}

type AnimeInfo struct {
	Start *string `json:"start"`
	End   *string `json:"end"`
}

type SecondaryTitle struct {
	Type  string  `json:"type"`
	Title string  `json:"title"`
	Note  *string `json:"note,omitempty"`
}

type TagV2 struct {
	ID            int            `json:"id"`
	ParentID      *int           `json:"parent_id"`
	Name          string         `json:"name"`
	NamePath      string         `json:"name_path"`
	Description   *string        `json:"description,omitempty"`
	IsSpoiler     *bool          `json:"is_spoiler,omitempty"`
	ContentRating *ContentRating `json:"content_rating,omitempty"`
	SeriesCount   *int           `json:"series_count,omitempty"`
	Level         *int           `json:"level,omitempty"`
}

type SourceEntry struct {
	ID               json.RawMessage `json:"id,omitempty"`
	Rating           *float64        `json:"rating,omitempty"`
	RatingNormalized *float64        `json:"rating_normalized,omitempty"`
}

type Relationships struct {
	MainStory   []int `json:"main_story,omitempty"`
	Adaptation  []int `json:"adaptation,omitempty"`
	Prequel     []int `json:"prequel,omitempty"`
	Sequel      []int `json:"sequel,omitempty"`
	SideStory   []int `json:"side_story,omitempty"`
	SpinOff     []int `json:"spin_off,omitempty"`
	Alternative []int `json:"alternative,omitempty"`
	Other       []int `json:"other,omitempty"`
}

type Series struct {
	ID              int                         `json:"id"`
	State           SeriesState                 `json:"state,omitempty"`
	MergedWith      *int                        `json:"merged_with,omitempty"`
	Title           string                      `json:"title"`
	NativeTitle     *string                     `json:"native_title,omitempty"`
	RomanizedTitle  *string                     `json:"romanized_title,omitempty"`
	SecondaryTitles map[string][]SecondaryTitle `json:"secondary_titles,omitempty"`
	Cover           Cover                       `json:"cover"`
	Authors         []string                    `json:"authors,omitempty"`
	Artists         []string                    `json:"artists,omitempty"`
	Description     *string                     `json:"description,omitempty"`
	Year            *int                        `json:"year,omitempty"`
	Status          SeriesStatus                `json:"status,omitempty"`
	IsLicensed      bool                        `json:"is_licensed,omitempty"`
	HasAnime        bool                        `json:"has_anime,omitempty"`
	Anime           *AnimeInfo                  `json:"anime,omitempty"`
	ContentRating   ContentRating               `json:"content_rating,omitempty"`
	Type            SeriesType                  `json:"type,omitempty"`
	Rating          *float64                    `json:"rating,omitempty"`
	FinalVolume     *string                     `json:"final_volume,omitempty"`
	TotalChapters   *string                     `json:"total_chapters,omitempty"`
	Links           []string                    `json:"links,omitempty"`
	Publishers      []Publisher                 `json:"publishers,omitempty"`
	Genres          []string                    `json:"genres"`
	GenresV2        []TagV2                     `json:"genres_v2,omitempty"`
	Tags            []string                    `json:"tags,omitempty"`
	TagsV2          []TagV2                     `json:"tags_v2,omitempty"`
	LastUpdatedAt   *string                     `json:"last_updated_at,omitempty"`
	Relationships   *Relationships              `json:"relationships,omitempty"`
	Source          map[string]SourceEntry      `json:"source,omitempty"`
}

type Pagination struct {
	Count    int     `json:"count"`
	Page     int     `json:"page"`
	Limit    int     `json:"limit"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
}

type seriesGetResponse struct {
	Status int    `json:"status"`
	Data   Series `json:"data"`
}

type SeriesSearchResponse struct {
	Status     int        `json:"status"`
	Pagination Pagination `json:"pagination"`
	Data       []Series   `json:"data"`
}

// Client

type MangaBaka struct {
	baseURL string
	client  *http.Client
}

func NewMangaBaka() *MangaBaka {
	return &MangaBaka{
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: defaultTimeout},
	}
}

var retryableStatuses = map[int]bool{
	429: true,
	500: true,
	502: true,
	503: true,
	504: true,
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("mangabaka: HTTP %d: %s", e.StatusCode, e.Body)
}

func isRetryable(err error) bool {
	if ae, ok := errors.AsType[*apiError](err); ok {
		return retryableStatuses[ae.StatusCode]
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

func (m *MangaBaka) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * retryMinDelay
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
			slog.Warn("mangabaka: retrying request", "attempt", attempt+1, "delay", delay, "err", lastErr)
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
		}

		resp, err := m.client.Do(req.WithContext(ctx))
		if err != nil {
			lastErr = err
			if isRetryable(err) {
				continue
			}
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Read body for error context, then close
		var buf [512]byte
		n, _ := resp.Body.Read(buf[:])
		_ = resp.Body.Close()

		lastErr = &apiError{StatusCode: resp.StatusCode, Body: string(buf[:n])}
		if isRetryable(lastErr) {
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func get[T any](m *MangaBaka, ctx context.Context, path string, query url.Values) (*T, error) {
	u := m.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Voltis/"+config.AppVersion)

	resp, err := m.doWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var body T
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("mangabaka: decode %s: %w", path, err)
	}
	return &body, nil
}

// SeriesGet fetches a series by ID. If the series has been merged, it follows
// the redirect to the merged target.
func (m *MangaBaka) SeriesGet(ctx context.Context, id int) (*Series, error) {
	body, err := get[seriesGetResponse](m, ctx, fmt.Sprintf("/v1/series/%d", id), nil)
	if err != nil {
		return nil, err
	}

	s := &body.Data
	if s.State == SeriesStateMerged && s.MergedWith != nil && *s.MergedWith != 0 {
		return m.SeriesGet(ctx, *s.MergedWith)
	}
	return s, nil
}

type SeriesSearchOpts struct {
	Query string
	Type  []SeriesType
}

// SeriesSearch searches for series matching the given options.
func (m *MangaBaka) SeriesSearch(ctx context.Context, opts SeriesSearchOpts) (*SeriesSearchResponse, error) {
	query := url.Values{}
	if opts.Query != "" {
		query.Set("q", opts.Query)
	}
	if len(opts.Type) > 0 {
		for i := range opts.Type {
			query.Add("type", opts.Type[i])
		}
	}
	return get[SeriesSearchResponse](m, ctx, "/v1/series/search", query)
}

// MangaBakaSeriesToMetadata converts a MangaBaka Series into a Metadata struct.
func MangaBakaSeriesToMetadata(s *Series) contentmeta.Metadata {
	meta := contentmeta.Metadata{
		Title:       s.Title,
		MangaBakaID: &s.ID,
	}
	if s.Description != nil {
		meta.Description = *s.Description
	}

	var staff []contentmeta.StaffEntry
	for _, a := range s.Authors {
		staff = append(staff, contentmeta.StaffEntry{Name: a, Role: "author"})
	}
	for _, a := range s.Artists {
		staff = append(staff, contentmeta.StaffEntry{Name: a, Role: "artist"})
	}
	meta.Staff = staff

	for _, p := range s.Publishers {
		if p.Name != nil && *p.Name != "" {
			meta.Publisher = *p.Name
			break
		}
	}

	if s.Year != nil {
		meta.PublicationDate = strconv.Itoa(*s.Year)
	}
	if len(s.Genres) > 0 {
		meta.Genre = strings.Join(s.Genres, ", ")
	}
	if s.ContentRating != "" {
		meta.AgeRating = string(s.ContentRating)
	}

	switch s.Type {
	case SeriesTypeManga, SeriesTypeManhwa, SeriesTypeManhua:
		meta.Manga = "Yes"
	case SeriesTypeNovel, SeriesTypeOEL:
		meta.Manga = "No"
	}

	return meta
}
