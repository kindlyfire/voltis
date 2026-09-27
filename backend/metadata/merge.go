package metadata

import (
	"reflect"
	"slices"
	"strings"

	"voltis/lib/fp"
)

// DataVersion is bumped whenever Merge, Normalize, or a provider mapper changes its output, so
// the linking worker recomputes stored rows in the background. Rows it has not reached yet are
// served as they are, so every bump must keep the data of the previous version readable by the
// current frontend.
const DataVersion = 2

type Layer struct {
	Source string // "file", a provider name, or "overrides"
	Fields Fields
}

type Resolved struct {
	Fields  Fields              // Values only
	Sources map[string][]string // field -> contributing layers, highest first
}

// Merge resolves layers given lowest priority first. A replace field takes the last layer that
// has it, where Null clears it; a union field gathers every layer's values. A title that loses
// to a later layer becomes an alternative title.
func Merge(layers ...Layer) Resolved {
	var out Fields
	contrib := map[string][]string{} // lowest first
	for _, l := range layers {
		f := l.Fields.Normalize()
		for _, d := range defs {
			src, dst := f.field(d.Key), out.field(d.Key)
			switch p := presence(src); {
			case p == Absent || (d.Union && p == Null):
			case d.Union:
				dst.Field(1).Set(reflect.AppendSlice(dst.Field(1), src.Field(1)))
				dst.Field(0).SetUint(uint64(Value))
				contrib[d.Key] = append(contrib[d.Key], l.Source)
			default:
				if d.Key == "title" && p == Value && out.Title.P == Value {
					out.AltTitles = Val(append(out.AltTitles.V, out.Title.V))
					contrib["alt_titles"] = append(contrib["alt_titles"], contrib["title"]...)
				}
				dst.Set(src)
				contrib[d.Key] = []string{l.Source}
			}
		}
	}

	// Normalizing again dedupes the unions.
	out = out.Normalize()
	out.AltTitles = list(out.AltTitles, Absent, func(vs []string) []string {
		return slices.DeleteFunc(vs, func(s string) bool { return strings.EqualFold(s, out.Title.V) })
	})

	sources := map[string][]string{}
	for _, d := range defs {
		v := out.field(d.Key)
		if presence(v) != Value {
			v.SetZero()
			continue
		}
		s := slices.Clone(contrib[d.Key])
		slices.Reverse(s)
		sources[d.Key] = fp.Dedup(s)
	}
	return Resolved{Fields: out, Sources: sources}
}
