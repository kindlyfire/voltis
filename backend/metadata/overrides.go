package metadata

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Msg
	}
	return e.Field + ": " + e.Msg
}

// DecodeOverrides reads an overrides layer as sent by an admin: null clears a field, and so does
// a blank string or an empty list.
func DecodeOverrides(raw json.RawMessage) (Fields, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return Fields{}, &ValidationError{Msg: "overrides must be an object"}
	}
	var f Fields
	for key, val := range m {
		i := slices.IndexFunc(defs, func(d FieldDef) bool { return d.Key == key })
		if i < 0 {
			return Fields{}, &ValidationError{key, "unknown field"}
		}
		d := defs[i]
		if !d.Editable || (d.Type == TypeCover && string(val) != "null") {
			return Fields{}, &ValidationError{key, "not editable"}
		}
		v := f.field(key)
		if err := json.Unmarshal(val, v.Addr().Interface()); err != nil {
			return Fields{}, &ValidationError{key, "invalid value"}
		}
		if presence(v) == Value {
			if err := d.check(v.Field(1)); err != nil {
				return Fields{}, err
			}
		}
	}
	return f.normalize(Null), nil
}

// check rejects a value that normalizing would silently drop.
func (d FieldDef) check(v reflect.Value) error {
	var n float64
	switch v.Kind() {
	case reflect.Int:
		n = float64(v.Int())
	case reflect.Float64:
		n = v.Float()
	case reflect.String:
		s := v.String()
		if d.Type == TypeDate && s != "" && ParsePartialDate(s) == "" {
			return &ValidationError{d.Key, "expected YYYY, YYYY-MM, or YYYY-MM-DD"}
		}
		if d.Type == TypeEnum && s != "" && !slices.Contains(d.Options, s) {
			return &ValidationError{d.Key, fmt.Sprintf("must be one of %s", strings.Join(d.Options, ", "))}
		}
		return nil
	default:
		return nil
	}
	if (d.Min != nil && n < *d.Min) || (d.Max != nil && n > *d.Max) {
		return &ValidationError{d.Key, "out of range"}
	}
	return nil
}
