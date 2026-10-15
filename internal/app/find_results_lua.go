package app

// findResultsLua wires up the "Find Results" page (ADR 0002, design in
// docs/plans/find-results-design.md):
//
//   - the buffer holds plain text; _G._termocode_find.rows[lnum] says
//     what each line is (k = 'q' query, 's' summary, 'r' rule, 'h' file
//     header, 'm' match, 'e' empty-state text, empty for a blank line). Go fills both
//     through _termocode_find_set / _append / _head (find_results.go).
//   - a decoration provider draws all the chrome (icons, fold marks,
//     count pills, match washes, hint bar, hairline) with ephemeral
//     extmarks for the visible rows only: nothing is stored, idle costs 0.
//   - folds per file group come from the row table (foldexpr).
//   - Enter / o / mouse / F4 (_G._termocode_find_nav) jump from the row
//     table; no text is parsed back.
//
// Loaded once during attach (model.go::attachCmd).
const findResultsLua = `
local api = vim.api
_G._termocode_find = _G._termocode_find or { rows = {}, icon_hls = {}, head = {} }
local F = _G._termocode_find
local ns = api.nvim_create_namespace('termocode_find')

-- File icon colours: one group per colour, defined on first use and
-- again after every theme switch (themeLuaBody calls this).
function _G._termocode_find_rehl()
  for name, fg in pairs(F.icon_hls) do api.nvim_set_hl(0, name, { fg = fg }) end
end

local function the_buf()
  local b = vim.g.termocode_find_results_buf
  if b and api.nvim_buf_is_valid(b) then return b end
end

local function put(b, s, e, lines)
  vim.bo[b].modifiable = true
  api.nvim_buf_set_lines(b, s, e, false, lines)
  vim.bo[b].modifiable = false
end

local function add_rows(base, rows)
  for i, r in ipairs(rows) do
    F.rows[base + i] = r
    if r.icon_hl and r.icon_fg and F.icon_hls[r.icon_hl] ~= r.icon_fg then
      F.icon_hls[r.icon_hl] = r.icon_fg
      api.nvim_set_hl(0, r.icon_hl, { fg = r.icon_fg })
    end
  end
end

local function head_text(h) return { '    ' .. (h.query or ''), ' ' .. (h.summary or '') } end

-- _termocode_find_head(h) rewrites the query and summary rows.
function _G._termocode_find_head(h)
  local b = the_buf(); if not b then return end
  F.head = h
  put(b, 0, 2, head_text(h))
end

-- _termocode_find_set(h, lines, rows) replaces the whole page (a new
-- search or a refresh). Windows showing it keep their cursor row.
function _G._termocode_find_set(h, lines, rows)
  local b = the_buf(); if not b then return end
  F.head = h
  local wins = {}
  for _, w in ipairs(vim.fn.win_findbuf(b)) do wins[w] = api.nvim_win_get_cursor(w) end
  local all = head_text(h)
  all[3] = ''
  vim.list_extend(all, lines)
  -- The row table first: the fold levels are computed when lines change.
  F.rows = { { k = 'q' }, { k = 's' }, { k = 'r' } }
  add_rows(3, rows)
  put(b, 0, -1, all)
  local n = api.nvim_buf_line_count(b)
  -- A window on the top rows (a new search) starts on the first match.
  local first = 4
  while first < n and not (F.rows[first] and F.rows[first].k == 'm') do first = first + 1 end
  for w, c in pairs(wins) do
    local row = c[1] <= 3 and first or math.min(c[1], n)
    pcall(api.nvim_win_set_cursor, w, { row, 0 })
  end
  vim.b[b].termocode_nav_row = nil
  -- The first F4 opens the match under the cursor, not the one after it.
  vim.b[b].termocode_nav_fresh = true
end

-- _termocode_find_append(h, lines, rows) adds one streamed batch.
function _G._termocode_find_append(h, lines, rows)
  local b = the_buf(); if not b then return end
  F.head = h
  put(b, 0, 2, head_text(h))
  if #lines == 0 then return end
  local base = api.nvim_buf_line_count(b)
  add_rows(base, rows)
  put(b, -1, -1, lines)
end

-- Folds: one per file group.
function _G._termocode_find_fold(lnum)
  local r = F.rows[lnum]
  if not r then return '0' end
  if r.k == 'h' then return '>1' end
  if r.k == 'm' then return '1' end
  return '0'
end

local function width(s) return vim.fn.strdisplaywidth(s) end

-- The hint bar, dropping pairs from the middle until it fits in room
-- cells ("Enter open" and the last pair always stay while they fit).
local function hint_chunks(room)
  local hs = F.head.hints or {}
  local keep = {}
  for i = 1, #hs do keep[i] = true end
  local function build()
    local out, w = {}, 0
    for i, p in ipairs(hs) do
      if keep[i] then
        out[#out + 1] = { ' ' .. p[1] .. ' ', 'TermocodeFindKeycap' }
        out[#out + 1] = { ' ' .. p[2] .. '  ', 'TermocodeFindHint' }
        w = w + width(p[1]) + width(p[2]) + 5
      end
    end
    return out, w
  end
  local out, w = build()
  local drop = #hs - 1
  while w > room and drop > 1 do
    keep[drop] = false
    drop = drop - 1
    out, w = build()
  end
  if w > room then return {} end
  return out
end

local function chips()
  local out = {}
  for _, c in ipairs(F.head.chips or {}) do
    out[#out + 1] = { ' ' .. c[1] .. ' ', c[2] and 'TermocodeFindChipOn' or 'TermocodeFindChipOff' }
    out[#out + 1] = { ' ', 'Normal' }
  end
  if F.head.meta and F.head.meta ~= '' then out[#out + 1] = { ' ' .. F.head.meta .. ' ', 'TermocodeFindMeta' } end
  return out
end

local function hairline(w)
  local a = math.max(1, math.floor(w / 6))
  local bar = '─'
  return {
    { bar:rep(a), 'TermocodeFindHair1' },
    { bar:rep(a), 'TermocodeFindHair2' },
    { bar:rep(math.max(0, w - 2 * a)), 'TermocodeFindHair3' },
  }
end

local cur_win, cur_w = nil, 80
api.nvim_set_decoration_provider(ns, {
  on_win = function(_, win, b)
    if vim.bo[b].filetype ~= 'findresults' then return false end
    cur_win, cur_w = win, api.nvim_win_get_width(win)
  end,
  on_line = function(_, win, b, row)
    local r = F.rows[row + 1]
    if not r or r.k == '' then return end
    local line = api.nvim_buf_get_lines(b, row, row + 1, false)[1] or ''
    local function E(col, o)
      o.ephemeral = true
      pcall(api.nvim_buf_set_extmark, b, ns, row, col, o)
    end
    local g = F.head.g or {}
    if r.k == 'q' then
      E(0, { virt_text = { { ' ' .. (g.search or '') .. '  ', 'TermocodeFindIcon' } }, virt_text_pos = 'overlay' })
      E(4, { end_col = #line, hl_group = 'TermocodeFindQuery' })
      E(0, { virt_text = chips(), virt_text_pos = 'right_align' })
    elseif r.k == 's' then
      E(0, { end_col = #line, hl_group = F.head.summary_hl or 'TermocodeFindSummary' })
      E(0, { virt_text = hint_chunks(cur_w - width(line) - 3), virt_text_pos = 'right_align' })
    elseif r.k == 'r' then
      E(0, { virt_text = hairline(cur_w), virt_text_pos = 'overlay' })
    elseif r.k == 'h' then
      local closed = api.nvim_win_call(win, function() return vim.fn.foldclosed(row + 1) end) ~= -1
      E(0, { virt_text = {
        { ' ' .. ((closed and g.closed or g.open) or '') .. '  ', 'TermocodeFindFold' },
        { (r.icon or '') .. ' ', r.icon_hl or 'TermocodeFindDir' },
      }, virt_text_pos = 'overlay' })
      E(0, { end_col = r.name_at, hl_group = 'TermocodeFindDir' })
      E(r.name_at, { end_col = #line, hl_group = 'TermocodeFindName' })
      E(0, { virt_text = { { ' ' .. r.n .. ' ', 'TermocodeFindCount' } }, virt_text_pos = 'right_align' })
    elseif r.k == 'm' then
      E(0, { end_col = 7, hl_group = 'TermocodeFindLineNr' })
      for _, x in ipairs(r.hl or {}) do
        E(x[1], { end_col = x[2], hl_group = 'TermocodeFindMatch', priority = 200 })
      end
      if r.cut and r.cut:find('l') then E(10, { end_col = 13, hl_group = 'TermocodeFindMeta' }) end
      if r.cut and r.cut:find('r') then E(#line - 3, { end_col = #line, hl_group = 'TermocodeFindMeta' }) end
    elseif r.k == 'e' then
      E(0, { end_col = #line, hl_group = r.hl or 'TermocodeFindSummary' })
    end
  end,
})

-- Window look: no number gutter (the rows carry their own line numbers),
-- the cursor row in the selection wash, folds per file.
local function set_win(win)
  local o = vim.wo[win]
  o.number, o.relativenumber = false, false
  o.signcolumn, o.foldcolumn = 'no', '0'
  pcall(function() o.statuscolumn = '' end)
  o.cursorline, o.wrap, o.list = true, false, true
  o.listchars = 'extends:…,tab:  '
  o.winhighlight = 'CursorLine:TermocodeFindCursor,Folded:TermocodeFindFolded'
  o.foldmethod = 'expr'
  o.foldexpr = 'v:lua._termocode_find_fold(v:lnum)'
  if not pcall(function() o.foldtext = '' end) then o.foldtext = 'getline(v:foldstart)' end
  o.foldlevel, o.fillchars = 1, 'fold: '
  o.conceallevel, o.scrolloff = 0, 2
end

local function open_row(r)
  if not r then return end
  if r.k == 'h' then
    vim.cmd('edit ' .. vim.fn.fnameescape(r.file))
  elseif r.k == 'm' then
    vim.cmd('edit +' .. r.line .. ' ' .. vim.fn.fnameescape(r.file))
    pcall(api.nvim_win_set_cursor, 0, { r.line, math.max(0, (r.col or 1) - 1) })
  end
end

local function toggle_fold(lnum)
  local r = F.rows[lnum]
  if r and (r.k == 'h' or r.k == 'm') then pcall(vim.cmd, lnum .. 'foldclose') end
end

vim.api.nvim_create_augroup('TermocodeFindResults', { clear = true })
vim.api.nvim_create_autocmd('FileType', {
  group = 'TermocodeFindResults',
  pattern = 'findresults',
  callback = function(args)
    local buf = args.buf
    vim.bo[buf].swapfile, vim.bo[buf].buflisted = false, true
    set_win(0)
    local function map(lhs, fn) vim.keymap.set('n', lhs, fn, { buffer = buf, silent = true, nowait = true }) end
    local function at_cursor() return F.rows[api.nvim_win_get_cursor(0)[1]] end
    map('<CR>', function() open_row(at_cursor()) end)
    map('o', function() open_row(at_cursor()) end)
    map('<Tab>', function()
      local l = api.nvim_win_get_cursor(0)[1]
      if vim.fn.foldclosed(l) ~= -1 then vim.cmd(l .. 'foldopen') else toggle_fold(l) end
    end)
    -- q / Esc close the page through the app, so the tab strip updates
    -- at once (a bdelete from here alone shows up on the next poll).
    local function close()
      if _G.termocode_notify then _G.termocode_notify('termocode_find_close') else pcall(vim.cmd, 'bdelete') end
    end
    map('q', close)
    map('<Esc>', close)
    map('R', function()
      if _G.termocode_notify then _G.termocode_notify('termocode_find_replace', F.head.query or '') end
    end)
    -- Mouse: a click is "position + act", never a drag selection. A click
    -- on a file header opens / closes its group; a double click opens
    -- the file; a click on a match opens it at the match.
    local function mouse(double)
      return function()
        local pos = vim.fn.getmousepos()
        if pos.winid ~= api.nvim_get_current_win() and pos.winid ~= 0 then pcall(api.nvim_set_current_win, pos.winid) end
        if pos.line <= 0 then return end
        pcall(api.nvim_win_set_cursor, 0, { pos.line, math.max(0, pos.column - 1) })
        local r = F.rows[pos.line]
        if r and r.k == 'q' then
          -- The chips sit right-aligned: find the one under the pointer.
          local col = api.nvim_win_get_width(0) - 1
          local hit
          local cs = chips()
          for i = #cs, 1, -1 do
            local w = width(cs[i][1])
            if pos.wincol - 1 >= col - w + 1 and pos.wincol - 1 <= col then hit = cs[i]; break end
            col = col - w
          end
          local label = hit and vim.trim(hit[1]) or ''
          if (label == 'Aa' or label == 'ab' or label == '.*') and _G.termocode_notify then
            _G.termocode_notify('termocode_find_toggle', label)
          end
          return
        end
        if r and r.k == 'h' and not double then
          if vim.fn.foldclosed(pos.line) ~= -1 then vim.cmd(pos.line .. 'foldopen') else toggle_fold(pos.line) end
          return
        end
        open_row(r)
      end
    end
    map('<LeftMouse>', mouse(false))
    map('<2-LeftMouse>', mouse(true))
    map('<LeftDrag>', '<Nop>')
    map('<LeftRelease>', '<Nop>')
    -- Force normal mode every time the page is entered (the global
    -- "BufEnter * startinsert" fires before buftype is set), and give any
    -- window that shows the page its look.
    api.nvim_create_autocmd({ 'BufEnter', 'WinEnter', 'BufWinEnter' }, {
      buffer = buf,
      callback = function() vim.cmd('stopinsert'); set_win(0) end,
    })
  end,
})

-- Next / previous match (F4 / Shift+F4 in the app), from anywhere. Walks
-- the row table from the page's cursor (or the last jump), skipping
-- closed groups, and opens the match. Returns 'ok', 'end' or 'none'.
local function navigate_results(direction)
  local b = the_buf()
  if not b then return 'none' end
  local total = api.nvim_buf_line_count(b)
  local win
  for _, w in ipairs(api.nvim_list_wins()) do
    if api.nvim_win_get_buf(w) == b then win = w; break end
  end
  local cur = vim.b[b].termocode_nav_row or (direction > 0 and 0 or total + 1)
  if win then cur = api.nvim_win_get_cursor(win)[1] end
  if vim.b[b].termocode_nav_fresh and direction > 0 then cur = cur - 1 end
  vim.b[b].termocode_nav_fresh = nil
  local r = cur + direction
  while r >= 1 and r <= total do
    local row = F.rows[r]
    local hidden = win and api.nvim_win_call(win, function() return vim.fn.foldclosed(r) end) ~= -1
    if row and row.k == 'm' and not hidden then
      vim.b[b].termocode_nav_row = r
      if win then api.nvim_win_set_cursor(win, { r, 0 }) end
      open_row(row)
      return 'ok'
    end
    r = r + direction
  end
  return 'end'
end
_G._termocode_find_nav = navigate_results
`
