package app

import (
	"testing"

	"termocode/internal/nvim"
)

func TestReorderPinned_NoPins_ReturnsAsIs(t *testing.T) {
	m := &Model{}
	in := []nvim.BufferInfo{{ID: 1}, {ID: 2}, {ID: 3}}
	out := m.reorderPinned(in)
	if len(out) != 3 || out[0].ID != 1 || out[2].ID != 3 {
		t.Errorf("no-pins should pass through unchanged: %v", out)
	}
}

func TestReorderPinned_PinnedMoveToFront(t *testing.T) {
	m := &Model{
		pinnedBufs: map[int]int{3: 1}, // pin id=3 first
	}
	in := []nvim.BufferInfo{{ID: 1}, {ID: 2}, {ID: 3}}
	out := m.reorderPinned(in)
	if out[0].ID != 3 {
		t.Errorf("pinned id=3 should be first, got %v", out)
	}
	// Order of unpinned preserved: 1, 2
	if out[1].ID != 1 || out[2].ID != 2 {
		t.Errorf("unpinned order should be preserved: %v", out)
	}
}

func TestReorderPinned_MultiplePinsKeepPinOrder(t *testing.T) {
	m := &Model{
		// id=1 pinned second, id=3 pinned first → 3 should appear before 1
		pinnedBufs: map[int]int{1: 2, 3: 1},
	}
	in := []nvim.BufferInfo{{ID: 1}, {ID: 2}, {ID: 3}}
	out := m.reorderPinned(in)
	if out[0].ID != 3 || out[1].ID != 1 {
		t.Errorf("pinned by pin-order: %v, want [3,1,2]", out)
	}
	if out[2].ID != 2 {
		t.Errorf("unpinned should follow: %v", out)
	}
}

func TestReorderPinned_GarbageCollectsClosedBufs(t *testing.T) {
	m := &Model{
		pinnedBufs: map[int]int{1: 1, 99: 2}, // 99 is no longer in the list
	}
	in := []nvim.BufferInfo{{ID: 1}, {ID: 2}}
	_ = m.reorderPinned(in)
	if _, exists := m.pinnedBufs[99]; exists {
		t.Errorf("closed buffer 99 should be GC'd from pinned map: %v", m.pinnedBufs)
	}
	if _, exists := m.pinnedBufs[1]; !exists {
		t.Errorf("active pinned 1 should remain")
	}
}

func TestIsPinned(t *testing.T) {
	m := Model{pinnedBufs: map[int]int{5: 1}}
	if !m.isPinned(5) {
		t.Errorf("isPinned(5) should be true")
	}
	if m.isPinned(99) {
		t.Errorf("isPinned(99) should be false")
	}
}

func TestSplitOnUnescapedBar(t *testing.T) {
	cases := []struct {
		in       string
		wantA    string
		wantB    string
	}{
		{"key|value", "key", "value"},
		{"key|val\\|ue", "key", "val\\|ue"},
		{"only", "only", ""},
		{"|empty", "", "empty"},
	}
	for _, c := range cases {
		a, b := splitOnUnescapedBar(c.in)
		if a != c.wantA || b != c.wantB {
			t.Errorf("splitOnUnescapedBar(%q) = (%q, %q), want (%q, %q)", c.in, a, b, c.wantA, c.wantB)
		}
	}
}

func TestUnescapeSnippetBody(t *testing.T) {
	cases := map[string]string{
		"plain":              "plain",
		`a\nb`:               "a\nb",
		`a\|b`:               "a|b",
		`back\\slash`:        `back\slash`,
		`tab\nnext\nthird`:   "tab\nnext\nthird",
	}
	for in, want := range cases {
		if got := unescapeSnippetBody(in); got != want {
			t.Errorf("unescapeSnippetBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"single":            "single",
		"first\nsecond":     "first",
		"\nleading newline": "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"one\ntwo\nthree", []string{"one", "two", "three"}},
		{"trailing\n", []string{"trailing"}},
		{"a", []string{"a"}},
	}
	for _, c := range cases {
		got := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q): len=%d, want %d (got %v)", c.in, len(got), len(c.want), got)
			continue
		}
		for i, line := range got {
			if line != c.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", c.in, i, line, c.want[i])
			}
		}
	}
}
