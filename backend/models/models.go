package models

import (
	"encoding/json"
	"math/rand"
	"time"
)

type JSONB = json.RawMessage

type User struct {
	ID           string    `db:"id" json:"id"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
	Username     string    `db:"username" json:"username"`
	PasswordHash string    `db:"password_hash" json:"-"`
	Permissions  []string  `db:"permissions" json:"permissions"`
	Preferences  JSONB     `db:"preferences" json:"preferences"`
}

type Session struct {
	Token     string    `db:"token"`
	UserID    string    `db:"user_id"`
	ExpiresAt time.Time `db:"expires_at"`
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
}

type ContentMetadata struct {
	URI       string    `db:"uri"`
	LibraryID string    `db:"library_id"`
	Data      JSONB     `db:"data"`
	DataRaw   JSONB     `db:"data_raw"`
	UpdatedAt time.Time `db:"updated_at"`
	ID        string    `db:"id"`
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
	TaskStatusInProgress = 1
	TaskStatusCompleted  = 2
	TaskStatusFailed     = 3
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
