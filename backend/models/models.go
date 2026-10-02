package models

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/rand"
	"path/filepath"
	"strings"
	"time"
)

type JSONB = json.RawMessage

type User struct {
	ID           string    `db:"id" json:"id"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
	Username     string    `db:"username" json:"username"`
	PasswordHash *string   `db:"password_hash" json:"-"`
	Email        *string   `db:"email" json:"email"`
	Permissions  []string  `db:"permissions" json:"permissions"`
	Preferences  JSONB     `db:"preferences" json:"preferences"`
}

const (
	SessionPassword = "password"
	SessionOIDC     = "oidc"
	SessionProxy    = "proxy"
)

type Session struct {
	Token             string     `db:"token"`
	UserID            string     `db:"user_id"`
	ExpiresAt         time.Time  `db:"expires_at"`
	Method            string     `db:"method"`
	AbsoluteExpiresAt *time.Time `db:"absolute_expires_at"`
}

// AppKey is a per-user credential carried in the URL path by key-authenticated protocols (OPDS).
type AppKey struct {
	ID         string     `db:"id"`
	UserID     string     `db:"user_id"`
	Name       string     `db:"name"`
	Key        string     `db:"key"`
	CreatedAt  time.Time  `db:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at"`
}

type AuthPending struct {
	ID           string    `db:"id"`
	Kind         string    `db:"kind"`
	Data         JSONB     `db:"data"`
	UserID       *string   `db:"user_id"`
	SessionToken *string   `db:"session_token"`
	ExpiresAt    time.Time `db:"expires_at"`
	Attempts     int       `db:"attempts"`
}

type UserIdentity struct {
	ID        string    `db:"id" json:"id"`
	Provider  string    `db:"provider" json:"provider"`
	Issuer    string    `db:"issuer" json:"issuer"`
	Subject   string    `db:"subject" json:"subject"`
	UserID    string    `db:"user_id" json:"user_id"`
	Email     *string   `db:"email" json:"email"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Library struct {
	ID        string     `db:"id" json:"id"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
	Name      string     `db:"name" json:"name"`
	Type      string     `db:"type" json:"type"`
	ScannedAt *time.Time `db:"scanned_at" json:"scanned_at"`
	Sources   JSONB      `db:"sources" json:"sources"`
	Settings  JSONB      `db:"settings" json:"settings"`
}

const (
	BookSeriesInferenceOff          = "off"
	BookSeriesInferenceConservative = "conservative"
)

type LibrarySettings struct {
	BookSeriesInference string   `json:"book_series_inference"`
	AutoMatch           Switches `json:"auto_match"`            // match series with each provider unattended; a missing one is off
	AlwaysRemoveMissing bool     `json:"always_remove_missing"` // turns off the scanner's removal guard
}

// SourceSettings overlay a library's settings for the files under a source: a missing key inherits.
type SourceSettings struct {
	AutoMatch Switches `json:"auto_match,omitempty"`
}

// Switches are on or off by key. A null switch is refused, rather than read as off.
type Switches map[string]bool

func (s *Switches) UnmarshalJSON(b []byte) error {
	var m map[string]*bool
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		*s = nil
		return err
	}
	*s = make(Switches, len(m))
	for k, v := range m {
		if v == nil {
			return fmt.Errorf("%s: null is not on or off", k)
		}
		(*s)[k] = *v
	}
	return nil
}

type LibrarySource struct {
	PathURI  string         `json:"path_uri"`
	Settings SourceSettings `json:"settings"`
}

// Resolve is the settings of a file under the source. Adding an overridable setting adds a
// SourceSettings field and its line here.
func (l LibrarySettings) Resolve(s SourceSettings) LibrarySettings {
	l.AutoMatch = overlay(l.AutoMatch, s.AutoMatch)
	return l
}

func overlay[K comparable, V any](base, over map[K]V) map[K]V {
	out := maps.Clone(base)
	if out == nil {
		out = map[K]V{}
	}
	maps.Copy(out, over)
	return out
}

// Prefix is what the file_uri of every file under the source starts with, as the scanner stores
// it. A "." source has none: its files are stored without a prefix.
func (s LibrarySource) Prefix() (string, bool) {
	p := filepath.Clean(s.PathURI)
	if p == "." {
		return "", false
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p, true
}

// ParseLibrarySources reads the stored sources; none is an empty list.
func ParseLibrarySources(raw JSONB) []LibrarySource {
	var out []LibrarySource
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []LibrarySource{}
	}
	return out
}

func DefaultLibrarySettings() LibrarySettings {
	return LibrarySettings{BookSeriesInference: BookSeriesInferenceConservative, AutoMatch: Switches{}}
}

// ParseLibrarySettings fills in defaults for keys the stored settings lack.
func ParseLibrarySettings(raw JSONB) LibrarySettings {
	s := DefaultLibrarySettings()
	_ = json.Unmarshal(raw, &s)
	if s.AutoMatch == nil {
		s.AutoMatch = Switches{}
	}
	return s
}

type Content struct {
	ID         string     `db:"id" json:"id"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at" json:"updated_at"`
	URIPart    string     `db:"uri_part" json:"uri_part"`
	URI        string     `db:"uri" json:"uri"`
	Valid      bool       `db:"valid" json:"valid"`
	FileURI    *string    `db:"file_uri" json:"file_uri"`
	FileMtime  *time.Time `db:"file_mtime" json:"file_mtime"`
	FileSize   *int       `db:"file_size" json:"file_size"`
	CoverURI   *string    `db:"cover_uri" json:"cover_uri"`
	Type       string     `db:"type" json:"type"`
	Order      *int       `db:"order" json:"order"`
	OrderParts []*float32 `db:"order_parts" json:"order_parts"`
	FileData   JSONB      `db:"file_data" json:"file_data"`
	ParentID   *string    `db:"parent_id" json:"parent_id"`
	LibraryID  string     `db:"library_id" json:"library_id"`
	WordCount  *int       `db:"word_count" json:"word_count"`
	PageCount  *int       `db:"page_count" json:"page_count"`
}

var contentColumns = []string{"id", "created_at", "updated_at", "uri_part", "uri", "valid", "file_uri",
	"file_mtime", "file_size", "cover_uri", "type", `"order"`, "order_parts", "file_data", "parent_id",
	"library_id", "word_count", "page_count"}

// ContentColumns lists the columns Content reads, qualified by alias unless it is empty. The
// table also holds the metadata, which strict row scans would reject.
func ContentColumns(alias string) string {
	if alias == "" {
		return strings.Join(contentColumns, ", ")
	}
	return alias + "." + strings.Join(contentColumns, ", "+alias+".")
}

type UserToContent struct {
	ID                string     `db:"id" json:"id"`
	UserID            string     `db:"user_id" json:"user_id"`
	LibraryID         *string    `db:"library_id" json:"library_id"`
	URI               string     `db:"uri" json:"uri"`
	Starred           bool       `db:"starred" json:"starred"`
	Status            *string    `db:"status" json:"status"`
	StatusUpdatedAt   *time.Time `db:"status_updated_at" json:"status_updated_at"`
	Notes             *string    `db:"notes" json:"notes"`
	Rating            *int       `db:"rating" json:"rating"`
	Progress          JSONB      `db:"progress" json:"progress"`
	ProgressUpdatedAt *time.Time `db:"progress_updated_at" json:"progress_updated_at"`
	Revision          *string    `db:"revision" json:"revision"`
	LastReadAt        *time.Time `db:"last_read_at" json:"last_read_at"`
}

type CustomList struct {
	ID          string    `db:"id" json:"id"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
	Name        string    `db:"name" json:"name"`
	Description *string   `db:"description" json:"description"`
	Visibility  string    `db:"visibility" json:"visibility"`
	UserID      string    `db:"user_id" json:"user_id"`
}

type CustomListToContent struct {
	ID           string    `db:"id" json:"id"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
	CustomListID string    `db:"custom_list_id" json:"custom_list_id"`
	LibraryID    string    `db:"library_id" json:"library_id"`
	URI          string    `db:"uri" json:"uri"`
	Notes        *string   `db:"notes" json:"notes"`
	Order        *int      `db:"order" json:"order"`
}

const (
	TaskStatusPending    = 0
	TaskStatusInProgress = 1
	TaskStatusCompleted  = 2
	TaskStatusFailed     = 3
	TaskStatusCancelled  = 4
)

type Task struct {
	ID        string    `db:"id" json:"id"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	Name      string    `db:"name" json:"name"`
	Status    int       `db:"status" json:"status"`
	Input     JSONB     `db:"input" json:"input"`
	Output    JSONB     `db:"output" json:"output"`
	Logs      *string   `db:"logs" json:"logs"`
	UserID    *string   `db:"user_id" json:"user_id"`
	LibraryID *string   `db:"library_id" json:"library_id"`
}

const idChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func makeID(prefix string) string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = idChars[rand.Intn(len(idChars))]
	}
	return prefix + "_" + string(b)
}

func MakeUserID() string              { return makeID("u") }
func MakeLibraryID() string           { return makeID("l") }
func MakeContentID() string           { return makeID("c") }
func MakeUserToContentID() string     { return makeID("utc") }
func MakeCustomListID() string        { return makeID("cl") }
func MakeCustomListContentID() string { return makeID("clc") }
func MakeTaskID() string              { return makeID("t") }
func MakeIdentityID() string          { return makeID("ui") }
func MakeAuthPendingID() string       { return makeID("ap") }
func MakeAppKeyID() string            { return makeID("ok") }
