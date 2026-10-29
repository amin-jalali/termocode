package keyhint

import (
	"reflect"
	"testing"
)

var testMaps = []Mapping{
	{LHS: "<Space>f", Desc: "Format the buffer"},
	{LHS: "<Space>x", RHS: "<Cmd>DbExplain<CR>"},
	{LHS: "<Space>gs", Desc: "Stage"},
	{LHS: "<Space>gp", Desc: "Push"},
	{LHS: "gd", Desc: "Definition"},
}

// Spec: "<Name>" is one key and compares without case.
func TestTokens(t *testing.T) {
	got := Tokens("<Space>f<C-x>\\")
	want := []string{"<space>", "f", "<c-x>", "\\"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens = %q, want %q", got, want)
	}
	if got := Tokens("a<b"); !reflect.DeepEqual(got, []string{"a", "<", "b"}) {
		t.Errorf("lone < = %q", got)
	}
}

// Spec: under a prefix, each next key shows once; a key with longer
// mappings below it is a group with a count; the text falls back from
// desc to the right side.
func TestEntries(t *testing.T) {
	got := Entries(testMaps, []string{"<space>"})
	want := []Entry{
		{Key: "f", Desc: "Format the buffer"},
		{Key: "g", Desc: "+2 more", Group: true, Count: 2},
		{Key: "x", Desc: "DbExplain"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Entries = %+v\nwant %+v", got, want)
	}
	deeper := Entries(testMaps, []string{"<space>", "g"})
	if len(deeper) != 2 || deeper[0].Key != "p" || deeper[1].Key != "s" {
		t.Errorf("level 2 = %+v", deeper)
	}
	if got := Entries(testMaps, []string{"z"}); len(got) != 0 {
		t.Errorf("unknown prefix = %+v", got)
	}
}
