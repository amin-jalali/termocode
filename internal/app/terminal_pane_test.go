package app

import "testing"

func TestTerminalTabPanes(t *testing.T) {
	tab := terminalTab{BufID: 5, Cwd: "/a"}
	if tab.paneCount() != 1 || tab.focusedBuf() != 5 {
		t.Fatalf("single tab: count=%d focused=%d", tab.paneCount(), tab.focusedBuf())
	}
	tab.Splits = []termPane{{BufID: 7, Cwd: "/b"}, {BufID: 9, Cwd: "/c"}}
	tab.PaneFocus = 2
	if tab.paneCount() != 3 || tab.focusedBuf() != 9 || tab.paneCwd(1) != "/b" || tab.paneCwd(0) != "/a" {
		t.Fatalf("three panes: count=%d focused=%d", tab.paneCount(), tab.focusedBuf())
	}
	if got := tab.paneBufs(); len(got) != 3 || got[0] != 5 || got[1] != 7 || got[2] != 9 {
		t.Fatalf("paneBufs = %v", got)
	}
	tab.PaneFocus = 9 // out of range is clamped, never a panic
	if tab.focusedPane() != 2 || tab.paneBuf(9) != 0 {
		t.Fatalf("clamp: focus=%d buf=%d", tab.focusedPane(), tab.paneBuf(9))
	}
}

func TestSplitRefusal(t *testing.T) {
	cases := []struct {
		name         string
		dir          paneDir
		w, h, count  int
		wantRefusing bool
	}{
		{"right fits", paneRight, 41, 10, 1, false},
		{"right too narrow", paneRight, 40, 10, 1, true},
		{"down fits", paneDown, 80, 10, 1, false},
		{"down too short", paneDown, 80, 9, 1, true},
		{"fifth pane", paneRight, 200, 30, 4, true},
		{"fourth pane", paneRight, 200, 30, 3, false},
	}
	for _, c := range cases {
		got := splitRefusal(c.dir, c.w, c.h, c.count)
		if (got != "") != c.wantRefusing {
			t.Errorf("%s: refusal %q, want refusing=%v", c.name, got, c.wantRefusing)
		}
	}
}

func TestWithoutPane(t *testing.T) {
	base := terminalTab{BufID: 1, Cwd: "/a", Splits: []termPane{
		{BufID: 2, Dir: paneRight, Parent: 0},
		{BufID: 3, Dir: paneDown, Parent: 1},
		{BufID: 4, Dir: paneDown, Parent: 0},
	}}
	got := base.withoutPane(2) // the middle pane (buf 3)
	if bufs := got.paneBufs(); len(bufs) != 3 || bufs[0] != 1 || bufs[1] != 2 || bufs[2] != 4 {
		t.Fatalf("bufs = %v", bufs)
	}
	if got.PaneFocus != 1 {
		t.Errorf("focus = %d, want the pane before the removed one", got.PaneFocus)
	}
	for k, s := range got.Splits {
		if s.Parent > k { // pane k+1 must have a parent earlier than itself
			t.Errorf("split %d parent %d is not earlier", k+1, s.Parent)
		}
	}
	first := base.withoutPane(0) // pane 0 closes: the next one takes over
	if first.BufID != 2 || first.paneCount() != 3 || first.PaneFocus != 0 {
		t.Fatalf("promote: buf=%d panes=%d focus=%d", first.BufID, first.paneCount(), first.PaneFocus)
	}
	one := terminalTab{BufID: 1}
	if got := one.withoutPane(0); got.BufID != 1 {
		t.Error("the only pane is never removed here (the tab closes instead)")
	}
	last := terminalTab{BufID: 1, Splits: []termPane{{BufID: 2}}}.withoutPane(1)
	if last.paneCount() != 1 || last.BufID != 1 || last.Splits != nil {
		t.Errorf("back to one pane: %+v", last)
	}
}
