package app

// HitTarget is one click target on screen (see hit_targets.go). For tests.
type HitTarget struct {
	// ID names the target; ProbeClick reports the same name for a click
	// that reaches it.
	ID string
	// Label is text the view draws inside the rect ("" = nothing to check).
	Label      string
	X, Y, W, H int
}

// HitTargets lists the click targets the mouse code knows about in the
// current state, with their screen rects. For tests.
func (m Model) HitTargets() []HitTarget {
	ts := m.hitTargets()
	out := make([]HitTarget, len(ts))
	for i, t := range ts {
		out[i] = HitTarget(t)
	}
	return out
}

// ProbeClick reports which target a left click at (x, y) would reach
// (a HitTarget ID, or another name such as "editor"), without acting
// on it. For tests.
func (m Model) ProbeClick(x, y int) string { return m.probeClick(x, y) }

// ExplorerWidth is the sidebar width in cells, its divider included
// (the splitter is its last column). For tests.
func (m Model) ExplorerWidth() int { return m.explorerWidth }
