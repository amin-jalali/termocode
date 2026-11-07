package blocks

import (
	"strings"
	"testing"
)

// MOB-10: MapStrings changes every text of a block (titles, items,
// cells, fields, json …) and keeps its shape; ids, kinds, tones and
// paths stay as they are.
func TestMapStringsRedactsEveryText(t *testing.T) {
	b := &Block{ID: "b1", Kind: KindTable, Title: "S title", ForYou: "S for you",
		Columns: []BlockColumn{{ID: "c", Label: "S label", Kind: ColText}},
		Rows:    [][]string{{"S cell"}},
		Items:   []BlockItem{{ID: "i1", Title: "S item", Sub: "S sub", Tone: ToneOK}},
		Fields:  []BlockField{{Label: "S f", Value: "S v"}},
		JSON:    `{"k":"S json"}`,
		Ref:     &BlockRef{Kind: "file", Path: "a/S.go"}}
	got, err := b.MapStrings(func(s string) string { return strings.ReplaceAll(s, "S ", "") })
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "title" || got.ForYou != "for you" || got.Columns[0].Label != "label" || got.Rows[0][0] != "cell" ||
		got.Items[0].Title != "item" || got.Items[0].Sub != "sub" || got.Fields[0].Value != "v" || got.JSON != `{"k":"json"}` {
		t.Errorf("mapped = %+v", got)
	}
	if got.ID != "b1" || got.Kind != KindTable || got.Items[0].ID != "i1" || got.Items[0].Tone != ToneOK || got.Ref.Path != "a/S.go" {
		t.Errorf("structure changed: %+v", got)
	}
	if b.Title != "S title" {
		t.Error("MapStrings changed the original")
	}
}

// PlainText is the block as short lines for the model (a block attach):
// title, then items / rows / fields, cut at max bytes.
func TestPlainText(t *testing.T) {
	b := &Block{ID: "b", Kind: KindTable, Title: "Open PRs", Source: "pr.list",
		Columns: []BlockColumn{{ID: "n", Label: "#"}, {ID: "t", Label: "Title"}},
		Rows:    [][]string{{"12", "Fix login"}, {"13", "Docs"}}}
	got := b.PlainText(1 << 10)
	for _, want := range []string{"Open PRs (table, from pr.list)", "# | Title", "12 | Fix login", "13 | Docs"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	l := &Block{ID: "l", Kind: KindList, Title: "Hits", Items: []BlockItem{{ID: "1", Title: "a.go", Sub: "line 3", Badge: "+2"}}}
	if got := l.PlainText(1 << 10); !strings.Contains(got, "- a.go — line 3 [+2]") {
		t.Errorf("list:\n%s", got)
	}
	d := &Block{ID: "d", Kind: KindDetail, Title: "Run", Fields: []BlockField{{Label: "State", Value: "done"}}}
	if got := d.PlainText(1 << 10); !strings.Contains(got, "State: done") {
		t.Errorf("detail:\n%s", got)
	}
	big := &Block{ID: "x", Kind: KindText, Title: "T", JSON: strings.Repeat("é", 500)}
	if got := big.PlainText(100); len(got) > 100+len("\n… (cut)") || !strings.HasSuffix(got, "… (cut)") {
		t.Errorf("cut: %d bytes %q", len(got), got[len(got)-12:])
	}
	if (*Block)(nil).PlainText(10) != "" {
		t.Error("nil block")
	}
}

// PlainText walks every part a block may have: for-you line, item tones,
// groups (items and rows), bars (text or value), state, diff, board,
// markdown (with a table) and the "only the first are shown" line.
func TestPlainTextEveryPart(t *testing.T) {
	md := &MdNode{K: "doc", C: []MdNode{
		{K: "h", C: []MdNode{{K: "text", Text: "Title"}}},
		{K: "p", C: []MdNode{{K: "text", Text: "Body"}}},
		{K: "table", Rows: [][]MdNode{{{K: "text", Text: "a"}, {K: "text", Text: "b"}}}},
	}}
	b := &Block{ID: "b", Kind: KindText, Title: "All", ForYou: "Look here.", MD: md, State: ToneOK, Total: 9, Truncated: true,
		Columns: []BlockColumn{{ID: "c", Label: "C"}},
		Items:   []BlockItem{{ID: "i", Title: "item", Tone: ToneBad}},
		Groups:  []BlockGroup{{Label: "G", Count: 2, Items: []BlockItem{{ID: "g", Title: "gi"}}, Rows: [][]string{{"gr"}}}},
		Bars:    []BlockBar{{Label: "cost", Text: "$1"}, {Label: "n", Value: 3}},
		Diff:    &BlockDiff{Path: "a.go", Adds: 2, Dels: 1},
		Board:   &BlockBoard{Columns: []BlockBoardColumn{{ID: "c", Label: "Col", Items: []BlockItem{{ID: "x", Title: "card"}}}}},
		JSON:    `{"k":1}`}
	got := b.PlainText(0)
	for _, want := range []string{"All (text)\nLook here.\n", "- item (bad)", "G (2)\n  - gi\n  gr\n", "cost: $1", "n: 3",
		"state: ok", "diff a.go +2 −1", "Col\n  - card", "Title\n", "Body\n", "a b \n", `{"k":1}`, "(9 in all; only the first are shown)"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

// MapStrings on a nil block is nil, without an error.
func TestMapStringsNil(t *testing.T) {
	out, err := (*Block)(nil).MapStrings(strings.ToUpper)
	if out != nil || err != nil {
		t.Errorf("nil block: %v %v", out, err)
	}
}
