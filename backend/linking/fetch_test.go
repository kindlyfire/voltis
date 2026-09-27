package linking

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"voltis/metadata"
	"voltis/providers/providertest"
)

func TestFetch(t *testing.T) {
	p := providertest.New()
	p.Put("1", providertest.Series("One", metadata.Manga))
	p.Put("2", providertest.Payload{MergedInto: "3"})
	p.Put("3", providertest.Payload{MergedInto: "4"})
	p.Put("4", providertest.Series("Four", metadata.Manga))
	p.Put("5", providertest.Payload{MergedInto: "6"})
	p.Put("6", providertest.Payload{MergedInto: "5"})
	p.Put("7", providertest.Payload{Deleted: true})
	p.Put("8", json.RawMessage(`"undecodable"`))
	p.Put("9", providertest.Payload{MergedInto: "missing"})

	type got struct {
		hops    string
		outcome FetchOutcome
	}
	want := []got{{"1", Found}, {"2 3 4", Found}, {"5 6", Failed}, {"7", Deleted}, {"8", Failed}, {"9", Unavailable}, {"", Unavailable}}
	res := fetch(context.Background(), p, []string{"1", "2", "5", "7", "8", "9", "10"})
	var gots []got
	for _, f := range res {
		gots = append(gots, got{strings.Join(f.Hops, " "), f.Outcome})
	}
	if !reflect.DeepEqual(gots, want) {
		t.Fatalf("got  %v\nwant %v", gots, want)
	}
	if res[1].Requested.ID != "2" || res[1].Record.Fields.Title.V != "Four" {
		t.Fatalf("merge chain = %+v", res[1])
	}
	if res[0].ObservedAt.IsZero() || res[0].ObservedAt != res[1].ObservedAt {
		t.Fatal("observation times differ within a fetch")
	}
	// Batches of at most two, then one round per merge hop.
	if got := p.Fetches(); len(got[0]) != 2 || len(got) != 4+1+1+1 {
		t.Fatalf("fetches = %v", got)
	}

	p.Fail(errors.New("down"))
	if f := fetch(context.Background(), p, []string{"1"})[0]; f.Outcome != Failed || f.Err == nil {
		t.Fatalf("provider failure = %+v", f)
	}
}
