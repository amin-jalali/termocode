package explorer

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// OpenFileMsg asks the host to open Path in the editor. KeepFocus = true
// means "open as a preview but leave focus in the explorer" (single mouse
// click). When false, focus shifts to the editor (Enter / palette / etc.).
type OpenFileMsg struct {
	Path      string
	KeepFocus bool
}

type Model struct {
	root       *Node
	extraRoots []*Node // additional workspace roots, rendered after `root`
	cursor     int
	top        int
	w, h       int
	err        string
	showHidden bool // when true, dotfiles/dotdirs are listed

	// Header / footer metadata. Wired by the host via SetMeta(...) before
	// each render; never derived from disk inside the explorer.
	metaWorkspace  string
	metaBranch     string
	metaIsRepo     bool
	metaTotalFiles int

	// Multi-selection state. selected is the set of absolute paths the user
	// has queued up via Ctrl+click / Shift+click / Ctrl+Space / Shift+Arrow.
	// anchorPath is the row Shift-range selection extends FROM — typically
	// set to the most recent "primary" click / Ctrl+toggle. Empty selection
	// means "no multi-select active"; the cursor row alone is the implied
	// target for single-row actions (Delete, etc).
	selected   map[string]struct{}
	anchorPath string
}

func New() Model {
	cwd, err := os.Getwd()
	if err != nil {
		return Model{err: err.Error()}
	}
	root, err := NewRoot(cwd)
	if err != nil {
		return Model{err: err.Error()}
	}
	return Model{root: root}
}

func (m Model) Init() tea.Cmd { return nil }

// ExpandedPaths returns absolute paths of every currently-expanded dir
// across every workspace root (primary + extras).
func (m Model) ExpandedPaths() []string {
	var out []string
	for _, r := range m.allRoots() {
		out = append(out, r.ExpandedPaths()...)
	}
	return out
}

// ExpandPaths re-expands the listed directories (lazy-loading on demand)
// across every workspace root so a previous session's tree state can be
// restored. Paths that don't live under any root are silently ignored.
func (m *Model) ExpandPaths(paths []string) {
	roots := m.allRoots()
	if len(roots) == 0 || len(paths) == 0 {
		return
	}
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	for _, r := range roots {
		r.ExpandPaths(set)
	}
	m.scrollIntoView()
}

func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.scrollIntoView()
}

func (m Model) visible() []VisibleEntry {
	roots := m.allRoots()
	if len(roots) == 0 {
		return nil
	}
	// Build a "is a root node" set so the dotfile filter never hides any
	// root's title row (multi-root: an extra root could legitimately have a
	// dot-prefixed basename).
	isRoot := make(map[*Node]bool, len(roots))
	for _, r := range roots {
		isRoot[r] = true
	}
	var all []VisibleEntry
	for _, r := range roots {
		all = append(all, r.Visible()...)
	}
	if m.showHidden {
		return all
	}
	out := make([]VisibleEntry, 0, len(all))
	for _, e := range all {
		if !isRoot[e.Node] && strings.HasPrefix(e.Node.Name, ".") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ToggleHidden flips visibility of dotfiles in the tree. Returns the new
// state via HiddenShown so callers can surface a toast.
func (m *Model) ToggleHidden() {
	m.showHidden = !m.showHidden
	m.scrollIntoView()
}

// HiddenShown reports whether dotfiles are currently visible in the tree.
func (m Model) HiddenShown() bool { return m.showHidden }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		return m.handleKey(k)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	vis := m.visible()
	if len(vis) == 0 {
		return m, nil
	}
	key := msg.String()
	// Selection-affecting keys: shift+up/down extend the multi-selection
	// from anchorPath to the new cursor row; ctrl+space toggles the cursor
	// row; ctrl+a selects every visible row; escape clears the selection.
	switch key {
	case "shift+down":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
		m.applyShiftRange(vis)
		m.scrollIntoView()
		return m, nil
	case "shift+up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.applyShiftRange(vis)
		m.scrollIntoView()
		return m, nil
	case "ctrl+@", "ctrl+ ":
		// Bubble Tea reports Ctrl+Space as "ctrl+@" on most terminals
		// (the NUL byte) and "ctrl+ " on a few. Both toggle the cursor
		// row in the multi-select set without losing the existing one.
		path := vis[m.cursor].Node.Path
		m.toggleSelection(path)
		m.anchorPath = path
		return m, nil
	case "ctrl+a":
		m.selectAllVisible(vis)
		m.anchorPath = vis[m.cursor].Node.Path
		return m, nil
	case "esc", "escape":
		if len(m.selected) > 0 {
			m.selected = nil
			return m, nil
		}
	}
	switch key {
	case "j", "down":
		// A plain arrow / motion clears the multi-selection so the user
		// gets the "click somewhere fresh" semantics they expect when
		// they're done with a selection.
		m.selected = nil
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
		m.anchorPath = vis[m.cursor].Node.Path
	case "k", "up":
		m.selected = nil
		if m.cursor > 0 {
			m.cursor--
		}
		m.anchorPath = vis[m.cursor].Node.Path
	case "g", "home":
		m.selected = nil
		m.cursor = 0
		m.anchorPath = vis[m.cursor].Node.Path
	case "G", "end":
		m.selected = nil
		m.cursor = len(vis) - 1
		m.anchorPath = vis[m.cursor].Node.Path
	case "enter", " ":
		node := vis[m.cursor].Node
		m.selected = nil
		m.anchorPath = node.Path
		if node.IsDir {
			_ = node.Toggle()
		} else {
			path := node.Path
			cmd := func() tea.Msg { return OpenFileMsg{Path: path} }
			m.scrollIntoView()
			return m, cmd
		}
	}
	m.scrollIntoView()
	return m, nil
}

// Reload rebuilds every root from disk, preserving the set of currently
// expanded directories where possible.
func (m *Model) Reload() {
	roots := m.allRoots()
	if len(roots) == 0 {
		return
	}
	expanded := map[string]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		if n.IsDir && n.Expanded {
			expanded[n.Path] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}

	var reexpand func(*Node)
	reexpand = func(n *Node) {
		if n.IsDir && expanded[n.Path] {
			if !n.Expanded {
				_ = n.Toggle()
			}
			for _, c := range n.Children {
				reexpand(c)
			}
		}
	}

	// Primary root: rebuild from cwd. Falls back to its existing path on
	// getcwd error so the root doesn't disappear.
	if m.root != nil {
		base := m.root.Path
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		}
		if newRoot, err := NewRoot(base); err == nil {
			reexpand(newRoot)
			m.root = newRoot
		}
	}
	// Extra roots: rebuild each from its on-disk path. Drop entries whose
	// directory has gone away so the tree doesn't carry zombie sections.
	rebuilt := make([]*Node, 0, len(m.extraRoots))
	for _, old := range m.extraRoots {
		if old == nil {
			continue
		}
		if newRoot, err := NewRoot(old.Path); err == nil {
			reexpand(newRoot)
			rebuilt = append(rebuilt, newRoot)
		}
	}
	m.extraRoots = rebuilt

	if c := len(m.visible()); m.cursor >= c {
		if c > 0 {
			m.cursor = c - 1
		} else {
			m.cursor = 0
		}
	}
}

// CountVisibleFiles returns the number of file (non-directory) entries
// currently visible in the tree. Folders aren't counted.
func (m Model) CountVisibleFiles() int {
	n := 0
	for _, e := range m.visible() {
		if !e.Node.IsDir {
			n++
		}
	}
	return n
}

// HeaderChromeRows reports how many rows at the TOP of the panel the
// explorer reserves for its own header chrome (header pad + label +
// header pad + divider). The host uses this to translate a panel-local
// click Y into a body-local row index.
//
// Mirrors the chrome budget logic in view.go::View — keep these two in
// lockstep or mouse hit-testing drifts.
func (m Model) HeaderChromeRows() int {
	switch {
	case m.h >= 12:
		return 2 // header + hairline (▁ underline)
	case m.h >= 6:
		return 2 // header + divider
	}
	return 0
}

// FooterChromeRows reports the row count reserved at the BOTTOM of the
// panel (footer divider + footer line). Used so clicks on the footer
// row don't get misinterpreted as a tail-of-tree click.
func (m Model) FooterChromeRows() int {
	switch {
	case m.h >= 12, m.h >= 6:
		return 2 // divider + footer
	}
	return 0
}

// VisibleAt returns the node currently rendered at PANEL-LOCAL row y, if
// any. Header / footer rows return (nil, false).
func (m Model) VisibleAt(y int) (*Node, bool) {
	bodyY := y - m.HeaderChromeRows()
	bodyH := m.h - m.HeaderChromeRows() - m.FooterChromeRows()
	if bodyY < 0 || bodyY >= bodyH {
		return nil, false
	}
	vis := m.visible()
	idx := m.top + bodyY
	if idx < 0 || idx >= len(vis) {
		return nil, false
	}
	return vis[idx].Node, true
}

// HandleMouse takes a PANEL-LOCAL (x, y) click. The explorer accounts
// for its own header / footer chrome internally so the host can pass
// the raw panel-local row index without knowing the chrome layout.
//
// ctrl / shift are the modifier flags from the underlying tea.MouseMsg.
// Ctrl+Left toggles the clicked row in the multi-selection set; Shift+Left
// extends the selection from the last anchor to the clicked row. Plain
// MouseLeft clears the multi-selection and behaves like a normal click
// (open file / toggle dir).
func (m Model) HandleMouse(x, y int, t tea.MouseEventType, ctrl, shift bool) (Model, tea.Cmd) {
	vis := m.visible()
	switch t {
	case tea.MouseWheelUp:
		m.top -= 3
		if m.top < 0 {
			m.top = 0
		}
		return m, nil
	case tea.MouseWheelDown:
		// Visible body height excludes the header / footer chrome rows
		// (m.h includes them). The previous clamp used m.h directly,
		// which stopped m.top growing 4-6 rows short of the last entry —
		// the user could see those rows getting cut off by the footer
		// but never scroll them into the visible body.
		bodyH := m.h - m.HeaderChromeRows() - m.FooterChromeRows()
		if bodyH < 1 {
			bodyH = 1
		}
		maxTop := len(vis) - bodyH
		if maxTop < 0 {
			maxTop = 0
		}
		m.top += 3
		if m.top > maxTop {
			m.top = maxTop
		}
		return m, nil
	case tea.MouseLeft:
		bodyY := y - m.HeaderChromeRows()
		bodyH := m.h - m.HeaderChromeRows() - m.FooterChromeRows()
		if bodyY < 0 || bodyY >= bodyH {
			return m, nil
		}
		idx := m.top + bodyY
		if idx < 0 || idx >= len(vis) {
			return m, nil
		}
		node := vis[idx].Node
		path := node.Path

		// Modifier-aware paths run BEFORE the "open file / toggle dir"
		// fallthrough so Ctrl-clicking a folder doesn't expand it and
		// Shift-clicking doesn't open the file.
		if ctrl {
			m.cursor = idx
			m.toggleSelection(path)
			m.anchorPath = path
			return m, nil
		}
		if shift {
			// Range select from anchor to the clicked row.
			m.cursor = idx
			m.applyShiftRange(vis)
			return m, nil
		}

		// Plain click — clear any prior multi-selection, set cursor +
		// anchor to the new row, then perform the row's primary action.
		m.selected = nil
		m.cursor = idx
		m.anchorPath = path
		if node.IsDir {
			_ = node.Toggle()
			return m, nil
		}
		// KeepFocus=true: file opens in editor as a preview, but focus
		// stays in the explorer so arrow keys keep navigating the tree.
		return m, func() tea.Msg { return OpenFileMsg{Path: path, KeepFocus: true} }
	}
	return m, nil
}

// RevealPath expands the necessary ancestor directories so that path becomes
// visible in the tree, then moves the cursor to that row and scrolls it into
// view. If path is empty, not under any current root, or cannot be located in
// the tree (e.g. excluded by ignore rules), the call is a no-op.
func (m *Model) RevealPath(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	r := m.findRootForPath(abs)
	if r == nil {
		return
	}
	// Walk down from the matched root, expanding dirs along the path until
	// we find the node whose Path equals abs.
	cur := r
	for cur.Path != abs {
		if !cur.IsDir {
			return
		}
		if !cur.Loaded {
			if err := cur.Load(); err != nil {
				return
			}
		}
		if !cur.Expanded {
			cur.Expanded = true
		}
		var next *Node
		for _, c := range cur.Children {
			cp := c.Path
			if cp == abs || strings.HasPrefix(abs, cp+string(filepath.Separator)) {
				next = c
				break
			}
		}
		if next == nil {
			return
		}
		cur = next
	}
	// Locate the row index in the now-current visible list.
	vis := m.visible()
	for i, e := range vis {
		if e.Node == cur {
			m.cursor = i
			m.scrollIntoView()
			return
		}
	}
}

// ─── Multi-selection API ──────────────────────────────────────────────────

// Selection returns the absolute paths currently in the multi-select set,
// in arbitrary (map iteration) order. Callers that need a stable order
// should sort the result.
func (m Model) Selection() []string {
	if len(m.selected) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.selected))
	for p := range m.selected {
		out = append(out, p)
	}
	return out
}

// CursorPath returns the absolute path of the row the cursor is currently
// on, or "" if the tree is empty / not initialised.
func (m Model) CursorPath() string {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return ""
	}
	return vis[m.cursor].Node.Path
}

// IsSelected reports whether the given absolute path is currently in the
// multi-select set.
func (m Model) IsSelected(path string) bool {
	if len(m.selected) == 0 {
		return false
	}
	_, ok := m.selected[path]
	return ok
}

// ClearSelection drops every entry from the multi-select set. The cursor
// row is left where it is; the next render will paint it as the only
// highlighted row.
func (m *Model) ClearSelection() {
	m.selected = nil
}

// ToggleSelection adds path to the multi-select set if it's missing,
// removes it if it's present. Anchor is updated so a follow-up Shift
// range starts from this row.
func (m *Model) ToggleSelection(path string) {
	if path == "" {
		return
	}
	m.toggleSelection(path)
	m.anchorPath = path
}

// SelectRange clears the current selection and adds every visible row
// between anchor and target (inclusive, by visible-row index, in either
// direction). The cursor and anchorPath are NOT moved — callers control
// those independently.
func (m *Model) SelectRange(anchor, target string) {
	vis := m.visible()
	ai, ti := -1, -1
	for i, e := range vis {
		if e.Node.Path == anchor {
			ai = i
		}
		if e.Node.Path == target {
			ti = i
		}
	}
	if ai < 0 || ti < 0 {
		return
	}
	if ti < ai {
		ai, ti = ti, ai
	}
	m.selected = make(map[string]struct{}, ti-ai+1)
	for i := ai; i <= ti; i++ {
		m.selected[vis[i].Node.Path] = struct{}{}
	}
}

// toggleSelection adds path if missing, removes if present. Called by
// the public ToggleSelection and the Ctrl+click / Ctrl+Space handlers.
func (m *Model) toggleSelection(path string) {
	if m.selected == nil {
		m.selected = make(map[string]struct{})
	}
	if _, ok := m.selected[path]; ok {
		delete(m.selected, path)
		return
	}
	m.selected[path] = struct{}{}
}

// applyShiftRange replaces the current multi-selection with every visible
// row between anchorPath and the cursor row (inclusive, in either
// direction). If anchorPath is unset or no longer visible we fall back to
// the cursor row alone.
func (m *Model) applyShiftRange(vis []VisibleEntry) {
	if len(vis) == 0 {
		m.selected = nil
		return
	}
	if m.cursor < 0 || m.cursor >= len(vis) {
		return
	}
	target := vis[m.cursor].Node.Path
	anchorIdx := -1
	if m.anchorPath != "" {
		for i, e := range vis {
			if e.Node.Path == m.anchorPath {
				anchorIdx = i
				break
			}
		}
	}
	if anchorIdx < 0 {
		// No anchor (or anchor scrolled off the visible list) — anchor
		// the range to the current cursor row so the user gets at least
		// the cursor cell selected.
		m.anchorPath = target
		anchorIdx = m.cursor
	}
	lo, hi := anchorIdx, m.cursor
	if hi < lo {
		lo, hi = hi, lo
	}
	m.selected = make(map[string]struct{}, hi-lo+1)
	for i := lo; i <= hi; i++ {
		m.selected[vis[i].Node.Path] = struct{}{}
	}
}

// selectAllVisible adds every currently-visible row (after the dotfile
// filter) to the multi-select set. Cheap O(N) walk; the user explicitly
// asked for it via Ctrl+A.
func (m *Model) selectAllVisible(vis []VisibleEntry) {
	if len(vis) == 0 {
		return
	}
	m.selected = make(map[string]struct{}, len(vis))
	for _, e := range vis {
		m.selected[e.Node.Path] = struct{}{}
	}
}

func (m *Model) scrollIntoView() {
	if m.h <= 0 {
		return
	}
	// Body height excludes the header/footer chrome rows. Using m.h directly
	// here let the cursor advance past the last visible body row without
	// scrolling, leaving the bottom 4 rows of the tree permanently hidden
	// under the footer when navigating via keyboard. Mirror the mouse-wheel
	// and view.go math instead.
	bodyH := m.h - m.HeaderChromeRows() - m.FooterChromeRows()
	if bodyH < 1 {
		bodyH = 1
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+bodyH {
		m.top = m.cursor - bodyH + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}
