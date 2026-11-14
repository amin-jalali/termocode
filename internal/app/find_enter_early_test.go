package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/findbar"
)

// Bench finding (PR #539): Enter in the find bar right after typing,
// before the search of the last key has come back, jumps to the first
// match of the whole query (it showed "0/1" and stayed). The search
// messages run as commands and may come late or out of order: a late
// one for an older query must not undo the newer search.
func TestFindEnterBeforeTheSearchCameBackJumps(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	covEditorFile(t, &m, "f.txt", "alpha\nbeta\nneedle here\n")
	m.findOpen = true
	m.find = findbar.New()
	for _, r := range "needle" {
		m, _ = m.update(coreRunes(string(r))) // the SearchMsg commands are not run
	}
	m, _ = m.update(findbar.FindNextMsg{})
	line, err := m.nvim.EvalLuaString(`return tostring(vim.fn.line('.'))`)
	if err != nil {
		t.Fatal(err)
	}
	if line != "3" {
		t.Errorf("Enter did not jump to the match: cursor on line %s", line)
	}
	m, _ = m.update(findbar.SearchMsg{Query: "nee"}) // a late search of an older query
	if pat, _ := m.nvim.EvalLuaString(`return vim.fn.getreg('/')`); pat == "" || len(pat) < len("needle") {
		t.Errorf("a late search for %q replaced the pattern of %q: @/ = %q", "nee", "needle", pat)
	}
}
