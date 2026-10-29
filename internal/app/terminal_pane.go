package app

// Terminal panes. A terminal tab shows one shell, or up to termPaneMax shells
// side by side or stacked. Pane 0 is the tab's own BufID (everything that
// talks to "the tab's terminal" keeps working); the other panes are in
// tab.Splits, in the order they were made. Each pane is a full nvim terminal
// buffer in its own nvim window inside the bottom panel.

// termPaneMax is the most panes one tab can hold.
const termPaneMax = 4

// Smallest pane that stays usable: columns x rows of shell content.
const (
	termPaneMinCols = 20
	termPaneMinRows = 4
)

// paneDir says how a pane was split off its parent.
type paneDir int

const (
	paneRight paneDir = iota // beside the parent
	paneDown                 // below the parent
)

// termPane is one extra shell in a tab (pane 1 and up).
type termPane struct {
	BufID int // nvim terminal buffer
	WinID int // nvim window showing it; 0 while the tab is not on show
	// Cwd is the folder the shell started in (the parent pane's folder),
	// then the folder shell integration reports (OSC 7).
	Cwd string
	// Dir and Parent record the shape: pane Parent (an index into the tab's
	// panes, 0 = the tab's own shell) was split in direction Dir.
	Dir    paneDir
	Parent int
	// Exited is set once the shell has ended; the pane stays until the user
	// restarts or closes it.
	Exited bool
	// Busy and LastCmd come from shell integration (OSC 133), like the
	// tab's own fields for pane 0: a command runs, and its command line.
	Busy    bool
	LastCmd string
}

// paneCount is the number of panes in the tab (always at least 1).
func (t terminalTab) paneCount() int { return 1 + len(t.Splits) }

// paneBuf returns the nvim buffer of pane i, or 0 when i is out of range.
func (t terminalTab) paneBuf(i int) int {
	switch {
	case i == 0:
		return t.BufID
	case i > 0 && i <= len(t.Splits):
		return t.Splits[i-1].BufID
	}
	return 0
}

// paneCwd returns the folder pane i started in (pane 0: the tab's folder).
func (t terminalTab) paneCwd(i int) string {
	if i > 0 && i <= len(t.Splits) {
		return t.Splits[i-1].Cwd
	}
	return t.Cwd
}

// focusedPane returns the focused pane index, clamped into range.
func (t terminalTab) focusedPane() int {
	return min(max(t.PaneFocus, 0), len(t.Splits))
}

// focusedBuf is the buffer of the focused pane: where typing goes.
func (t terminalTab) focusedBuf() int { return t.paneBuf(t.focusedPane()) }

// paneBufs lists the buffers of all panes, pane 0 first.
func (t terminalTab) paneBufs() []int {
	out := make([]int, 0, t.paneCount())
	for i := 0; i < t.paneCount(); i++ {
		out = append(out, t.paneBuf(i))
	}
	return out
}

// splitRefusal returns why a pane of winW x winH cells cannot be split in
// direction dir (with count panes already in the tab), or "" when it can. The
// text is a hint for the user. A pane must keep termPaneMinCols x
// termPaneMinRows of shell content; each pane also spends one row on its
// title and one column on the divider.
func splitRefusal(dir paneDir, winW, winH, count int) string {
	if count >= termPaneMax {
		return "A terminal tab holds at most 4 panes. Close one first."
	}
	if dir == paneRight {
		if winW < 2*termPaneMinCols+1 {
			return "Too narrow to split: each pane needs 20 columns. Make the panel wider or split down."
		}
		return ""
	}
	// Down: each pane needs its title row plus the minimum rows, and the
	// divider (the next pane's title) is inside that.
	if winH < 2*(termPaneMinRows+1) {
		return "Too short to split: each pane needs 4 rows. Make the panel taller or split right."
	}
	return ""
}

// withoutPane returns tab t with pane i taken out (the bookkeeping only: the
// nvim window and buffer are closed by the caller). Removing pane 0 promotes
// the next pane to pane 0, so BufID is always a live pane. The focus moves to
// the pane before the removed one. t must have at least two panes.
func (t terminalTab) withoutPane(i int) terminalTab {
	if t.paneCount() < 2 || i < 0 || i >= t.paneCount() {
		return t
	}
	panes := append([]termPane{{BufID: t.BufID, Cwd: t.Cwd}}, t.Splits...)
	removedParent := panes[i].Parent
	panes = append(panes[:i:i], panes[i+1:]...)
	for k := range panes {
		switch p := panes[k].Parent; {
		case p == i:
			panes[k].Parent = removedParent
		case p > i:
			panes[k].Parent = p - 1
		}
		if panes[k].Parent >= k { // keep every parent earlier than its child
			panes[k].Parent = max(k-1, 0)
		}
	}
	t.BufID = panes[0].BufID
	t.Splits = panes[1:]
	if len(t.Splits) == 0 {
		t.Splits = nil
	}
	t.PaneFocus = max(i-1, 0)
	return t
}
