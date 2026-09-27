package providers_test

import (
	"encoding/json"
	"testing"

	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/providertest"
)

func TestLayerTakesOnlyWebCovers(t *testing.T) {
	fake := providertest.New()
	reg := providers.NewRegistry(fake)
	for coverURL, want := range map[string]bool{
		"https://img.example/a.jpg": true, "http://img.example/a.jpg": true,
		"file:///etc/passwd": false, "ftp://img.example/a.jpg": false, "": false,
	} {
		p := providertest.Series("S", metadata.Manga)
		p.CoverURL = coverURL
		raw, _ := json.Marshal(p)
		f, err := reg.Layer(metadata.EntryKey{Provider: fake.Name(), ID: "1"}, raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Cover.P == metadata.Value; got != want {
			t.Errorf("%q: cover = %+v", coverURL, f.Cover)
		}
	}
}
