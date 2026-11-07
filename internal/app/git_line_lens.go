package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Line history lens (GITX-39): a dim line above the cursor's block (the
// run of non-blank lines around the cursor) that names its newest
// change and how many authors and commits made it. Git: Show History of
// Block opens the block's commits. It reads the same loaded blame as
// inline blame.

// lensBlockReach is how far the block may reach up and down from the
// cursor.
const lensBlockReach = 200

// lensBlock is the block around line cur. blank has one flag per line
// from line first on: '1' for a blank line, '0' for text.
func lensBlock(blank string, first, cur int) (from, to int) {
	i := cur - first
	if i < 0 || i >= len(blank) || blank[i] == '1' {
		return cur, cur
	}
	a, b := i, i
	for a > 0 && blank[a-1] == '0' {
		a--
	}
	for b+1 < len(blank) && blank[b+1] == '0' {
		b++
	}
	return first + a, first + b
}

// lineLensText is the lens of lines from..to: the newest change, then
// the counts when more than one commit made the block, then the hint.
func lineLensText(ls []git.BlameRangeLine, from, to int) string {
	var newest *git.BlameRangeLine
	uncommitted := false
	authors, commits := map[string]bool{}, map[string]bool{}
	for i := range ls {
		l := &ls[i]
		if l.Line < from || l.Line > to {
			continue
		}
		if l.Hash == "uncommitted" || l.Full == "" && l.When == 0 {
			uncommitted = true
			continue
		}
		authors[l.Author], commits[l.Hash] = true, true
		if newest == nil || l.When > newest.When {
			newest = l
		}
	}
	var text string
	switch {
	case uncommitted:
		text = "You · Not committed yet"
	case newest != nil:
		text = blameStatusText(*newest) + " · " + newest.Hash + " " + newest.Subject
	default:
		return ""
	}
	if len(commits) > 1 {
		text += fmt.Sprintf(" · %s, %d commits", lensCount(len(authors), "author"), len(commits))
	}
	return text + " · Git: Show History of Block"
}

func lensCount(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// gitLensBlock is the cursor's block (the cursor line alone without an
// editor).
func (m *Model) gitLensBlock() (from, to int) {
	cur := max(m.cursorLine, 1)
	if m.nvim == nil {
		return cur, cur
	}
	s, err := m.nvim.EvalLuaStringArgs(gitLensBlockLua, lensBlockReach)
	if err != nil {
		return cur, cur
	}
	parts := strings.SplitN(s, ",", 3)
	var first, c int
	if len(parts) != 3 {
		return cur, cur
	}
	if _, err := fmt.Sscanf(parts[0]+","+parts[1], "%d,%d", &first, &c); err != nil || first < 1 || c < 1 {
		return cur, cur
	}
	return lensBlock(parts[2], first, c)
}

const gitLensBlockLua = `
local reach = ...
local cur = vim.fn.line('.')
local n = vim.api.nvim_buf_line_count(0)
local a, b = math.max(1, cur - reach), math.min(n, cur + reach)
local t = {}
for i, s in ipairs(vim.api.nvim_buf_get_lines(0, a - 1, b, false)) do
  t[i] = s:match('^%s*$') and '1' or '0'
end
return a .. ',' .. cur .. ',' .. table.concat(t)
`

// paintLineLens puts the lens above the cursor's block.
func (m *Model) paintLineLens() {
	text, from := "", 0
	if m.gitBlame.lens && m.gitBlame.key != "" && m.gitBlame.key == m.gitBlameKey() {
		f, to := m.gitLensBlock()
		if text = lineLensText(m.gitBlame.lines, f, to); text != "" {
			from = f
		}
	}
	if text == m.gitBlame.lensText && from == m.gitBlame.lensFrom {
		return
	}
	m.gitBlame.lensText, m.gitBlame.lensFrom = text, from
	if m.nvim != nil {
		_, _ = m.nvim.EvalLuaStringArgs(gitLineLensLua, text, from, hexOfToken(theme.TextDim))
	}
}

const gitLineLensLua = `
local text, line, fg = ...
local ns = vim.api.nvim_create_namespace('termocode_line_lens')
vim.api.nvim_set_hl(0, 'TermocodeLineLens', { fg = fg, italic = true })
local prev = _G.termocode_line_lens_buf
if prev and vim.api.nvim_buf_is_valid(prev) then vim.api.nvim_buf_clear_namespace(prev, ns, 0, -1) end
_G.termocode_line_lens_buf = nil
if text == '' then return '' end
local b = vim.api.nvim_get_current_buf()
local first = vim.api.nvim_buf_get_lines(b, line - 1, line, false)[1] or ''
local indent = first:match('^%s*'):gsub('\t', string.rep(' ', vim.bo[b].tabstop))
pcall(vim.api.nvim_buf_set_extmark, b, ns, line - 1, 0, { virt_lines = { { { indent .. text, 'TermocodeLineLens' } } }, virt_lines_above = true })
_G.termocode_line_lens_buf = b
return ''
`

// saveGitLineLens writes git_line_lens to config.json.
func (m *Model) saveGitLineLens() {
	path, err := settingsConfigPath()
	if err != nil {
		return
	}
	on := m.gitBlame.lens
	_ = cfgfile.Update(path, func(c map[string]any) error {
		c["git_line_lens"] = on
		return nil
	})
}

// toggleLineLens turns the line history lens on or off (saved).
func (m *Model) toggleLineLens() tea.Cmd {
	m.gitBlameInit()
	m.gitBlame.lens = !m.gitBlame.lens
	m.saveGitLineLens()
	if !m.gitBlame.lens {
		m.paintLineLens()
		return m.gitBlameToast(toast.Info, "Line history lens off", "")
	}
	return tea.Batch(m.gitBlameShowCmd(), m.gitBlameToast(toast.Info, "Line history lens on",
		"Git: Show History of Block lists the commits of the block"))
}

// gitBlockHistoryPicker lists the commits that changed the selection,
// or the cursor's block.
func (m *Model) gitBlockHistoryPicker() tea.Cmd {
	from, to := m.gitLineRange()
	if from == to {
		from, to = m.gitLensBlock()
	}
	return m.gitLineHistoryPickerRange(from, to)
}
