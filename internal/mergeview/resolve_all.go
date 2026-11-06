package mergeview

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
)

// Whole-file resolutions (GITX-1 I10, GITX-34): every conflict at once,
// or only the simple ones. Both return the new result; the app puts it in
// the editor as one undo step.

// withBase is block b's conflict with its base lines (from the file's own
// diff3 section, else from the index), as Take uses it.
func (m Model) withBase(b Block) git.ConflictBlock {
	c := b.Conflict
	if c.Base < 0 {
		rg := b.Ranges[PaneBase]
		if rg[1] <= len(m.sides[PaneBase]) {
			c.BaseLn = append([]string{}, m.sides[PaneBase][rg[0]:rg[1]]...)
		}
	}
	return c
}

// replace builds the result with block i replaced by pick(i) (nil keeps
// the block as it is).
func (m Model) replace(pick func(i int, c git.ConflictBlock) []string) []string {
	out := make([]string, 0, len(m.result))
	at := 0
	for i, b := range m.blocks {
		c := m.withBase(b)
		out = append(out, m.result[at:c.Start]...)
		if lines := pick(i, c); lines != nil {
			out = append(out, lines...)
		} else {
			out = append(out, m.result[c.Start:c.End+1]...)
		}
		at = c.End + 1
	}
	return append(out, m.result[at:]...)
}

// ResolveAll is the result with resolution r taken for every conflict
// (nil when there is none). Lines git merged on its own stay.
func (m Model) ResolveAll(r git.Resolution) []string {
	if len(m.blocks) == 0 {
		return nil
	}
	return m.replace(func(_ int, c git.ConflictBlock) []string {
		if l := c.Replacement(r); l != nil {
			return l
		}
		return []string{}
	})
}

// ResolveSimple settles the conflicts where only one side changed the
// base or both sides made the same change; ignoreSpace also counts a
// change of spaces and tabs only as the same. n is how many it settled.
func (m Model) ResolveSimple(ignoreSpace bool) (lines []string, n int) {
	same := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			x, y := a[i], b[i]
			if ignoreSpace {
				x, y = strings.Join(strings.Fields(x), " "), strings.Join(strings.Fields(y), " ")
			}
			if x != y {
				return false
			}
		}
		return true
	}
	lines = m.replace(func(_ int, c git.ConflictBlock) []string {
		var l []string
		switch {
		case same(c.Ours, c.Theirs):
			l = c.Ours
		case same(c.Ours, c.BaseLn) && !m.noBase:
			l = c.Theirs
		case same(c.Theirs, c.BaseLn) && !m.noBase:
			l = c.Ours
		default:
			return nil
		}
		n++
		return append([]string{}, l...)
	})
	return lines, n
}
