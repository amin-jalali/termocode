package app

import (
	"fmt"

	"termocode/internal/theme"
)

// aiLua installs _G._termocode_ai — the nvim half of inline ghost-text
// completion (Group A):
//
//   - context(nb, na): cursor snapshot (buffer, changedtick, row/col,
//     mode, filetype, windowed text) returned as JSON to Go;
//   - show(...): draws a completion as extmark virtual text at the caret
//     (inline on nvim ≥ 0.10, overlay / eol on 0.9) plus virt_lines for
//     the remaining lines — only if buffer, tick and caret still match;
//   - has() / accept() / clear(): Tab (snippets_lua.go) calls accept()
//     first; it replays the continuation-vs-rewrite edit decided in Go
//     (delete N bytes left of the caret, insert text, add imports);
//   - TextChangedI → termocode_ai_changed, caret moves / InsertLeave →
//     termocode_ai_cancel (Go debounces and cancels in-flight requests).
//
// Loaded right after snippetsLua. No backticks anywhere (Go raw string).
const aiLua = `
_G._termocode_ai = _G._termocode_ai or {}
local A = _G._termocode_ai
A.ns = vim.api.nvim_create_namespace('termocode_ai_ghost')
A.enabled = A.enabled or false
A.ghost = nil
A.hl = A.hl or { fg = '#6c6c6c', italic = true }
local has_inline = vim.fn.has('nvim-0.10') == 1

local function notify(method, ...)
  if _G.termocode_notify then _G.termocode_notify(method, ...) end
end

function A.set_colors(ghost, accent)
  A.hl = { fg = ghost, italic = true }
  vim.api.nvim_set_hl(0, 'TermocodeAIGhost', A.hl)
  vim.api.nvim_set_hl(0, 'TermocodeAIAccent', { fg = accent, bold = true })
end

function A.set_enabled(on)
  A.enabled = on and true or false
  if not A.enabled then A.clear() end
end

function A.context(nb, na)
  local buf = vim.api.nvim_get_current_buf()
  local pos = vim.api.nvim_win_get_cursor(0)
  local row, col = pos[1], pos[2]
  local total = vim.api.nvim_buf_line_count(buf)
  local first = math.max(row - nb, 1)
  local last = math.min(row + na, total)
  local lines = vim.api.nvim_buf_get_lines(buf, first - 1, last, false)
  local cur = lines[row - first + 1] or ''
  local before = {}
  for i = 1, row - first do before[#before + 1] = lines[i] end
  before[#before + 1] = cur:sub(1, col)
  local after = { cur:sub(col + 1) }
  for i = row - first + 2, #lines do after[#after + 1] = lines[i] end
  return {
    buf = buf, tick = vim.api.nvim_buf_get_changedtick(buf),
    row = row - 1, col = col, mode = vim.api.nvim_get_mode().mode,
    ft = vim.bo[buf].filetype, bt = vim.bo[buf].buftype,
    path = vim.api.nvim_buf_get_name(buf),
    before = table.concat(before, '\n'), after = table.concat(after, '\n'),
    line_before = cur:sub(1, col), line_after = cur:sub(col + 1),
  }
end

local function at_ghost(g)
  if not g then return false end
  if not vim.api.nvim_buf_is_valid(g.buf) or vim.api.nvim_get_current_buf() ~= g.buf then return false end
  local pos = vim.api.nvim_win_get_cursor(0)
  return pos[1] - 1 == g.row and pos[2] == g.col and vim.api.nvim_buf_get_changedtick(g.buf) == g.tick
end

function A.clear()
  local g = A.ghost
  A.ghost = nil
  if g and vim.api.nvim_buf_is_valid(g.buf) then
    pcall(vim.api.nvim_buf_clear_namespace, g.buf, A.ns, 0, -1)
  end
end

function A.has() return at_ghost(A.ghost) end

function A.show(buf, tick, row, col, text, del, imports)
  local g = { buf = buf, tick = tick, row = row, col = col, text = text, del = del or 0, imports = imports }
  if vim.api.nvim_get_mode().mode:sub(1, 1) ~= 'i' or not at_ghost(g) then return false end
  A.clear()
  vim.api.nvim_set_hl(0, 'TermocodeAIGhost', A.hl)
  local lines = vim.split(text, '\n', { plain = true })
  local line = vim.api.nvim_buf_get_lines(buf, row, row + 1, false)[1] or ''
  local opts = { hl_mode = 'combine', priority = 200 }
  if g.del > 0 then
    opts.virt_text = { { '  ⟲ ' .. lines[1], 'TermocodeAIGhost' } }
    opts.virt_text_pos = 'eol'
  elseif has_inline then
    opts.virt_text = { { lines[1], 'TermocodeAIGhost' } }
    opts.virt_text_pos = 'inline'
  elseif col >= #line then
    opts.virt_text = { { lines[1], 'TermocodeAIGhost' } }
    opts.virt_text_pos = 'overlay'
  else
    opts.virt_text = { { '  ' .. lines[1], 'TermocodeAIGhost' } }
    opts.virt_text_pos = 'eol'
  end
  if #lines > 1 then
    local vl = {}
    for i = 2, #lines do vl[#vl + 1] = { { lines[i], 'TermocodeAIGhost' } } end
    opts.virt_lines = vl
  end
  local ok = pcall(vim.api.nvim_buf_set_extmark, buf, A.ns, row, math.min(col, #line), opts)
  if ok then A.ghost = g end
  return ok
end

function A.accept()
  local g = A.ghost
  if not at_ghost(g) then
    A.clear()
    return false
  end
  A.clear()
  local lines = vim.split(g.text, '\n', { plain = true })
  local start = math.max(g.col - g.del, 0)
  local ok = pcall(vim.api.nvim_buf_set_text, g.buf, g.row, start, g.row, g.col, lines)
  if not ok then return false end
  local erow = g.row + #lines - 1
  local ecol = (#lines == 1) and (start + #lines[1]) or #lines[#lines]
  local shift = 0
  local imp = g.imports
  if type(imp) == 'table' and type(imp.lines) == 'table' and #imp.lines > 0 then
    pcall(vim.cmd, 'undojoin')
    if pcall(vim.api.nvim_buf_set_lines, g.buf, imp.at, imp.at, false, imp.lines) and imp.at <= erow then
      shift = #imp.lines
    end
  end
  pcall(vim.api.nvim_win_set_cursor, 0, { erow + 1 + shift, ecol })
  return true
end

local grp = vim.api.nvim_create_augroup('TermocodeAI', { clear = true })
vim.api.nvim_create_autocmd({ 'TextChangedI', 'TextChangedP' }, {
  group = grp,
  callback = function(ev)
    if A.ghost and not at_ghost(A.ghost) then A.clear() end
    if not A.enabled or vim.bo[ev.buf].buftype ~= '' then return end
    notify('termocode_ai_changed', ev.buf, vim.api.nvim_buf_get_changedtick(ev.buf))
  end,
})
vim.api.nvim_create_autocmd('CursorMovedI', {
  group = grp,
  callback = function()
    if A.ghost and not at_ghost(A.ghost) then
      A.clear()
      notify('termocode_ai_cancel')
    end
  end,
})
vim.api.nvim_create_autocmd({ 'InsertLeave', 'BufLeave' }, {
  group = grp,
  callback = function()
    if A.ghost then A.clear() end
    notify('termocode_ai_cancel')
  end,
})
`

// aiColorsLua pushes the active theme's AIGhost / AIAccent into nvim's
// highlight groups — called at attach and on every theme switch, so ghost
// text recolors live.
func aiColorsLua() string {
	return fmt.Sprintf(`if _G._termocode_ai and _G._termocode_ai.set_colors then _G._termocode_ai.set_colors(%q, %q) end`,
		hexOr(theme.AIGhost, "#6c6c6c"), hexOr(theme.AIAccent, "#bb86fc"))
}

// hexOr returns the token's truecolor hex, or fallback when the palette
// only knows a generic "colorN" name (nvim needs #rrggbb).
func hexOr(c theme.Color256, fallback string) string {
	if h := theme.Hex(c); len(h) == 7 && h[0] == '#' {
		return h
	}
	return fallback
}

// aiEnableLua mirrors the Go-side inline toggle into Lua.
func aiEnableLua(on bool) string {
	return fmt.Sprintf(`if _G._termocode_ai and _G._termocode_ai.set_enabled then _G._termocode_ai.set_enabled(%v) end`, on)
}
