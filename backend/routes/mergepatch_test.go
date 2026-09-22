package routes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergePatch(t *testing.T) {
	// RFC 7396 appendix A, plus a no-op patch, a nested object created under an
	// absent key, and integers above 2^53, which only survive via UseNumber().
	cases := []struct{ target, patch, want string }{
		{`{"a":"b"}`, `{"a":"c"}`, `{"a":"c"}`},
		{`{"a":"b"}`, `{"b":"c"}`, `{"a":"b","b":"c"}`},
		{`{"a":"b"}`, `{"a":null}`, `{}`},
		{`{"a":"b","b":"c"}`, `{"a":null}`, `{"b":"c"}`},
		{`{"a":["b"]}`, `{"a":"c"}`, `{"a":"c"}`},
		{`{"a":"c"}`, `{"a":["b"]}`, `{"a":["b"]}`},
		{`{"a":{"b":"c"}}`, `{"a":{"b":"d","c":null}}`, `{"a":{"b":"d"}}`},
		{`{"a":[{"b":"c"}]}`, `{"a":[1]}`, `{"a":[1]}`},
		{`["a","b"]`, `["c","d"]`, `["c","d"]`},
		{`{"a":"b"}`, `["c"]`, `["c"]`},
		{`{"a":"foo"}`, `null`, `null`},
		{`{"a":"foo"}`, `"bar"`, `"bar"`},
		{`{"e":null}`, `{"a":1}`, `{"a":1,"e":null}`},
		{`[1,2]`, `{"a":"b","c":null}`, `{"a":"b"}`},
		{`{}`, `{"a":{"bb":{"ccc":null}}}`, `{"a":{"bb":{}}}`},
		{`{"a":{"b":"c"}}`, `{}`, `{"a":{"b":"c"}}`},
		{`{"a":"b"}`, `{"t":{"comicReader":true}}`, `{"a":"b","t":{"comicReader":true}}`},
		{
			`{"big":10000000000000001,"keep":20000000000000003}`,
			`{"big":30000000000000005}`,
			`{"big":30000000000000005,"keep":20000000000000003}`,
		},
	}

	// As the handler does: through float64 these integers would come back
	// rounded.
	decode := func(s string) any {
		t.Helper()
		d := json.NewDecoder(strings.NewReader(s))
		d.UseNumber()
		var v any
		if err := d.Decode(&v); err != nil {
			t.Fatalf("decode %s: %v", s, err)
		}
		return v
	}

	for _, tc := range cases {
		got, err := json.Marshal(mergePatch(decode(tc.target), decode(tc.patch)))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(got) != tc.want {
			t.Errorf("mergePatch(%s, %s) = %s, want %s", tc.target, tc.patch, got, tc.want)
		}
	}
}
