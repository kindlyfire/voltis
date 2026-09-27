package metadata

import "encoding/json"

type Presence uint8

const (
	Absent Presence = iota
	Null            // explicitly cleared
	Value
)

// Opt is a field with three states. In JSON, a missing key is Absent, null is Null, and anything
// else is a Value, so 0 and "" are values.
type Opt[T any] struct {
	P Presence
	V T
}

func Val[T any](v T) Opt[T] { return Opt[T]{P: Value, V: v} }

// Set is a Value unless v is zero, for sources where zero means missing.
func Set[T comparable](v T) Opt[T] {
	var zero T
	if v == zero {
		return Opt[T]{}
	}
	return Val(v)
}

func SetList[T any](v []T) Opt[[]T] {
	if len(v) == 0 {
		return Opt[[]T]{}
	}
	return Val(v)
}

func Ptr[T any](p *T) Opt[T] {
	if p == nil {
		return Opt[T]{}
	}
	return Val(*p)
}

// IsZero reports Absent, for `omitzero`.
func (o Opt[T]) IsZero() bool { return o.P == Absent }

func (o Opt[T]) Get() (T, bool) { return o.V, o.P == Value }

func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if o.P != Value {
		return []byte("null"), nil
	}
	return json.Marshal(o.V)
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*o = Opt[T]{P: Null}
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*o = Val(v)
	return nil
}
