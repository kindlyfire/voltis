package reading

import "errors"

// ErrNotFound is returned for content that doesn't exist.
var ErrNotFound = errors.New("Content not found")

// ErrVolumeNotFound is returned for a volume that isn't in the series.
var ErrVolumeNotFound = errors.New("Volume not found")

// InvalidError is a request the content can't take.
type InvalidError struct{ Msg string }

func (e InvalidError) Error() string { return e.Msg }

// ConflictError is a reader's write that its base revision no longer matches. Current is the
// state it conflicted with.
type ConflictError struct{ Current Envelope }

func (ConflictError) Error() string { return "reading state changed elsewhere" }
