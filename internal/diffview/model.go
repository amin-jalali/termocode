// Package diffview is the diff viewer and change-review card: a port of
// mobocode's DiffViewer + AI review card (lib/features/diff/diff_viewer.dart,
// lib/features/ai/ai_surface.dart) to the terminal.
//
// It shows one or more files as a side-by-side or unified diff with
// word-level highlights (internal/textdiff), long unchanged runs folded
// into "N unchanged lines" bands, an overview ruler and change
// navigation. In review mode every change (hunk) has Accept / Reject
// buttons and each file has Accept file / Reject file / Apply selected /
// Accept & edit; the card emits FinishMsg and the app applies it. With
// several files a list on the left steps through them.
//
// The model is pure: it never touches the disk or the editor.
package diffview

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/textdiff"
)

// Mode is the diff layout.
type Mode uint8

const (
	ModeAuto    Mode = iota // split when wide enough, else unified
	ModeSplit               // old left, new right
	ModeUnified             // one column, old / new line numbers
)

// Decision is the state of one change (hunk).
type Decision uint8

const (
	Pending Decision = iota
	Accepted
	Rejected
)

// File is one file to show.
type File struct {
	Name     string // display path
	Old, New string
	Tag      string // "new file", "delete" … shown next to the name

	// Commit mode (NewCommit) only.
	Status   byte   // 'A', 'M', 'D', 'R' (the list's status letter)
	Add, Del int    // line counts from git's numstat (shown before the text loads)
	Binary   bool   // no text diff: the body says so
	Pending  bool   // Old / New are not loaded yet: the body says Loading…
	Note     string // shown instead of the diff ("Too big to diff here")
}

// FinishMsg says the user finished a file: Accepted holds the accepted
// hunk indexes (empty = the whole change was rejected; an EOL-only
// change uses index 0). Edit asks the app to open the file afterwards so
// the user can change it by hand.
type FinishMsg struct {
	File     int
	Accepted map[int]bool
	Edit     bool
}

// HideMsg asks the app to hide the card (Esc). In review mode the review
// stays pending and can be shown again.
type HideMsg struct{}

type fileState struct {
	File
	diff    *textdiff.Diff
	dec     []Decision // per hunk; one entry for an EOL-only change
	done    bool
	summary string // shown once done ("Applied 2 of 3 changes")
	// expanded marks folded bands opened by the user (key: first row).
	expanded  map[int]bool
	expandAll bool
	scroll    int
	hscroll   int
	focus     int // focused hunk
}

// Model is the diff card.
type Model struct {
	title  string
	review bool
	files  []*fileState
	cur    int
	mode   Mode
	w, h   int // screen size

	// Commit mode (NewCommit): a header above the labels, footer buttons,
	// keys that emit ActionMsg. nil header: review / compare files.
	header   *Header
	buttons  []Button
	bodyOpen bool
	// bounds docks the card at a screen rectangle (w, h > 0); otherwise
	// it is centred on the screen.
	bx, by, bw, bh int

	// cache of the built display rows of the current file (a pointer:
	// View runs on value copies)
	cache *cache
}

// New builds a card. review shows the accept / reject controls.
func New(title string, files []File, review bool) Model {
	m := Model{title: title, review: review, cache: &cache{}}
	for _, f := range files {
		m.files = append(m.files, newFileState(f))
	}
	return m
}

func newFileState(f File) *fileState {
	if f.Pending || f.Binary || f.Note != "" {
		return &fileState{File: f, diff: textdiff.Compute("", ""), expanded: map[int]bool{}}
	}
	d := textdiff.Compute(f.Old, f.New)
	n := len(d.Hunks)
	if n == 0 && d.EOLOnly() {
		n = 1
	}
	return &fileState{File: f, diff: d, dec: make([]Decision, n), expanded: map[int]bool{}}
}

// AddFile appends a file (a later proposal of the same review) and
// returns its index.
func (m *Model) AddFile(f File) int {
	m.files = append(m.files, newFileState(f))
	return len(m.files) - 1
}

// SetSize sets the screen size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// Files is the number of files.
func (m Model) Files() int { return len(m.files) }

// Current is the index of the shown file.
func (m Model) Current() int { return m.cur }

// Review reports whether the card has accept / reject controls.
func (m Model) Review() bool { return m.review }

// Done reports whether file i was finished.
func (m Model) Done(i int) bool { return i >= 0 && i < len(m.files) && m.files[i].done }

// Pending is the number of files not finished yet.
func (m Model) Pending() int {
	n := 0
	for _, f := range m.files {
		if !f.done {
			n++
		}
	}
	return n
}

// Diff returns the computed diff of file i.
func (m Model) Diff(i int) *textdiff.Diff { return m.files[i].diff }

// Decisions returns the per-hunk decisions of file i (a copy).
func (m Model) Decisions(i int) []Decision {
	return append([]Decision(nil), m.files[i].dec...)
}

// MarkDone locks file i with a summary line and moves to the next file
// that is not done.
func (m *Model) MarkDone(i int, summary string) {
	if i < 0 || i >= len(m.files) {
		return
	}
	m.files[i].done = true
	m.files[i].summary = summary
	m.cache.reset()
	if i == m.cur {
		for k := 1; k <= len(m.files); k++ {
			j := (i + k) % len(m.files)
			if !m.files[j].done {
				m.cur = j
				break
			}
		}
	}
}

// SetMode sets the layout.
func (m *Model) SetMode(mode Mode) { m.mode = mode; m.cache.reset() }

// Select shows file i. In commit mode a file whose text is not loaded
// yet returns a Cmd that asks the app for it (LoadFileMsg).
func (m *Model) Select(i int) tea.Cmd {
	if i >= 0 && i < len(m.files) {
		m.cur = i
		m.cache.reset()
	}
	return m.loadCmd()
}

// SetDecision sets the decision of hunk h of the current file.
func (m *Model) SetDecision(h int, d Decision) {
	f := m.file()
	if f == nil || f.done || h < 0 || h >= len(f.dec) {
		return
	}
	f.dec[h] = d
}

func (m Model) file() *fileState {
	if m.cur < 0 || m.cur >= len(m.files) {
		return nil
	}
	return m.files[m.cur]
}

// accepted returns the accepted hunk set of f.
func accepted(f *fileState) map[int]bool {
	out := map[int]bool{}
	for i, d := range f.dec {
		if d == Accepted {
			out[i] = true
		}
	}
	return out
}

func allHunks(f *fileState) map[int]bool {
	out := map[int]bool{}
	for i := range f.dec {
		out[i] = true
	}
	return out
}

// finish emits FinishMsg for the current file.
func (m *Model) finish(acc map[int]bool, edit bool) tea.Cmd {
	f := m.file()
	if f == nil || f.done || !m.review {
		return nil
	}
	i := m.cur
	return func() tea.Msg { return FinishMsg{File: i, Accepted: acc, Edit: edit} }
}

// Action runs a named action (keys and buttons share these ids).
func (m *Model) Action(id string) tea.Cmd {
	f := m.file()
	switch id {
	case "hide":
		return func() tea.Msg { return HideMsg{} }
	case "mode":
		if m.splitNow() {
			m.mode = ModeUnified
		} else {
			m.mode = ModeSplit
		}
		m.cache.reset()
	case "mode-split":
		m.SetMode(ModeSplit)
	case "mode-unified":
		m.SetMode(ModeUnified)
	case "next":
		m.jump(1)
	case "prev":
		m.jump(-1)
	case "next-file":
		return m.stepFile(1)
	case "prev-file":
		return m.stepFile(-1)
	case "expand":
		if f != nil {
			f.expandAll = !f.expandAll
			m.cache.reset()
		}
	case "body":
		m.ToggleBody()
	default:
		if m.header != nil && id != "" {
			// Commit actions (copy, open, raw, revert …) are the app's.
			cur := m.cur
			return func() tea.Msg { return ActionMsg{ID: id, File: cur} }
		}
	}
	if f == nil || !m.review || f.done {
		return nil
	}
	switch id {
	case "accept-file":
		return m.finish(allHunks(f), false)
	case "reject-file":
		return m.finish(map[int]bool{}, false)
	case "apply":
		return m.finish(accepted(f), false)
	case "edit":
		acc := accepted(f)
		if len(acc) == 0 {
			acc = allHunks(f)
		}
		return m.finish(acc, true)
	case "accept":
		return m.toggle(f.focus, Accepted)
	case "reject":
		return m.toggle(f.focus, Rejected)
	}
	return nil
}

// toggle sets hunk h to d, or back to pending when it already is d. When
// every hunk is decided the file finishes on its own.
func (m *Model) toggle(h int, d Decision) tea.Cmd {
	f := m.file()
	if f == nil || f.done || !m.review || h < 0 || h >= len(f.dec) {
		return nil
	}
	if f.dec[h] == d {
		f.dec[h] = Pending
		return nil
	}
	f.dec[h] = d
	// Move the focus to the next undecided change.
	for k := 1; k <= len(f.dec); k++ {
		j := (h + k) % len(f.dec)
		if f.dec[j] == Pending {
			f.focus = j
			m.scrollToHunk(j)
			return nil
		}
	}
	return m.finish(accepted(f), false)
}

// AllDecided reports whether every hunk of the current file is decided.
func (m Model) AllDecided() bool {
	f := m.file()
	if f == nil {
		return false
	}
	for _, d := range f.dec {
		if d == Pending {
			return false
		}
	}
	return true
}

func (m *Model) stepFile(dir int) tea.Cmd {
	if len(m.files) < 2 {
		return nil
	}
	return m.Select((m.cur + dir + len(m.files)) % len(m.files))
}

// jump focuses the next / previous change and scrolls to it.
func (m *Model) jump(dir int) {
	f := m.file()
	if f == nil || len(f.diff.Hunks) == 0 {
		return
	}
	n := len(f.diff.Hunks)
	f.focus = (f.focus + dir + n) % n
	m.scrollToHunk(f.focus)
}

// scrollToHunk puts hunk h about a third down the body.
func (m *Model) scrollToHunk(h int) {
	f := m.file()
	if f == nil {
		return
	}
	g := m.geom()
	rows := m.vrows(g)
	for i, r := range rows {
		if r.kind == vHead && r.hunk == h {
			f.scroll = i - g.bodyH/3
			m.clampScroll(g)
			return
		}
	}
}

func (m *Model) clampScroll(g geom) {
	f := m.file()
	if f == nil {
		return
	}
	n := len(m.vrows(g))
	if mx := n - g.bodyH; f.scroll > mx {
		f.scroll = mx
	}
	if f.scroll < 0 {
		f.scroll = 0
	}
}

// Scroll moves the body by delta rows.
func (m *Model) Scroll(delta int) {
	f := m.file()
	if f == nil {
		return
	}
	f.scroll += delta
	m.clampScroll(m.geom())
}

func (m *Model) hScroll(delta int) {
	f := m.file()
	if f == nil {
		return
	}
	f.hscroll += delta
	if f.hscroll < 0 {
		f.hscroll = 0
	}
	if f.hscroll > 400 {
		f.hscroll = 400
	}
	m.cache.reset()
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	g := m.geom()
	if m.header != nil {
		if id, ok := commitKeys[k.String()]; ok {
			return m, m.Action(id)
		}
		switch k.String() {
		case "x", "a", "A", "R", "e":
			return m, nil
		}
	}
	switch k.String() {
	case "esc", "q":
		return m, m.Action("hide")
	case "down", "j", "ctrl+n":
		m.Scroll(1)
	case "up", "k", "ctrl+p":
		m.Scroll(-1)
	case "pgdown", "ctrl+d", " ":
		m.Scroll(max(1, g.bodyH-2))
	case "pgup", "ctrl+u":
		m.Scroll(-max(1, g.bodyH-2))
	case "home", "g":
		m.Scroll(-1 << 30)
	case "end", "G":
		m.Scroll(1 << 30)
	case "right", "l":
		m.hScroll(8)
	case "left", "h":
		m.hScroll(-8)
	case "n", "]", "J":
		m.jump(1)
	case "p", "[", "K":
		m.jump(-1)
	case "tab":
		return m, m.stepFile(1)
	case "shift+tab":
		return m, m.stepFile(-1)
	case "t", "v":
		return m, m.Action("mode")
	case "z":
		return m, m.Action("expand")
	case "a", "y":
		return m, m.Action("accept")
	case "r", "x":
		return m, m.Action("reject")
	case "A":
		return m, m.Action("accept-file")
	case "R":
		return m, m.Action("reject-file")
	case "enter":
		if !m.review {
			return m, m.Action("hide")
		}
		return m, m.Action("apply")
	case "e":
		return m, m.Action("edit")
	}
	return m, nil
}

// HandleMouse handles a mouse event at screen cell (x, y). inside
// reports whether it landed on the card.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd, bool) {
	l := m.layout(false)
	inside := x >= l.x && x < l.x+l.w && y >= l.y && y < l.y+l.h
	if !inside {
		return m, nil, false
	}
	switch button {
	case tea.MouseButtonWheelUp:
		m.Scroll(-3)
		return m, nil, true
	case tea.MouseButtonWheelDown:
		m.Scroll(3)
		return m, nil, true
	case tea.MouseButtonWheelLeft:
		m.hScroll(-8)
		return m, nil, true
	case tea.MouseButtonWheelRight:
		m.hScroll(8)
		return m, nil, true
	}
	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil, true
	}
	id := l.hitAt(x-l.x, y-l.y)
	return m, m.click(id), true
}

// ButtonAt returns the id of the control at screen cell (x, y) ("" when
// none). For tests and the app's mouse probe.
func (m Model) ButtonAt(x, y int) string {
	l := m.layout(false)
	return l.hitAt(x-l.x, y-l.y)
}

func (m *Model) click(id string) tea.Cmd {
	var n int
	switch {
	case id == "":
		return nil
	case scanID(id, "file:", &n):
		return m.Select(n)
	case scanID(id, "band:", &n):
		if f := m.file(); f != nil {
			f.expanded[n] = true
			m.cache.reset()
		}
		return nil
	case scanID(id, "hacc:", &n):
		m.focusHunk(n)
		return m.toggle(n, Accepted)
	case scanID(id, "hrej:", &n):
		m.focusHunk(n)
		return m.toggle(n, Rejected)
	case scanID(id, "hunk:", &n):
		m.focusHunk(n)
		return nil
	}
	return m.Action(id)
}

func (m *Model) focusHunk(h int) {
	if f := m.file(); f != nil && h >= 0 && h < len(f.dec) {
		f.focus = h
	}
}

// scanID parses "<prefix><int>".
func scanID(id, prefix string, n *int) bool {
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return false
	}
	v := 0
	for _, c := range id[len(prefix):] {
		if c < '0' || c > '9' {
			return false
		}
		v = v*10 + int(c-'0')
	}
	*n = v
	return true
}
