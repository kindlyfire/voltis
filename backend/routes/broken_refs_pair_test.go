package routes

import (
	"fmt"
	"strings"
	"testing"
)

func TestPairBrokenRefs(t *testing.T) {
	str := func(s string) *string { return &s }
	const g, to = "comic/Old_2020", "comic/New_2020"
	for _, tc := range []struct {
		name          string
		group         string
		srcs          []string
		to            string
		toParts       []string
		overrides     map[string]*string
		wantPairs     string // "from>to:by,..." with the group and target cut to their last segment
		wantLeftovers string
	}{
		{name: "root and exact", srcs: []string{g, g + "/ch1"}, toParts: []string{"ch1"},
			wantPairs: "Old_2020>New_2020:root,ch1>ch1:exact"},
		{name: "chapter keeps its number across volumes", srcs: []string{g + "/ch12"}, toParts: []string{"v2_ch12"},
			wantPairs: "ch12>v2_ch12:number"},
		{name: "volume never meets chapter", srcs: []string{g + "/v12"}, toParts: []string{"ch12"},
			wantLeftovers: "v12"},
		{name: "a year never meets other numbers", srcs: []string{g + "/y1999"}, toParts: []string{"ch1999", "v1999"},
			wantLeftovers: "y1999"},
		{name: "two targets with one number", srcs: []string{g + "/ch3"}, toParts: []string{"v1_ch3", "v2_ch3"},
			wantLeftovers: "ch3"},
		{name: "two sources with one number", srcs: []string{g + "/ch3", g + "/v1_ch3"}, toParts: []string{"v2_ch3"},
			wantLeftovers: "ch3,v1_ch3"},
		{name: "book volumes", group: "book/Gone Stem", to: "book/Quiet Orchard",
			srcs:      []string{"book/Gone Stem/Gone Stem Vol. 3", "book/Gone Stem/Gone Stem Vol. 3 Bonus"},
			toParts:   []string{"Quiet Orchard v03 [Grp]", "Quiet Orchard Vol. 3 Bonus"},
			wantPairs: "Gone Stem Vol. 3>Quiet Orchard v03 [Grp]:number", wantLeftovers: "Gone Stem Vol. 3 Bonus"},
		{name: "override takes the number's target", srcs: []string{g + "/ch2", g + "/ch5"}, toParts: []string{"v1_ch2"},
			overrides: map[string]*string{g + "/ch5": str(to + "/v1_ch2")},
			wantPairs: "ch5>v1_ch2:manual", wantLeftovers: "ch2"},
		{name: "nil override unpairs", srcs: []string{g + "/ch1"}, toParts: []string{"ch1"},
			overrides: map[string]*string{g + "/ch1": nil}, wantLeftovers: "ch1"},
		{name: "one item moves anywhere", group: "book/Gone Stem", srcs: []string{"book/Gone Stem"},
			to: "comic/New_2020/ch1", wantPairs: "Gone Stem>ch1:root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.group == "" {
				tc.group = g
			}
			if tc.to == "" {
				tc.to = to
			}
			p, err := pairRefs(tc.group, tc.srcs, tc.to, tc.toParts, tc.overrides, partKeys{})
			if err != nil {
				t.Fatal(err)
			}
			last := func(uri string) string { return uri[strings.LastIndexByte(uri, '/')+1:] }
			var pairs, left []string
			for _, pr := range p.Pairs {
				pairs = append(pairs, fmt.Sprintf("%s>%s:%s", last(pr.From), last(pr.To), pr.By))
			}
			for _, l := range p.Leftovers {
				left = append(left, last(l))
			}
			assertEq(t, strings.Join(pairs, ","), tc.wantPairs)
			assertEq(t, strings.Join(left, ","), tc.wantLeftovers)
		})
	}

	t.Run("bad overrides", func(t *testing.T) {
		for _, o := range []map[string]*string{
			{g + "/ch9": str(to)},
			{g + "/ch1": str(to + "/ch1"), g + "/ch2": str(to + "/ch1")},
		} {
			if _, err := pairRefs(g, []string{g + "/ch1", g + "/ch2"}, to, []string{"ch1"}, o, partKeys{}); err == nil {
				t.Errorf("%v: no error", o)
			}
		}
	})

	t.Run("suggest", func(t *testing.T) {
		ix := newTargetIndex([]targetRow{
			{"comic/Old_2020", "Old_2020", str("ch9")},
			{"comic/Other_2001", "Other_2001", str("ch1")},
			{"comic/Olden_2001", "Olden_2001", str("v1_ch1")},
			{"comic/Zeta", "Zeta", str("ch1")},
			{"comic/Zeta", "Zeta", str("ch2")},
			{"comic/Nothing", "Nothing", str("ch7")},
			{"book/Old_2020", "Old_2020", str("ch1")},
		})
		got := ix.suggest(g, []string{g, g + "/ch1", g + "/ch2"})
		assertEq(t, fmt.Sprint(got), "[{comic/Old_2020 0 2 true} {comic/Zeta 2 2 false} "+
			"{comic/Olden_2001 1 2 false} {comic/Other_2001 1 2 false}]")
		assertEq(t, len(ix.suggest("book/Gone Stem", []string{"book/Gone Stem"})), 0)

		// Generic volumes pair with an unrelated series too; the shared name word wins.
		ix = newTargetIndex([]targetRow{
			{"comic/Lantern Bay_1997", "Lantern Bay_1997", str("v1")},
			{"comic/Lantern Bay_1997", "Lantern Bay_1997", str("v2")},
			{"comic/Quiet Harbor Redux", "Quiet Harbor Redux", str("v1")},
		})
		h := "comic/Quiet-Harbor_2011"
		got = ix.suggest(h, []string{h + "/v1", h + "/v2"})
		assertEq(t, fmt.Sprint(got), "[{comic/Quiet Harbor Redux 1 2 false} {comic/Lantern Bay_1997 2 2 false}]")

		// Short words such as "The" aren't shared names.
		ix = newTargetIndex([]targetRow{
			{"comic/The Harbor Redux", "The Harbor Redux", str("v1")},
			{"comic/The Harbor Redux", "The Harbor Redux", str("v2")},
			{"comic/The Lantern Redux", "The Lantern Redux", str("v1")},
		})
		l := "comic/The Lantern"
		got = ix.suggest(l, []string{l + "/v1", l + "/v2"})
		assertEq(t, fmt.Sprint(got), "[{comic/The Lantern Redux 1 2 false} {comic/The Harbor Redux 2 2 false}]")
	})
}
