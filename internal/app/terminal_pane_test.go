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
