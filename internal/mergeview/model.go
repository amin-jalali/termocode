// Package mergeview is the three-way merge view of one conflicted file:
// base, ours and theirs side by side (read-only), with the conflict
// blocks of the result numbered and highlighted. The result itself is the
// real file in the editor; this package only works out which lines of
// each side belong to which block and what a "Take" puts in the result.
// It is pure: no git process, no editor.
package mergeview

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/textdiff"
)

// Pane is one of the three read-only panes.
type Pane int

const (
	PaneBase Pane = iota
	PaneOurs
	PaneTheirs
	paneCount
)

// Title is the pane's name.
func (p Pane) Title() string {
	return [...]string{"Base", "Ours", "Theirs"}[p]
}

// Block is one conflict of the result and where it sits in each side.
type Block struct {
	Conflict git.ConflictBlock // in the result's lines (0-based)
	// Ranges are the block's lines in each pane: [start, end), 0-based.
	Ranges [paneCount][2]int
}

// Model is the merge view of one file.
type Model struct {
	path   string
	labels [paneCount]string // "HEAD", "feature", …
	sides  [paneCount][]string
	noBase bool // both sides added the file: there is no base
	// oursToBase maps ours lines to base lines (base never changes).
	oursToBase []int

	result []string
	blocks []Block
	cur    int

	pane    Pane // the pane shown when only one fits
	narrow  bool
	scroll  int // extra rows scrolled past the current block's spot
	w, h    int
	focused bool
}

// New builds the view of path from its index stages (a text merge).
func New(path string, st git.ConflictStages) Model {
	m := Model{path: path, noBase: !st.Base.Present}
	m.sides[PaneBase] = git.SplitLines(st.Base.Text)
	m.sides[PaneOurs] = git.SplitLines(st.Ours.Text)
	m.sides[PaneTheirs] = git.SplitLines(st.Theirs.Text)
	m.oursToBase = anchors(m.sides[PaneOurs], m.sides[PaneBase])
	m.pane = PaneOurs
	return m
}

// Path is the file's path.
func (m Model) Path() string { return m.path }

// Blocks are the result's conflicts, in order.
func (m Model) Blocks() []Block { return m.blocks }

// Current is the index of the current block (0 when there is none).
func (m Model) Current() int { return m.cur }

// CurrentBlock is the current block, if any.
func (m Model) CurrentBlock() (Block, bool) {
	if m.cur < 0 || m.cur >= len(m.blocks) {
		return Block{}, false
	}
	return m.blocks[m.cur], true
}

// Side is a pane's lines.
func (m Model) Side(p Pane) []string { return m.sides[p] }

// Label is the branch label git wrote for a pane ("" when unknown).
func (m Model) Label(p Pane) string { return m.labels[p] }

// SetResult takes the result's current lines (the editor buffer) and
// works out its blocks again. The current block keeps its number when it
// can.
func (m *Model) SetResult(lines []string) {
	m.result = append(m.result[:0], lines...)
	cs := git.ParseConflictLines(m.result)
	m.blocks = make([]Block, len(cs))
	views := [paneCount][]string{}
	starts := [paneCount][]int{}
	for p := PaneOurs; p < paneCount; p++ {
		views[p], starts[p] = sideView(m.result, cs, p)
	}
	baseView, baseStarts := sideView(m.result, cs, PaneBase)
	var toStage [paneCount][]int
	for p := PaneOurs; p < paneCount; p++ {
		toStage[p] = anchors(views[p], m.sides[p])
	}
	baseAnch := anchors(baseView, m.sides[PaneBase])
	for i, c := range cs {
		b := Block{Conflict: c}
		for p := PaneOurs; p < paneCount; p++ {
			s := starts[p][i]
			b.Ranges[p] = mapRange(toStage[p], len(m.sides[p]), s, s+len(sideLines(c, p)))
		}
		if c.Base >= 0 {
			s := baseStarts[i]
			b.Ranges[PaneBase] = mapRange(baseAnch, len(m.sides[PaneBase]), s, s+len(c.BaseLn))
		} else {
			o := b.Ranges[PaneOurs]
			b.Ranges[PaneBase] = mapRange(m.oursToBase, len(m.sides[PaneBase]), o[0], o[1])
		}
		if c.Base >= 0 {
			m.labels[PaneBase] = c.BaseLabel
		}
		m.labels[PaneOurs], m.labels[PaneTheirs] = c.OursLabel, c.TheirsLabel
		m.blocks[i] = b
	}
	m.cur = min(m.cur, max(len(m.blocks)-1, 0))
}

// sideLines is a conflict's body for one side.
func sideLines(c git.ConflictBlock, p Pane) []string {
	switch p {
	case PaneOurs:
		return c.Ours
	case PaneTheirs:
		return c.Theirs
	}
	return c.BaseLn
}

// sideView is the result with every conflict replaced by side p's lines,
// and where each block starts in it.
func sideView(result []string, cs []git.ConflictBlock, p Pane) ([]string, []int) {
	out := make([]string, 0, len(result))
	starts := make([]int, len(cs))
	at := 0
	for i, c := range cs {
		out = append(out, result[at:c.Start]...)
		starts[i] = len(out)
		out = append(out, sideLines(c, p)...)
		at = c.End + 1
	}
	return append(out, result[at:]...), starts
}

// anchors maps each line of a to the line of b it matches (-1: none).
func anchors(a, b []string) []int {
	out := make([]int, len(a))
	for i := range out {
		out[i] = -1
	}
	if len(a) == 0 || len(b) == 0 {
		return out
	}
	join := func(ls []string) string { return strings.Join(ls, "\n") + "\n" }
	for _, r := range textdiff.Compute(join(a), join(b)).Rows {
		if r.Kind == textdiff.Context {
			out[r.OldLine-1] = r.NewLine - 1
		}
	}
	return out
}

// mapRange maps a's lines [s, e) to b's: from just after the last matched
// line before s to the first matched line at or after e.
func mapRange(anch []int, bLen, s, e int) [2]int {
	bs, be := 0, bLen
	for i := min(s, len(anch)) - 1; i >= 0; i-- {
		if anch[i] >= 0 {
			bs = anch[i] + 1
			break
		}
	}
	for i := max(e, 0); i < len(anch); i++ {
		if anch[i] >= 0 {
			be = anch[i]
			break
		}
	}
	be = max(be, bs)
	return [2]int{bs, be}
}

// SetCursorLine makes the block at (or else the first one after) the
// result's 0-based line current; past the last block, the last one.
func (m *Model) SetCursorLine(line int) {
	for i, b := range m.blocks {
		if line <= b.Conflict.End {
			m.setCur(i)
			return
		}
	}
	if len(m.blocks) > 0 {
		m.setCur(len(m.blocks) - 1)
	}
}

// Next / Prev step to the next or previous block, wrapping around.
func (m *Model) Next() { m.step(1) }
func (m *Model) Prev() { m.step(-1) }

func (m *Model) step(d int) {
	if n := len(m.blocks); n > 0 {
		m.setCur(((m.cur+d)%n + n) % n)
	}
}

func (m *Model) setCur(i int) {
	if i != m.cur {
		m.scroll = 0
	}
	m.cur = i
}

// Take is what a resolution does to the result: replace lines
// [Start, End) with Lines.
type Take struct {
	Start, End int
	Lines      []string
}

// Take works out resolution r for the current block. Base comes from the
// file's own diff3 section when it has one, else from the index.
func (m Model) Take(r git.Resolution) (Take, bool) {
	b, ok := m.CurrentBlock()
	if !ok {
		return Take{}, false
	}
	c := b.Conflict
	if c.Base < 0 {
		rg := b.Ranges[PaneBase]
		c.BaseLn = append([]string{}, m.sides[PaneBase][rg[0]:rg[1]]...)
	}
	return Take{Start: c.Start, End: c.End + 1, Lines: c.Replacement(r)}, true
}

// SetPane picks the pane shown when only one fits.
func (m *Model) SetPane(p Pane) {
	if p >= 0 && p < paneCount {
		m.pane = p
	}
}

// CyclePane shows the next pane (narrow layout).
func (m *Model) CyclePane() { m.pane = (m.pane + 1) % paneCount }

// ShownPane is the pane shown when only one fits.
func (m Model) ShownPane() Pane { return m.pane }

// SetSize sets the view's size; narrow shows one pane at a time.
func (m *Model) SetSize(w, h int, narrow bool) { m.w, m.h, m.narrow = w, h, narrow }

// Narrow reports whether one pane is shown at a time.
func (m Model) Narrow() bool { return m.narrow }

// SetFocused marks whether the view has the keys (its hint bar shows).
func (m *Model) SetFocused(f bool) { m.focused = f }

// Focused reports whether the view has the keys.
func (m Model) Focused() bool { return m.focused }

// Scroll moves the panes by d rows from the current block's spot.
func (m *Model) Scroll(d int) { m.scroll += d }
