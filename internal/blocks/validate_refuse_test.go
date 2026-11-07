package blocks

import (
	"errors"
	"strings"
	"testing"
)

// The guard, rule by rule: each block breaks one rule and is refused with
// ErrInvalid and a message that names that rule.
func TestValidateRefusesEachRule(t *testing.T) {
	list := func(mod func(*Block)) Block {
		b := Block{ID: "b", Kind: KindList, Title: "x"}
		mod(&b)
		return b
	}
	table := func(mod func(*Block)) Block {
		b := Block{ID: "b", Kind: KindTable, Title: "x", Columns: []BlockColumn{{ID: "c", Label: "c"}}}
		mod(&b)
		return b
	}
	spec := func(mod func(*ViewSpec)) Block {
		s := &ViewSpec{View: "v"}
		mod(s)
		return Block{ID: "b", Kind: KindList, Title: "x", Spec: s}
	}
	diff := func(d BlockDiff) Block { return Block{ID: "b", Kind: KindDiff, Title: "x", Diff: &d} }
	form := func(f ...BlockFormField) Block { return Block{ID: "b", Kind: KindForm, Title: "x", Form: f} }
	board := func(c ...BlockBoardColumn) Block {
		return Block{ID: "b", Kind: KindBoard, Title: "x", Board: &BlockBoard{Columns: c}}
	}
	item := func(it BlockItem) Block { return list(func(b *Block) { b.Items = []BlockItem{it} }) }
	act := func(a BlockAction) Block { return list(func(b *Block) { b.Actions = []BlockAction{a} }) }
	strs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "v"
		}
		return out
	}
	hunks := func(n, lines int) []BlockHunk {
		out := make([]BlockHunk, n)
		for i := range out {
			out[i].Lines = make([]BlockDiffLine, lines)
			for j := range out[i].Lines {
				out[i].Lines[j] = BlockDiffLine{K: "ctx"}
			}
		}
		return out
	}
	cases := []struct {
		name, msg string
		b         Block
	}{
		// the block's own text
		{"source control", "source has a control", list(func(b *Block) { b.Source = "a\x07" })},
		{"ws too long", "ws has", list(func(b *Block) { b.WS = long(MaxID + 1) })},
		{"for you too long", "for_you has", list(func(b *Block) { b.ForYou = long(MaxForYou + 1) })},
		{"title not utf8", "not UTF-8", list(func(b *Block) { b.Title = "a\xff" })},
		{"negative total", "negative total", list(func(b *Block) { b.Total = -1 })},
		{"bad spec", "bad view", list(func(b *Block) { b.Spec = &ViewSpec{} })},
		{"bad block ref", "bad ref kind", list(func(b *Block) { b.Ref = &BlockRef{} })},
		{"bad block action", "bad action id", list(func(b *Block) { b.Actions = []BlockAction{{Label: "a", Kind: ActLaptop}} })},
		// columns, rows, groups
		{"column id", "bad column id", table(func(b *Block) { b.Columns[0].ID = "a b" })},
		{"column label", "column label has", table(func(b *Block) { b.Columns[0].Label = long(MaxTitle + 1) })},
		{"column kind", "unknown kind", table(func(b *Block) { b.Columns[0].Kind = "html" })},
		{"groups", "groups, max", list(func(b *Block) { b.Groups = make([]BlockGroup, MaxGroups+1) })},
		{"group label", "empty group label", list(func(b *Block) { b.Groups = []BlockGroup{{}} })},
		{"group count", "negative count", list(func(b *Block) { b.Groups = []BlockGroup{{Label: "g", Count: -1}} })},
		{"group items", "bad item id", list(func(b *Block) { b.Groups = []BlockGroup{{Label: "g", Items: []BlockItem{{Title: "t"}}}} })},
		{"group rows", "cells for", table(func(b *Block) { b.Groups = []BlockGroup{{Label: "g", Rows: [][]string{{"a", "b"}}}} })},
		// items
		{"item title", "empty item title", item(BlockItem{ID: "i"})},
		{"item sub", "item sub has", item(BlockItem{ID: "i", Title: "t", Sub: long(MaxSub + 1)})},
		{"item badge", "item badge has", item(BlockItem{ID: "i", Title: "t", Badge: long(MaxID + 1)})},
		{"item tone", "unknown tone", item(BlockItem{ID: "i", Title: "t", Tone: "red"})},
		{"item time", "negative time", item(BlockItem{ID: "i", Title: "t", Time: -1})},
		{"item ref", "negative ref line", item(BlockItem{ID: "i", Title: "t", Ref: &BlockRef{Kind: "file", Line: -1}})},
		{"item action", "unknown kind", item(BlockItem{ID: "i", Title: "t", Actions: []BlockAction{{ID: "a", Label: "a", Kind: "shell"}}})},
		// fields, bars
		{"field label", "empty field label", Block{ID: "b", Kind: KindCard, Title: "x", Fields: []BlockField{{Value: "v"}}}},
		{"field value", "field value has", Block{ID: "b", Kind: KindCard, Title: "x", Fields: []BlockField{{Label: "l", Value: long(MaxSub + 1)}}}},
		{"field tone", "unknown kind or tone", Block{ID: "b", Kind: KindCard, Title: "x", Fields: []BlockField{{Label: "l", Tone: "red"}}}},
		{"bar label", "empty bar label", Block{ID: "b", Kind: KindChart, Title: "x", Bars: []BlockBar{{}}}},
		{"bar text", "bar text has", Block{ID: "b", Kind: KindChart, Title: "x", Bars: []BlockBar{{Label: "l", Text: long(MaxTitle + 1)}}}},
		{"bar absurd", "out of range", Block{ID: "b", Kind: KindChart, Title: "x", Bars: []BlockBar{{Label: "l", Value: 2e15}}}},
		{"bar tone", "unknown tone", Block{ID: "b", Kind: KindChart, Title: "x", Bars: []BlockBar{{Label: "l", Tone: "red"}}}},
		// payloads by kind
		{"form empty", "without its data", Block{ID: "b", Kind: KindForm, Title: "x"}},
		{"json empty", "without its data", Block{ID: "b", Kind: KindJSON, Title: "x"}},
		{"text empty", "without its data", Block{ID: "b", Kind: KindText, Title: "x"}},
		{"board nil", "without its data", Block{ID: "b", Kind: KindBoard, Title: "x"}},
		{"diff nil", "without its data", Block{ID: "b", Kind: KindDiff, Title: "x"}},
		// diff
		{"diff empty path", "empty path", diff(BlockDiff{})},
		{"diff old path", "not relative", diff(BlockDiff{Path: "a", Old: "/b"})},
		{"diff counts", "negative diff counts", diff(BlockDiff{Path: "a", Adds: -1})},
		{"diff hunks", "hunks, max", diff(BlockDiff{Path: "a", Hunks: make([]BlockHunk, MaxDiffHunks+1)})},
		{"diff header", "hunk header has", diff(BlockDiff{Path: "a", Hunks: []BlockHunk{{Header: "@@\x00"}}})},
		{"diff lines", "more than", diff(BlockDiff{Path: "a", Hunks: hunks(2, MaxDiffLines/2+1)})},
		{"diff line number", "bad diff line", diff(BlockDiff{Path: "a", Hunks: []BlockHunk{{Lines: []BlockDiffLine{{K: "add", B: -1}}}}})},
		{"diff line text", "diff line has", diff(BlockDiff{Path: "a", Hunks: []BlockHunk{{Lines: []BlockDiffLine{{K: "add", Text: "\x1b"}}}}})},
		// paths and refs
		{"long path", "bad path", list(func(b *Block) { b.Ref = &BlockRef{Kind: "file", Path: long(MaxPath + 1)} })},
		{"nul path", "bad path", list(func(b *Block) { b.Ref = &BlockRef{Kind: "file", Path: "a\x00b"} })},
		{"backslash path", "not relative", list(func(b *Block) { b.Ref = &BlockRef{Kind: "file", Path: `\x`} })},
		{"control path", "control character", list(func(b *Block) { b.Ref = &BlockRef{Kind: "file", Path: "a\x07"} })},
		{"ref id", "ref id has", list(func(b *Block) { b.Ref = &BlockRef{Kind: "pr", ID: long(MaxTitle + 1)} })},
		// actions
		{"action label", "empty action label", act(BlockAction{ID: "a", Kind: ActLaptop})},
		{"op on a laptop action", "only an op action", act(BlockAction{ID: "a", Label: "a", Kind: ActLaptop, Op: "git.push"})},
		{"action ref", "bad ref kind", act(BlockAction{ID: "a", Label: "a", Kind: ActLaptop, Ref: &BlockRef{}})},
		{"action spec", "limit", act(BlockAction{ID: "a", Label: "a", Kind: ActView, Spec: &ViewSpec{View: "v", Limit: -1}})},
		// form
		{"form fields", "form fields, max", form(make([]BlockFormField, MaxFormFields+1)...)},
		{"form id", "bad form field id", form(BlockFormField{Label: "l", Kind: FormText})},
		{"form label", "empty form label", form(BlockFormField{ID: "a", Kind: FormText})},
		{"form hint", "form hint has", form(BlockFormField{ID: "a", Label: "l", Kind: FormText, Hint: long(MaxSub + 1)})},
		{"form value", "form value has", form(BlockFormField{ID: "a", Label: "l", Kind: FormText, Value: long(MaxSub + 1)})},
		{"secret label", "asks for a secret", form(BlockFormField{ID: "a", Label: "API token", Kind: FormText})},
		{"choice without choices", "choices go with", form(BlockFormField{ID: "a", Label: "l", Kind: FormChoice})},
		{"too many choices", "choices, max", form(BlockFormField{ID: "a", Label: "l", Kind: FormChoice, Choices: strs(MaxChoices + 1)})},
		{"empty choice", "empty form choice", form(BlockFormField{ID: "a", Label: "l", Kind: FormChoice, Choices: []string{" "}})},
		// board
		{"board too many columns", "board has", board(make([]BlockBoardColumn, MaxBoardCols+1)...)},
		{"board column id", "bad board column id", board(BlockBoardColumn{Label: "l"})},
		{"board column label", "empty board column label", board(BlockBoardColumn{ID: "c"})},
		{"board column tone", "unknown tone", board(BlockBoardColumn{ID: "c", Label: "l", Tone: "red"})},
		{"board items", "bad item id", board(BlockBoardColumn{ID: "c", Label: "l", Items: []BlockItem{{Title: "t"}}})},
		// spec
		{"spec ws", "bad spec ws", spec(func(s *ViewSpec) { s.WS = "a b" })},
		{"spec args", "args, max", spec(func(s *ViewSpec) {
			s.Args = map[string]string{}
			for i := 0; i <= MaxSpecArgs; i++ {
				s.Args["a"+strings.Repeat("x", i)] = "v"
			}
		})},
		{"spec arg name", "bad arg name", spec(func(s *ViewSpec) { s.Args = map[string]string{"a b": "v"} })},
		{"spec arg text", "arg k has", spec(func(s *ViewSpec) { s.Args = map[string]string{"k": long(MaxSub + 1)} })},
		{"spec drive arg", "never carries a path", spec(func(s *ViewSpec) { s.Args = map[string]string{"k": `C:\x`} })},
		{"spec fields", "fields, max", spec(func(s *ViewSpec) { s.Fields = strs(MaxSpecFields + 1) })},
		{"spec field id", "bad field", spec(func(s *ViewSpec) { s.Fields = []string{"a b"} })},
		{"spec filters", "filter conditions, max", spec(func(s *ViewSpec) { s.Filter = make([]ViewCond, MaxSpecFilter+1) })},
		{"spec filter field", "bad filter field", spec(func(s *ViewSpec) { s.Filter = []ViewCond{{Op: "eq"}} })},
		{"spec filter value", "filter value has", spec(func(s *ViewSpec) { s.Filter = []ViewCond{{Field: "f", Op: "eq", Value: long(MaxSub + 1)}} })},
		{"spec in too many", "in takes", spec(func(s *ViewSpec) { s.Filter = []ViewCond{{Field: "f", Op: "in", In: strs(MaxSpecIn + 1)}} })},
		{"spec in value", "filter value has", spec(func(s *ViewSpec) { s.Filter = []ViewCond{{Field: "f", Op: "in", In: []string{"\x07"}}} })},
		{"spec sorts", "sort keys, max", spec(func(s *ViewSpec) { s.Sort = make([]ViewSort, MaxSpecSort+1) })},
		{"spec sort field", "bad sort field", spec(func(s *ViewSpec) { s.Sort = []ViewSort{{}} })},
		{"spec group", "bad group", spec(func(s *ViewSpec) { s.Group = "a b" })},
		{"spec layout", "unknown layout", spec(func(s *ViewSpec) { s.Layout = "html" })},
		{"spec title", "spec title has", spec(func(s *ViewSpec) { s.Title = long(61) })},
	}
	for _, c := range cases {
		err := c.b.Validate()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: %q does not say %q", c.name, err, c.msg)
		}
	}
	if err := (*ViewSpec)(nil).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("nil spec: %v", err)
	}
}
