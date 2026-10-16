package app

// httpBufferLua wires up .http / .rest buffers (ADR 0002, plan 2.2):
//
//   - filetype http for *.http and *.rest, and a small syntax (no
//     Tree-sitter needed): ### separators, comments, @directives, file
//     variables, the request line, headers, {{placeholders}} and a JSON
//     body. Colours are the TermocodeHttp* groups of themeLuaBody.
//   - a decoration provider draws the lens at the end of each request
//     line ("▸ send Ctrl+Alt+R · 200 · 118 ms") and an undercurl under
//     unknown {{names}}, with ephemeral extmarks for the visible rows
//     only: nothing is stored, idle costs 0.
//   - Go owns the row table: _G._termocode_http_bufs[buf] = { [lnum] =
//     { lens = chunks, curls = {{s, e}, …}, w = cells } }. A change in
//     the buffer (150 ms debounce) notifies termocode_http_buf; Go
//     parses the text and calls _termocode_http_buf_set.
//   - a click on the lens sends that request (termocode_http_lens);
//     every other click is a normal click. Esc while a request runs
//     cancels it.
//
// Loaded once during attach (model.go::attachCmd) and on first use.
const httpBufferLua = `
local api = vim.api
_G._termocode_http_bufs = _G._termocode_http_bufs or {}
local B = _G._termocode_http_bufs
local ns = api.nvim_create_namespace('termocode_http')
pcall(vim.filetype.add, { extension = { http = 'http', rest = 'http' } })

function _G._termocode_http_buf_set(buf, rows, running)
  if not api.nvim_buf_is_valid(buf) then return end
  local t = {}
  for _, r in ipairs(rows) do t[r.l] = r end
  B[buf] = t
  _G._termocode_http_running = running
  for _, w in ipairs(vim.fn.win_findbuf(buf)) do pcall(api.nvim__redraw, { win = w, valid = false }) end
end

local function width(s) return vim.fn.strdisplaywidth(s) end

local function lens_width(r)
  if r.w then return r.w end
  local w = 0
  for _, c in ipairs(r.lens or {}) do w = w + width(c[1]) end
  r.w = w
  return w
end

api.nvim_set_decoration_provider(ns, {
  on_win = function(_, _, b)
    if vim.bo[b].filetype ~= 'http' or not B[b] then return false end
  end,
  on_line = function(_, _, b, row)
    local r = B[b] and B[b][row + 1]
    if not r then return end
    if r.lens and #r.lens > 0 then
      pcall(api.nvim_buf_set_extmark, b, ns, row, 0, { virt_text = r.lens, virt_text_pos = 'right_align', ephemeral = true })
    end
    for _, c in ipairs(r.curls or {}) do
      pcall(api.nvim_buf_set_extmark, b, ns, row, c[1], { end_col = c[2], hl_group = 'TermocodeHttpUnresolved', ephemeral = true })
    end
  end,
})

local syntax = [==[
syntax clear
syntax case match
syntax match TermocodeHttpPlaceholder /{{[^{}]*}}/ containedin=ALL
syntax match TermocodeHttpSep /^###.*$/ contains=TermocodeHttpSepTitle
syntax match TermocodeHttpSepTitle /^###\zs.*$/ contained
syntax match TermocodeHttpComment /^\s*\(#\|\/\/\).*$/ contains=TermocodeHttpDirective
syntax match TermocodeHttpDirective /^\s*\(#\|\/\/\)\s*\zs@\S\+.*$/ contained
syntax match TermocodeHttpVarLine /^@[A-Za-z0-9_.-]\+\s*=.*$/ contains=TermocodeHttpVarName,TermocodeHttpVarValue
syntax match TermocodeHttpVarName /^@[A-Za-z0-9_.-]\+/ contained
syntax match TermocodeHttpVarValue /=\s*\zs.*$/ contained contains=TermocodeHttpPlaceholder
syntax match TermocodeHttpRequest /\c^\(get\|post\|put\|patch\|delete\|head\|options\|trace\|connect\)\s.*$/ contains=TermocodeHttpMethod,TermocodeHttpURL,TermocodeHttpVersion
syntax match TermocodeHttpRequest /^\(https\?:\/\/\|{{\)\S.*$/ contains=TermocodeHttpURL,TermocodeHttpVersion
syntax match TermocodeHttpMethod /^\a\+\ze\s/ contained
syntax match TermocodeHttpURL /\(^\|\s\)\zs\(https\?:\/\/\|{{\|\/\)\S*/ contained contains=TermocodeHttpPlaceholder
syntax match TermocodeHttpVersion /\sHTTP\/\S\+$/ contained
syntax match TermocodeHttpHeader /^[A-Za-z0-9_-]\+:\(\s.*\)\?$/ contains=TermocodeHttpHeaderName,TermocodeHttpColon
syntax match TermocodeHttpHeaderName /^[A-Za-z0-9_-]\+\ze:/ contained
syntax match TermocodeHttpColon /:/ contained
]==]

local function set_syntax(buf)
  api.nvim_buf_call(buf, function()
    vim.cmd(syntax)
    if not vim.b.termocode_http_json then
      vim.b.current_syntax = nil
      local ok = pcall(vim.cmd, 'syntax include @termocodeHttpJsonBody syntax/json.vim')
      vim.b.termocode_http_json = ok
    end
    if vim.b.termocode_http_json then
      vim.cmd([==[syntax region TermocodeHttpBody start=/^\s*[{\[]/ end=/^###/me=s-1 keepend contains=@termocodeHttpJsonBody,TermocodeHttpPlaceholder]==])
    end
    vim.b.current_syntax = 'http'
  end)
end

local timers = {}
local function changed(buf)
  if timers[buf] then timers[buf]:stop() end
  timers[buf] = vim.defer_fn(function()
    timers[buf] = nil
    if api.nvim_buf_is_valid(buf) and _G.termocode_notify then _G.termocode_notify('termocode_http_buf', buf) end
  end, 150)
end

local function lens_hit(buf)
  local pos = vim.fn.getmousepos()
  if pos.winid == 0 or api.nvim_win_get_buf(pos.winid) ~= buf or pos.line <= 0 then return nil end
  local r = B[buf] and B[buf][pos.line]
  if not r or not r.lens or #r.lens == 0 then return nil end
  local ww = api.nvim_win_get_width(pos.winid)
  if pos.wincol > ww - lens_width(r) and pos.wincol <= ww then return pos.line end
end

vim.api.nvim_create_augroup('termocode_http', { clear = true })
vim.api.nvim_create_autocmd('FileType', {
  group = 'termocode_http',
  pattern = 'http',
  callback = function(args)
    local buf = args.buf
    vim.bo[buf].commentstring = '# %s'
    set_syntax(buf)
    api.nvim_create_autocmd('BufEnter', { group = 'termocode_http', buffer = buf, callback = function() changed(buf) end })
    -- Every change (typing, undo, the API, the AI) reaches on_lines.
    if not vim.b[buf].termocode_http_attached then
      vim.b[buf].termocode_http_attached = true
      api.nvim_buf_attach(buf, false, {
        on_lines = function() changed(buf) end,
        on_detach = function() pcall(function() vim.b[buf].termocode_http_attached = nil end) end,
      })
    end
    api.nvim_create_autocmd('BufWipeout', { group = 'termocode_http', buffer = buf, callback = function() B[buf] = nil end })
    local function click()
      local line = lens_hit(buf)
      if line then
        vim.schedule(function() if _G.termocode_notify then _G.termocode_notify('termocode_http_lens', buf, line) end end)
        return '<Ignore>'
      end
      return '<LeftMouse>'
    end
    vim.keymap.set({ 'n', 'i' }, '<LeftMouse>', click, { buffer = buf, expr = true, silent = true })
    vim.keymap.set('n', '<Esc>', function()
      if _G._termocode_http_running and _G.termocode_notify then
        _G.termocode_notify('termocode_http_cancel')
        return '<Ignore>'
      end
      return '<Esc>'
    end, { buffer = buf, expr = true, silent = true })
    changed(buf)
  end,
})
`
