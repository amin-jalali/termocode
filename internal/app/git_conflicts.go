package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/git"
	"termocode/internal/theme"
	"termocode/internal/toast"
)

// Merge-conflict resolver (FEATURE.md §8, Group G).
//
//   - Highlighting + `]x` / `[x` live in nvim (conflictLua): a debounced
//     scan paints the <<<<<<< / ||||||| / ======= / >>>>>>> regions with
//     extmarks. Colours are blended from theme tokens on the Go side and
//     pushed as highlight groups (conflictHighlightLua), re-sent on theme
//     switch.
//   - Resolution (Accept Current / Incoming / Both) is parsed in Go with
//     git.ParseConflictLines and applied as ONE nvim_buf_set_lines call, so a
//     single undo restores the markers.
//   - Entry points: Ctrl+. code actions on a conflict, palette commands, and
//     the CONFLICTS section of the Source Control panel.

// Synthesized code-action kinds for conflict resolution (see
// code_actions_extra.go for the synth-action pattern).
const (
	synthKindConflictOurs   = "conflict.ours"
	synthKindConflictTheirs = "conflict.theirs"
	synthKindConflictBoth   = "conflict.both"
)

// isConflictSynthKind reports whether kind is one of the conflict actions.
func isConflictSynthKind(kind string) bool {
	switch kind {
	case synthKindConflictOurs, synthKindConflictTheirs, synthKindConflictBoth:
		return true
	}
	return false
}

// conflictResolutionFor maps a synth kind to a git.Resolution.
func conflictResolutionFor(kind string) git.Resolution {
	switch kind {
	case synthKindConflictTheirs:
		return git.AcceptTheirs
	case synthKindConflictBoth:
		return git.AcceptBoth
	}
	return git.AcceptOurs
}

// conflictActionTitles returns the code-action titles for a block, naming the
// branch labels when git wrote them ("Accept Current Change (HEAD)").
func conflictActionTitles(b git.ConflictBlock) (ours, theirs, both string) {
	ours, theirs, both = "Accept Current Change", "Accept Incoming Change", "Accept Both Changes"
	if b.OursLabel != "" {
		ours += " (" + b.OursLabel + ")"
	}
	if b.TheirsLabel != "" {
		theirs += " (" + b.TheirsLabel + ")"
	}
	return ours, theirs, both
}

// conflictCodeActions appends the three Accept actions when the cursor
// (1-based line) sits inside a conflict block of the current buffer.
func (m *Model) conflictCodeActions(line int, add func(title, kind string, payload synthCodeAction)) {
	if m.nvim == nil {
		return
	}
	lines, err := m.nvim.CurrentBufLines()
	if err != nil {
		return
	}
	b, ok := git.ConflictAt(git.ParseConflictLines(lines), line-1)
	if !ok {
		return
	}
	ours, theirs, both := conflictActionTitles(b)
	add(ours, synthKindConflictOurs, synthCodeAction{Line: line})
	add(theirs, synthKindConflictTheirs, synthCodeAction{Line: line})
	add(both, synthKindConflictBoth, synthCodeAction{Line: line})
}

// resolveConflictAt resolves the conflict around the 1-based line of the
// current buffer. The buffer is re-read at apply time (it may have changed
// since the picker opened).
func (m *Model) resolveConflictAt(line int, res git.Resolution) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	var toastCmd tea.Cmd
	lines, err := m.nvim.CurrentBufLines()
	if err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Conflict", err.Error())
		return toastCmd
	}
	blocks := git.ParseConflictLines(lines)
	b, ok := git.ConflictAt(blocks, line-1)
	if !ok {
		m.toast, toastCmd = m.toast.Push(toast.Info, "No merge conflict at cursor")
		return toastCmd
	}
	if err := m.nvim.ReplaceCurrentBufLines(b.Start, b.End+1, b.Replacement(res)); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Conflict", err.Error())
		return toastCmd
	}
	_ = m.nvim.ExecLua(`pcall(_G.termocode_conflict_paint)`)
	title := map[git.Resolution]string{
		git.AcceptOurs:   "Accepted current change",
		git.AcceptTheirs: "Accepted incoming change",
		git.AcceptBoth:   "Accepted both changes",
	}[res]
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, title, conflictsLeftDetail(len(blocks)-1))
	return toastCmd
}

// conflictsLeftDetail is the toast detail after one block is resolved.
func conflictsLeftDetail(left int) string {
	switch {
	case left <= 0:
		return "No conflicts left — save, then stage (s) to mark resolved"
	case left == 1:
		return "1 conflict left — ]x for next"
	}
	return fmt.Sprintf("%d conflicts left — ]x for next", left)
}

// resolveConflictAtCursor is the palette entry point.
func (m *Model) resolveConflictAtCursor(res git.Resolution) tea.Cmd {
	line := m.cursorLine
	if m.nvim != nil {
		if s, err := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_win_get_cursor(0)[1])`); err == nil {
			var n int
			if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n > 0 {
				line = n
			}
		}
	}
	return m.resolveConflictAt(line, res)
}

// jumpConflict moves to the next (dir>0) / previous conflict start marker,
// wrapping around. Toasts when the buffer has none.
func (m *Model) jumpConflict(dir int) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	out, _ := m.nvim.EvalLuaString(fmt.Sprintf(`return _G.termocode_conflict_jump and _G.termocode_conflict_jump(%d) or 'none'`, dir))
	if out == "none" || out == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No merge conflicts in this file")
		return toastCmd
	}
	m.focus = FocusEditor
	return nil
}

// gitOpenConflictFile opens a CONFLICTS-section file and puts the cursor on
// its first conflict marker.
func (m *Model) gitOpenConflictFile(fileIndex int) tea.Cmd {
	if fileIndex < 0 || fileIndex >= len(m.gitFiles) || m.nvim == nil {
		return nil
	}
	path := gitFileAbsPath(m.gitFiles[fileIndex].Path)
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`
		local p = ...
		vim.cmd('edit ' .. vim.fn.fnameescape(p))
		vim.api.nvim_win_set_cursor(0, { 1, 0 })
		if _G.termocode_conflict_jump then
			vim.fn.search([[^<<<<<<<\%( \|\t\|$\)]], 'cW')
			pcall(_G.termocode_conflict_paint)
		end
	`, path)
	m.focus = FocusEditor
	return nil
}

// ─── nvim side ─────────────────────────────────────────────────────────

// hexOfToken returns "#rrggbb" for a theme token, converting a bare xterm
// index ("color<N>") when the palette has no truecolor mapping.
func hexOfToken(c theme.Color256) string {
	h := theme.Hex(c)
	if strings.HasPrefix(h, "#") && len(h) == 7 {
		return h
	}
	r, g, b := xterm256RGB(c)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// xterm256RGB converts an xterm-256 index to RGB.
func xterm256RGB(n int) (int, int, int) {
	switch {
	case n < 0:
		return 0, 0, 0
	case n < 16:
		base := [16][3]int{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0}, {0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}
		return base[n][0], base[n][1], base[n][2]
	case n < 232:
		n -= 16
		lv := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return lv(n / 36), lv((n / 6) % 6), lv(n % 6)
	case n < 256:
		v := 8 + (n-232)*10
		return v, v, v
	}
	return 255, 255, 255
}

// blendHex mixes fg into bg: alpha=0 → bg, alpha=1 → fg. Invalid input
// returns bg unchanged.
func blendHex(fg, bg string, alpha float64) string {
	var fr, fgc, fb, br, bgc, bb int
	if _, err := fmt.Sscanf(fg, "#%02x%02x%02x", &fr, &fgc, &fb); err != nil {
		return bg
	}
	if _, err := fmt.Sscanf(bg, "#%02x%02x%02x", &br, &bgc, &bb); err != nil {
		return bg
	}
	mix := func(a, b int) int { return int(float64(b) + (float64(a)-float64(b))*alpha + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", mix(fr, br), mix(fgc, bgc), mix(fb, bb))
}

// conflictHighlightLua (re)defines the conflict highlight groups from the
// active theme tokens: current side tinted with GitAdded, incoming with
// AccentBlue, the diff3 base with TextMuted.
func conflictHighlightLua() string {
	bg := hexOfToken(theme.BgEditor)
	ours := hexOfToken(theme.GitAdded)
	theirs := hexOfToken(theme.AccentBlue)
	base := hexOfToken(theme.TextMuted)
	fg := hexOfToken(theme.TextPrimary)
	dim := hexOfToken(theme.TextDim)
	return fmt.Sprintf(`
local hi = function(g, o) vim.api.nvim_set_hl(0, g, o) end
hi('TermocodeConflictOursMarker',   { bg = %q, fg = %q, bold = true })
hi('TermocodeConflictOurs',         { bg = %q })
hi('TermocodeConflictBaseMarker',   { bg = %q, fg = %q, bold = true })
hi('TermocodeConflictBase',         { bg = %q })
hi('TermocodeConflictSep',          { bg = %q, fg = %q, bold = true })
hi('TermocodeConflictTheirs',       { bg = %q })
hi('TermocodeConflictTheirsMarker', { bg = %q, fg = %q, bold = true })
hi('TermocodeConflictLabel',        { fg = %q, italic = true })
`,
		blendHex(ours, bg, 0.40), fg,
		blendHex(ours, bg, 0.16),
		blendHex(base, bg, 0.30), fg,
		blendHex(base, bg, 0.10),
		blendHex(base, bg, 0.20), fg,
		blendHex(theirs, bg, 0.16),
		blendHex(theirs, bg, 0.40), fg,
		dim,
	)
}

// conflictLua installs the painter, the jump helper, and the `]x` / `[x`
// normal-mode maps. The Lua parser mirrors git.ParseConflictLines (markers
// are the 7-char run alone or followed by a space/tab).
func conflictLua() string {
	return conflictHighlightLua() + `
local ns = vim.api.nvim_create_namespace('TermocodeConflicts')

local function is_marker(l, m)
  if l:sub(1, 7) ~= m then return false end
  local r = l:sub(8, 8)
  return r == '' or r == ' ' or r == '\t'
end

local function parse(lines)
  local blocks, i, n = {}, 1, #lines
  while i <= n do
    if not is_marker(lines[i], '<<<<<<<') then
      i = i + 1
    else
      local s, base, sep, e = i, nil, nil, nil
      for j = s + 1, n do
        local l = lines[j]
        if is_marker(l, '<<<<<<<') or is_marker(l, '>>>>>>>') then break end
        if not base and is_marker(l, '|||||||') then
          base = j
        elseif is_marker(l, '=======') then
          sep = j; break
        end
      end
      if sep then
        for j = sep + 1, n do
          local l = lines[j]
          if is_marker(l, '<<<<<<<') or is_marker(l, '=======') or is_marker(l, '|||||||') then break end
          if is_marker(l, '>>>>>>>') then e = j; break end
        end
      end
      if e then
        table.insert(blocks, { s = s, base = base, sep = sep, e = e })
        i = e + 1
      else
        i = s + 1
      end
    end
  end
  return blocks
end

local function mark(buf, row1, group, label)
  local opts = { line_hl_group = group, priority = 60 }
  if label then
    opts.virt_text = { { '  ' .. label, 'TermocodeConflictLabel' } }
    opts.virt_text_pos = 'eol'
  end
  pcall(vim.api.nvim_buf_set_extmark, buf, ns, row1 - 1, 0, opts)
end

local function paint(buf)
  buf = (buf and buf ~= 0) and buf or vim.api.nvim_get_current_buf()
  if not vim.api.nvim_buf_is_valid(buf) or vim.bo[buf].buftype ~= '' then return end
  vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
  if vim.api.nvim_buf_line_count(buf) > 100000 then return end
  local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  local any = false
  for _, l in ipairs(lines) do
    if l:sub(1, 7) == '<<<<<<<' then any = true; break end
  end
  if not any then return end
  for _, b in ipairs(parse(lines)) do
    mark(buf, b.s, 'TermocodeConflictOursMarker', 'Current Change · Ctrl+. to resolve')
    for r = b.s + 1, (b.base or b.sep) - 1 do mark(buf, r, 'TermocodeConflictOurs') end
    if b.base then
      mark(buf, b.base, 'TermocodeConflictBaseMarker', 'Common Ancestor')
      for r = b.base + 1, b.sep - 1 do mark(buf, r, 'TermocodeConflictBase') end
    end
    mark(buf, b.sep, 'TermocodeConflictSep')
    for r = b.sep + 1, b.e - 1 do mark(buf, r, 'TermocodeConflictTheirs') end
    mark(buf, b.e, 'TermocodeConflictTheirsMarker', 'Incoming Change')
  end
end
_G.termocode_conflict_paint = paint

-- Debounced repaint: edits fire TextChanged* on every keystroke.
local pending = {}
local function schedule_paint(ev)
  local buf = ev.buf
  if pending[buf] then return end
  pending[buf] = true
  vim.defer_fn(function()
    pending[buf] = nil
    pcall(paint, buf)
  end, 150)
end

vim.api.nvim_create_augroup('TermocodeConflicts', { clear = true })
vim.api.nvim_create_autocmd({ 'BufReadPost', 'BufEnter', 'BufWritePost', 'TextChanged', 'TextChangedI', 'InsertLeave', 'FileChangedShellPost' }, {
  group = 'TermocodeConflicts',
  callback = schedule_paint,
})

-- Jump to the next (dir > 0) / previous conflict start marker, wrapping.
_G.termocode_conflict_jump = function(dir)
  local ln = vim.fn.search([[^<<<<<<<\%( \|\t\|$\)]], dir > 0 and 'w' or 'bw')
  if ln == 0 then return 'none' end
  return tostring(ln)
end
vim.keymap.set('n', ']x', function() _G.termocode_conflict_jump(1) end, { silent = true, desc = 'Next merge conflict' })
vim.keymap.set('n', '[x', function() _G.termocode_conflict_jump(-1) end, { silent = true, desc = 'Previous merge conflict' })
`
}

// refreshConflictHighlights re-sends the highlight groups after a theme
// switch so conflict tints follow the new palette.
func (m *Model) refreshConflictHighlights() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(conflictHighlightLua())
	}
}

// dispatchGitExtrasPalette handles the Group G palette IDs (merge conflicts
// and clone). ok=false means the ID is not ours.
func (m *Model) dispatchGitExtrasPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-conflict-next":
		return m.jumpConflict(1), true
	case "git-conflict-prev":
		return m.jumpConflict(-1), true
	case "git-conflict-ours":
		return m.resolveConflictAtCursor(git.AcceptOurs), true
	case "git-conflict-theirs":
		return m.resolveConflictAtCursor(git.AcceptTheirs), true
	case "git-conflict-both":
		return m.resolveConflictAtCursor(git.AcceptBoth), true
	case "git-clone":
		return m.openCloneURLPrompt(), true
	}
	return nil, false
}
