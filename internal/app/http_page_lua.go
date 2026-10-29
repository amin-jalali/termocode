package app

// httpPageLua wires up the "HTTP Response" page (plan 2.6), built like
// the Find Results page (find_results_lua.go):
//
//   - the buffer holds the real text: row 1 the request line, row 2 the
//     status summary, row 3 empty (the hairline is drawn on it), then the
//     view (Body, Headers or Raw). _G._termocode_http.rows[lnum] says what
//     a later row is (k = 't' title, 'h' header, 'hop', 'note', 'key').
//   - a decoration provider draws the chrome (request name, the status
//     number colour, the view chips, the hairline, the key hints) with
//     ephemeral extmarks for the visible rows only: idle costs 0.
//   - keys and clicks notify termocode_http_key; Go does the work.
//
// Loaded once during attach (model.go::attachCmd) and on first use.
const httpPageLua = `
local api = vim.api
_G._termocode_http = _G._termocode_http or { rows = {}, head = {} }
local H = _G._termocode_http
local ns = api.nvim_create_namespace('termocode_http_page')

local function the_buf()
  local b = vim.g.termocode_http_buf
  if b and api.nvim_buf_is_valid(b) then return b end
end

local function put(b, s, e, lines)
  vim.bo[b].modifiable = true
  api.nvim_buf_set_lines(b, s, e, false, lines)
  vim.bo[b].modifiable = false
end

local function add_rows(rows)
  for _, r in ipairs(rows) do H.rows[r.l] = r end
end

local function notify(key)
  if _G.termocode_notify then _G.termocode_notify('termocode_http_key', key) end
end

-- Window look: no gutter, folds by indent (zc on a { folds it), the
-- cursor row in the selection wash.
local function set_win(win)
  local o = vim.wo[win]
  o.number, o.relativenumber = false, false
  o.signcolumn, o.foldcolumn = 'no', '0'
  pcall(function() o.statuscolumn = '' end)
  o.cursorline, o.wrap, o.list = true, false, false
  o.winhighlight = 'CursorLine:TermocodeFindCursor,Folded:TermocodeHttpFolded'
  o.foldmethod = 'indent'
  if not pcall(function() o.foldtext = '' end) then o.foldtext = 'getline(v:foldstart)' end
  o.foldlevel, o.fillchars = 99, 'fold: '
  o.conceallevel, o.scrolloff = 0, 2
end

local function apply_syntax(b)
  local want = H.head.syntax or ''
  if vim.bo[b].syntax ~= want then vim.bo[b].syntax = want end
  vim.bo[b].synmaxcol = 200
end

-- folds turns the page windows' indent folds on, or off (manual) while
-- the batches of a big body arrive: nvim then works the folds out once,
-- not again for every batch.
local function folds(b, on)
  for _, w in ipairs(vim.fn.win_findbuf(b)) do
    local want = on and 'indent' or 'manual'
    if vim.wo[w].foldmethod ~= want then vim.wo[w].foldmethod = want end
  end
end

-- _termocode_http_open(tmp, split, head, lines, rows, more): fill the
-- page and show it (a vertical split on the right when split, else the
-- current window; the .http file stays the alternate buffer). more:
-- batches follow (_termocode_http_append).
function _G._termocode_http_open(tmp, split, h, lines, rows, more)
  local b = the_buf()
  local fresh = false
  if not b then
    b = api.nvim_create_buf(true, false)
    vim.bo[b].buftype = 'nofile'
    vim.bo[b].swapfile = false
    vim.bo[b].bufhidden = 'hide'
    vim.bo[b].undolevels = -1
    pcall(api.nvim_buf_set_name, b, vim.fn.fnamemodify(tmp, ':h') .. '/HTTP Response')
    vim.g.termocode_http_buf = b
    fresh = true
  end
  H.head = h
  H.rows = {}
  add_rows(rows)
  folds(b, false)
  put(b, 0, -1, lines)
  if fresh then vim.bo[b].filetype = 'httpresponse' end
  apply_syntax(b)
  local wins = vim.fn.win_findbuf(b)
  if #wins == 0 then
    if split then
      local cur = api.nvim_get_current_win()
      vim.cmd('rightbelow vsplit')
      api.nvim_win_set_buf(0, b)
      set_win(0)
      api.nvim_set_current_win(cur)
    else
      vim.cmd('buffer ' .. b)
      set_win(0)
    end
  end
  folds(b, not more)
  for _, w in ipairs(vim.fn.win_findbuf(b)) do
    if not split or #vim.fn.win_findbuf(b) > 0 then pcall(api.nvim_win_set_cursor, w, { 1, 0 }) end
  end
  vim.cmd('stopinsert')
end

-- _termocode_http_head(h): rows 1 and 2 only (progress, sending).
function _G._termocode_http_head(h)
  local b = the_buf(); if not b then return end
  H.head = h
  put(b, 0, 2, { h.req or '', h.summary or '' })
end

-- _termocode_http_append(lines, rows, last): one more batch of a big
-- body.
-- last: the final batch; the indent folds come back on.
function _G._termocode_http_append(lines, rows, last)
  local b = the_buf(); if not b then return end
  add_rows(rows)
  put(b, -1, -1, lines)
  if last then folds(b, true) end
end

local function width(s) return vim.fn.strdisplaywidth(s) end

local function chunks_width(cs)
  local w = 0
  for _, c in ipairs(cs) do w = w + width(c[1]) end
  return w
end

local function chips(w)
  local out = {}
  for i, name in ipairs(H.head.chips or {}) do
    local label = name
    if w < 90 and name == 'Headers' then label = 'Hdrs' end
    local on = (H.head.view or 0) + 1 == i
    out[#out + 1] = { ' ' .. label .. ' ', on and 'TermocodeHttpChipOn' or 'TermocodeHttpChipOff' }
  end
  out[#out + 1] = { ' ', 'Normal' }
  return out
end

-- The key hints, dropping pairs from the middle until they fit.
local function hint_chunks(room)
  local hs = H.head.hints or {}
  local keep = {}
  for i = 1, #hs do keep[i] = true end
  local function build()
    local out, w = {}, 0
    for i, p in ipairs(hs) do
      if keep[i] then
        out[#out + 1] = { ' ' .. p[1] .. ' ', 'TermocodeHttpKeycap' }
        out[#out + 1] = { ' ' .. p[2] .. '  ', 'TermocodeHttpHint' }
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
  if w > room then return {}, 0 end
  return out, w
end

local cur_w = 80
api.nvim_set_decoration_provider(ns, {
  on_win = function(_, win, b)
    if vim.bo[b].filetype ~= 'httpresponse' then return false end
    cur_w = api.nvim_win_get_width(win)
  end,
  on_line = function(_, _, b, row)
    local lnum = row + 1
    local line = api.nvim_buf_get_lines(b, row, row + 1, false)[1] or ''
    local function E(col, o)
      o.ephemeral = true
      pcall(api.nvim_buf_set_extmark, b, ns, row, col, o)
    end
    local h = H.head
    if lnum == 1 then
      E(0, { end_col = #line, hl_group = 'TermocodeHttpReq' })
      local right = {}
      if h.by and h.by ~= '' then right[#right + 1] = { ' ' .. h.by .. ' ', 'TermocodeHttpByAI' } end
      if h.name and h.name ~= '' then right[#right + 1] = { ' ' .. h.name .. ' ', 'TermocodeHttpName' } end
      -- Only when it fits after the URL (never over the text).
      if #right > 0 and width(line) + chunks_width(right) + 1 <= cur_w then
        E(0, { virt_text = right, virt_text_pos = 'right_align' })
      end
    elseif lnum == 2 then
      E(0, { end_col = #line, hl_group = 'TermocodeHttpSummary' })
      if (h.st_n or 0) > 0 then E(0, { end_col = math.min(h.st_n, #line), hl_group = h.st_hl, priority = 5000 }) end
      if line ~= '' and h.summary and not h.summary:find('sending', 1, true) then
        local cs = chips(cur_w)
        if width(line) + chunks_width(cs) + 1 <= cur_w then
          E(0, { virt_text = cs, virt_text_pos = 'right_align' })
        end
      end
    elseif lnum == 3 then
      local hints, hw = hint_chunks(cur_w - 8)
      local bar = cur_w - hw - 1
      local a = math.max(1, math.floor(bar / 6))
      local chunks = {
        { ('─'):rep(a), 'TermocodeFindHair1' },
        { ('─'):rep(a), 'TermocodeFindHair2' },
        { ('─'):rep(math.max(0, bar - 2 * a)), 'TermocodeFindHair3' },
        { ' ', 'Normal' },
      }
      for _, c in ipairs(hints) do chunks[#chunks + 1] = c end
      E(0, { virt_text = chunks, virt_text_pos = 'overlay' })
    else
      local r = H.rows[lnum]
      if not r then return end
      if r.k == 't' then
        E(0, { end_col = r.n or #line, hl_group = 'TermocodeHttpSection' })
        if r.n and r.n < #line then E(r.n, { end_col = #line, hl_group = 'TermocodeHttpSummary' }) end
      elseif r.k == 'h' then
        E(0, { end_col = math.min(r.n or 0, #line), hl_group = 'TermocodeHttpHdrName' })
      elseif r.k == 'hop' then
        E(0, { end_col = #line, hl_group = 'TermocodeHttpHop' })
      elseif r.k == 'note' then
        E(0, { end_col = #line, hl_group = 'TermocodeHttpSummary' })
      elseif r.k == 'key' then
        E(2, { end_col = math.min(2 + (r.n or 1), #line), hl_group = 'TermocodeHttpKeycap' })
        E(math.min(2 + (r.n or 1), #line), { end_col = #line, hl_group = 'TermocodeHttpHint' })
      end
    end
  end,
})

vim.api.nvim_create_augroup('termocode_http_page', { clear = true })
vim.api.nvim_create_autocmd('FileType', {
  group = 'termocode_http_page',
  pattern = 'httpresponse',
  callback = function(args)
    local buf = args.buf
    vim.bo[buf].swapfile, vim.bo[buf].buflisted = false, true
    set_win(0)
    local function map(lhs, key)
      vim.keymap.set('n', lhs, function() notify(key) end, { buffer = buf, silent = true, nowait = true })
    end
    for _, k in ipairs({ 'r', 'c', 'C', 's', 'S', 'h', 'e', 'k', 'x', 'o', 'q', '1', '2', '3' }) do map(k, k) end
    map('<Tab>', 'tab')
    map('<Esc>', 'esc')
    -- Mouse: the chips on row 2, the request name on row 1, a redirect
    -- hop (copies its URL); everywhere else a plain click.
    vim.keymap.set('n', '<LeftMouse>', function()
      local pos = vim.fn.getmousepos()
      if pos.winid ~= 0 and pos.winid ~= api.nvim_get_current_win() then pcall(api.nvim_set_current_win, pos.winid) end
      if pos.line <= 0 then return end
      pcall(api.nvim_win_set_cursor, 0, { pos.line, math.max(0, pos.column - 1) })
      local w = api.nvim_win_get_width(0)
      if pos.line == 2 then
        local col = w
        local cs = chips(w)
        for i = #cs, 1, -1 do
          local cw = width(cs[i][1])
          if pos.wincol > col - cw and pos.wincol <= col then
            local label = vim.trim(cs[i][1])
            if label ~= '' then notify('view:' .. label) end
            return
          end
          col = col - cw
        end
      elseif pos.line == 1 and H.head.name and H.head.name ~= '' then
        if pos.wincol > w - width(H.head.name) - 2 then notify('jump') end
      else
        local r = H.rows[pos.line]
        if r and r.k == 'hop' and r.url then notify('hop:' .. r.url) end
      end
    end, { buffer = buf, silent = true })
    vim.keymap.set('n', '<LeftDrag>', '<Nop>', { buffer = buf })
    vim.keymap.set('n', '<LeftRelease>', '<Nop>', { buffer = buf })
    api.nvim_create_autocmd({ 'BufEnter', 'WinEnter', 'BufWinEnter' }, {
      buffer = buf,
      callback = function() vim.cmd('stopinsert'); set_win(0) end,
    })
  end,
})
`
