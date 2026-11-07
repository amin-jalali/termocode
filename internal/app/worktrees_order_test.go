package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtGroupPaths are the root cards of group g, top to bottom.
func wtGroupPaths(m *Model, g string) []string {
	items, rows := m.wtList()
	var out []string
	for _, r := range rows {
		if r.item >= 0 && r.group == g && r.depth == 0 {
			out = append(out, items[r.item].e.Path)
		}
	}
	return out
}

// K moves the selected card one up in its group, J one down; the order
// is stored in worktrees.json (Order 1..n for the whole group) and the
// cursor stays on the card. At the top K does nothing.
func TestWorktreeReorderMovesCard(t *testing.T) {
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	before := wtGroupPaths(m, "ACTIVE")
	if len(before) < 2 {
		t.Fatalf("fixture ACTIVE has %d cards", len(before))
	}
	st.cursorPath = before[1]
	wtKey(t, m, "K")
	after := wtGroupPaths(m, "ACTIVE")
	if after[0] != before[1] || after[1] != before[0] {
		t.Fatalf("after K: %v (was %v)", after, before)
	}
	if st.cursorPath != before[1] {
		t.Errorf("cursor moved to %s", st.cursorPath)
	}
	metas, err := worktrees.Load(st.common)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range after {
		if metas[p].Order != i+1 {
			t.Errorf("%s: Order %d, want %d", p, metas[p].Order, i+1)
		}
	}
	// At the top: nothing changes.
	wtKey(t, m, "K")
	if got := wtGroupPaths(m, "ACTIVE"); got[0] != before[1] {
		t.Errorf("K at the top moved it: %v", got)
	}
	// J moves it back down.
	wtKey(t, m, "J")
	if got := wtGroupPaths(m, "ACTIVE"); got[0] != before[0] || got[1] != before[1] {
		t.Errorf("after J: %v, want %v", got, before)
	}
	// Other groups are left alone.
	if metas, _ := worktrees.Load(st.common); metas["/src/app.worktrees/wt-sidebar"].Order != 0 {
		t.Errorf("PINNED card got an order: %+v", metas["/src/app.worktrees/wt-sidebar"])
	}
}

// The stored order wins over the automatic sort inside a group; cards
// with no order follow, in the automatic sort.
func TestWorktreeOrderSortsItems(t *testing.T) {
	st, tabs := wtFixture(t)
	auto := wtItems(st.entries, tabs, st.others, st.root)
	var active []string
	for _, it := range auto {
		if it.group == "ACTIVE" {
			active = append(active, it.e.Path)
		}
	}
	if len(active) < 3 {
		t.Fatalf("fixture ACTIVE has %d cards", len(active))
	}
	last := active[len(active)-1]
	for i := range st.entries {
		if st.entries[i].Path == last {
			st.entries[i].Meta.Order = 1
		}
	}
	var got []string
	for _, it := range wtItems(st.entries, tabs, st.others, st.root) {
		if it.group == "ACTIVE" {
			got = append(got, it.e.Path)
		}
	}
	want := append([]string{last}, active[:len(active)-1]...)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

// Reset Order drops every stored order; a pin change drops the card's
// own order (it joins its new group in the automatic sort). The palette
// rows run from the AI without a card: only the list order changes.
func TestWorktreeOrderResetAndPolicies(t *testing.T) {
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	before := wtGroupPaths(m, "ACTIVE")
	st.cursorPath = before[1]
	if _, ok := m.dispatchWorktreePalette(wtMoveUpPaletteID); !ok {
		t.Fatal("move up not handled")
	}
	if got := wtGroupPaths(m, "ACTIVE"); got[0] != before[1] {
		t.Fatalf("palette move up: %v", got)
	}
	if _, ok := m.dispatchWorktreePalette(wtMoveDownPaletteID); !ok {
		t.Fatal("move down not handled")
	}
	if _, ok := m.dispatchWorktreePalette(wtMoveUpPaletteID); !ok {
		t.Fatal("move up not handled")
	}
	wtKey(t, m, "p")
	if metas, _ := worktrees.Load(st.common); metas[before[1]].Order != 0 || !metas[before[1]].Pinned {
		t.Errorf("pin kept the order: %+v", metas[before[1]])
	}
	if _, ok := m.dispatchWorktreePalette(wtResetOrderPaletteID); !ok {
		t.Fatal("reset not handled")
	}
	metas, _ := worktrees.Load(st.common)
	for p, md := range metas {
		if md.Order != 0 {
			t.Errorf("%s kept Order %d after reset", p, md.Order)
		}
	}
	for _, e := range st.entries {
		if e.Meta.Order != 0 {
			t.Errorf("%s kept Order %d in the list", e.Path, e.Meta.Order)
		}
	}
	for _, id := range []string{wtMoveUpPaletteID, wtMoveDownPaletteID, wtResetOrderPaletteID} {
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateAuto {
			t.Errorf("%s policy %+v", id, p)
		}
		found := false
		for _, it := range worktreePaletteItems() {
			found = found || it.ID == id
		}
		if !found {
			t.Errorf("%s not in the palette", id)
		}
	}
}
