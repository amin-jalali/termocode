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
