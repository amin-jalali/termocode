package blocks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// one returns a valid block of each kind, the same set the page kit draws.
func one() []Block {
	ref := &BlockRef{Kind: "pr", ID: "418"}
	md := &MdNode{K: "doc", C: []MdNode{{K: "p", C: []MdNode{{K: "text", Text: "All tests pass."}}}}}
	return []Block{
		{ID: "b1", Kind: KindList, Title: "Open pull requests", Items: []BlockItem{
			{ID: "418", Title: "fix toast", Sub: "main ← fix/toast", Tone: ToneBad, Badge: "CI ✗", Time: 1, Ref: ref,
				Actions: []BlockAction{{ID: "a1", Label: "To chat", Kind: ActAttach, Ref: ref}}},
		}, Total: 3, Truncated: true},
		{ID: "b2", Kind: KindTable, Title: "Pull requests", Columns: []BlockColumn{{ID: "n", Label: "#", Kind: ColNum}, {ID: "t", Label: "title"}},
			Rows: [][]string{{"418", "fix toast"}}, Spec: &ViewSpec{View: "git.prs", Fields: []string{"n", "t"}, Sort: []ViewSort{{Field: "n", Desc: true}}, Limit: 20}},
		{ID: "b3", Kind: KindDiff, Title: "a.go", Diff: &BlockDiff{Path: "internal/a.go", Adds: 1, Dels: 1, Hunks: []BlockHunk{
			{Header: "@@ -1,2 +1,2 @@", Lines: []BlockDiffLine{{K: "ctx", A: 1, B: 1, Text: "package a"}, {K: "del", A: 2, Text: "x"}, {K: "add", B: 2, Text: "y"}}}}}},
		{ID: "b4", Kind: KindCard, Title: "Run 12", Fields: []BlockField{{Label: "state", Value: "running", Tone: ToneBusy}}},
		{ID: "b5", Kind: KindDetail, Title: "Container web", Fields: []BlockField{{Label: "image", Value: "nginx:1", Kind: ColMono}}},
		{ID: "b6", Kind: KindChart, Title: "Cost today", Bars: []BlockBar{{Label: "claude", Value: 1.2, Text: "$1.20"}}},
		{ID: "b7", Kind: KindStatus, Title: "CI", State: ToneOK},
		{ID: "b8", Kind: KindForm, Title: "New issue", Form: []BlockFormField{{ID: "title", Label: "Title", Kind: FormText, Required: true}, {ID: "kind", Label: "Kind", Kind: FormChoice, Choices: []string{"bug", "idea"}}}},
		{ID: "b9", Kind: KindJSON, Title: "Raw result", JSON: `{"ok":true}`},
		{ID: "b10", Kind: KindText, Title: "Answer", MD: md, ForYou: "Two files changed."},
		{ID: "b11", Kind: KindBoard, Title: "Board", Board: &BlockBoard{Columns: []BlockBoardColumn{{ID: "need", Label: "Needs you", Tone: ToneWarn, Items: []BlockItem{{ID: "c1", Title: "Allow go test"}}}}}},
	}
}

func TestOneOfEachKindIsValid(t *testing.T) {
	got := map[string]bool{}
	for _, b := range one() {
		b := b
		if err := b.Validate(); err != nil {
			t.Errorf("%s: %v", b.Kind, err)
		}
		got[b.Kind] = true
	}
	for _, k := range Kinds() {
		if !got[k] {
			t.Errorf("no sample of kind %s", k)
		}
	}
	if len(Kinds()) != 11 {
		t.Errorf("%d kinds, the plan has eleven", len(Kinds()))
	}
}

func long(n int) string { return strings.Repeat("x", n) }

// The guard: a block over any cap, or breaking a rule, is refused.
func TestValidateRefusesBlocksOverTheCaps(t *testing.T) {
	items := func(n int) []BlockItem {
		out := make([]BlockItem, n)
		for i := range out {
			out[i] = BlockItem{ID: "i" + strings.Repeat("0", i%5), Title: "t"}
		}
		return out
	}
	cols := func(n int) []BlockColumn {
		out := make([]BlockColumn, n)
		for i := range out {
			out[i] = BlockColumn{ID: "c", Label: "c"}
		}
		return out
	}
	rows := func(n, w int, cell string) [][]string {
		out := make([][]string, n)
		for i := range out {
			out[i] = make([]string, w)
			for j := range out[i] {
				out[i][j] = cell
			}
		}
		return out
	}
	acts := func(n int) []BlockAction {
		out := make([]BlockAction, n)
		for i := range out {
			out[i] = BlockAction{ID: "a", Label: "a", Kind: ActLaptop}
		}
		return out
	}
	big := make([]BlockItem, MaxItems)
	for i := range big {
		big[i] = BlockItem{ID: "i", Title: long(MaxTitle), Sub: long(MaxSub)}
	}
	cases := map[string]Block{
		"unknown kind":     {ID: "b", Kind: "html", Title: "x"},
		"no id":            {Kind: KindList, Title: "x"},
		"bad id":           {ID: "a b", Kind: KindList, Title: "x"},
		"empty title":      {ID: "b", Kind: KindList, Title: " "},
		"long title":       {ID: "b", Kind: KindList, Title: long(MaxTitle + 1)},
		"control in title": {ID: "b", Kind: KindList, Title: "a\x1b[31m"},
		"items":            {ID: "b", Kind: KindList, Title: "x", Items: items(MaxItems + 1)},
		"columns":          {ID: "b", Kind: KindTable, Title: "x", Columns: cols(MaxColumns + 1)},
		"rows":             {ID: "b", Kind: KindTable, Title: "x", Columns: cols(1), Rows: rows(MaxRows+1, 1, "a")},
		"long cell":        {ID: "b", Kind: KindTable, Title: "x", Columns: cols(1), Rows: rows(1, 1, long(MaxSub+1))},
		"ragged row":       {ID: "b", Kind: KindTable, Title: "x", Columns: cols(2), Rows: rows(1, 1, "a")},
		"table no columns": {ID: "b", Kind: KindTable, Title: "x"},
		"actions":          {ID: "b", Kind: KindList, Title: "x", Actions: acts(MaxActions + 1)},
		"fields":           {ID: "b", Kind: KindCard, Title: "x", Fields: make([]BlockField, MaxFields+1)},
		"bars":             {ID: "b", Kind: KindChart, Title: "x", Bars: make([]BlockBar, MaxBars+1)},
		"negative bar":     {ID: "b", Kind: KindChart, Title: "x", Bars: []BlockBar{{Label: "a", Value: -1}}},
		"json too big":     {ID: "b", Kind: KindJSON, Title: "x", JSON: `"` + long(MaxJSON) + `"`},
		"json not json":    {ID: "b", Kind: KindJSON, Title: "x", JSON: `{nope`},
		"whole block":      {ID: "b", Kind: KindList, Title: "x", Items: big},
		"absolute path":    {ID: "b", Kind: KindList, Title: "x", Ref: &BlockRef{Kind: "file", Path: "/etc/passwd"}},
		"drive path":       {ID: "b", Kind: KindList, Title: "x", Ref: &BlockRef{Kind: "file", Path: `C:\x`}},
		"dotdot path":      {ID: "b", Kind: KindList, Title: "x", Ref: &BlockRef{Kind: "file", Path: "a/../../b"}},
		"url path":         {ID: "b", Kind: KindDiff, Title: "x", Diff: &BlockDiff{Path: "https://x/y"}},
		"secret field":     {ID: "b", Kind: KindForm, Title: "x", Form: []BlockFormField{{ID: "pw", Label: "Password", Kind: FormText}}},
		"token field":      {ID: "b", Kind: KindForm, Title: "x", Form: []BlockFormField{{ID: "api_token", Label: "Value", Kind: FormText}}},
		"form kind":        {ID: "b", Kind: KindForm, Title: "x", Form: []BlockFormField{{ID: "a", Label: "a", Kind: "secret"}}},
		"choices":          {ID: "b", Kind: KindForm, Title: "x", Form: []BlockFormField{{ID: "a", Label: "a", Kind: FormText, Choices: []string{"x"}}}},
		"op action":        {ID: "b", Kind: KindList, Title: "x", Actions: []BlockAction{{ID: "a", Label: "a", Kind: ActOp, Op: "rm -rf"}}},
		"view action":      {ID: "b", Kind: KindList, Title: "x", Actions: []BlockAction{{ID: "a", Label: "a", Kind: ActView}}},
		"status no state":  {ID: "b", Kind: KindStatus, Title: "x"},
		"unknown tone":     {ID: "b", Kind: KindStatus, Title: "x", State: "red"},
		"board columns":    {ID: "b", Kind: KindBoard, Title: "x", Board: &BlockBoard{}},
		"diff line kind":   {ID: "b", Kind: KindDiff, Title: "x", Diff: &BlockDiff{Path: "a", Hunks: []BlockHunk{{Lines: []BlockDiffLine{{K: "x"}}}}}},
		"spec url arg":     {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "http.req", Args: map[string]string{"u": "https://x"}}},
		"spec abs arg":     {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "files", Args: map[string]string{"p": "/etc"}}},
		"spec limit":       {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "v", Limit: MaxSpecLimit + 1}},
		"spec refresh":     {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "v", Refresh: 2}},
		"spec cond op":     {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "v", Filter: []ViewCond{{Field: "f", Op: "like"}}}},
		"spec in":          {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "v", Filter: []ViewCond{{Field: "f", Op: "in"}}}},
		"spec density":     {ID: "b", Kind: KindList, Title: "x", Spec: &ViewSpec{View: "v", Density: "huge"}},
	}
	for name, b := range cases {
		b := b
		err := b.Validate()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	var nilBlock *Block
	if nilBlock.Validate() == nil {
		t.Error("a nil block is valid")
	}
}

func TestFormAllowsHarmlessNames(t *testing.T) {
	for _, id := range []string{"compass", "passed", "max_tokens", "spinner", "topic"} {
		b := Block{ID: "b", Kind: KindForm, Title: "x", Form: []BlockFormField{{ID: id, Label: id, Kind: FormText}}}
		if err := b.Validate(); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// FuzzBlockValidate: no panic on any JSON; an accepted block encodes in
// MaxBlockBytes and is accepted again after a round trip.
func FuzzBlockValidate(f *testing.F) {
	for _, b := range one() {
		raw, err := json.Marshal(b)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Add([]byte(`{"id":"b","kind":"table","title":"t","columns":[{"id":"a","label":"a"}],"rows":[["x","y"]]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var b Block
		if json.Unmarshal(raw, &b) != nil {
			return
		}
		if b.Validate() != nil {
			return
		}
		out, err := json.Marshal(b)
		if err != nil || len(out) > MaxBlockBytes {
			t.Fatalf("accepted block encodes to %d bytes: %v", len(out), err)
		}
		var again Block
		if err := json.Unmarshal(out, &again); err != nil {
			t.Fatal(err)
		}
		if err := again.Validate(); err != nil {
			t.Fatalf("round trip refused: %v", err)
		}
	})
}
