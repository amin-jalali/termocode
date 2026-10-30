package app

// The Lua side of the web file and terminal views (WEB-17, WEB-19,
// WEB-21). Every chunk is a constant: a web message never carries Lua, an
// RPC name or a command for nvim (never-do 17); paths, numbers and text
// travel as RPC arguments. Each chunk starts with webLuaPrelude, which
// defines _G.termocode_web once per nvim (a version check makes a newer
// binary redefine it).
//
// A buffer is attached once (nvim_buf_attach with Lua callbacks), however
// many browsers follow it; the hub counts the browsers. Each callback
// sends one rpcnotify with a per-buffer sequence number, so the hub sees
// a lost notification (the event channel drops when full) as a gap and
// reads the whole buffer again instead of drifting. The modified flag
// rides along: nvim does not fire BufModifiedSet for API edits.
//
//	termocode_web_lines  buf, seq, tick, first, last, lines, modified
//	termocode_web_tick   buf, seq, tick, modified
//	termocode_web_reset  buf, seq, tick, lines      (reloaded from disk)
//	termocode_web_dirty  buf, modified
//	termocode_web_gone   buf                        (unloaded or deleted)

const webLuaPrelude = `
local W = _G.termocode_web
if not W or W.v ~= 2 then
  W = { v = 2, bufs = (W and W.bufs) or {} }
  _G.termocode_web = W
  local api = vim.api
  local function notify(...)
    if _G.termocode_notify then _G.termocode_notify(...) end
  end
  function W.info(b)
    local bo = vim.bo[b]
    return { buf = b, tick = api.nvim_buf_get_changedtick(b), dirty = bo.modified,
      ft = bo.filetype, readonly = bo.readonly or not bo.modifiable,
      name = api.nvim_buf_get_name(b), bt = bo.buftype, count = api.nvim_buf_line_count(b),
      winw = -1, winh = -1 }
  end
  function W.size(i)
    local w = vim.fn.bufwinid(i.buf)
    if w ~= -1 then i.winw, i.winh = api.nvim_win_get_width(w), api.nvim_win_get_height(w) end
    return i
  end
  function W.attach(b, tail)
    if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
    local st = W.bufs[b]
    if not st then
      st = { seq = 0 }
      W.bufs[b] = st
      api.nvim_buf_attach(b, false, {
        on_lines = function(_, buf, tick, first, last, new_last)
          if W.bufs[buf] ~= st then return true end
          st.seq = st.seq + 1
          notify('termocode_web_lines', buf, st.seq, tick, first, last,
            api.nvim_buf_get_lines(buf, first, new_last, false), vim.bo[buf].modified)
        end,
        on_changedtick = function(_, buf, tick)
          if W.bufs[buf] ~= st then return true end
          st.seq = st.seq + 1
          notify('termocode_web_tick', buf, st.seq, tick, vim.bo[buf].modified)
        end,
        on_reload = function(_, buf)
          if W.bufs[buf] ~= st then return end
          st.seq = st.seq + 1
          notify('termocode_web_reset', buf, st.seq, api.nvim_buf_get_changedtick(buf),
            api.nvim_buf_get_lines(buf, 0, -1, false))
        end,
        on_detach = function(_, buf)
          if W.bufs[buf] == st then
            W.bufs[buf] = nil
            notify('termocode_web_gone', buf)
          end
        end,
      })
    end
    local i = W.size(W.info(b))
    i.seq = st.seq
    i.first = 0
    if tail > 0 and i.count > tail then i.first = i.count - tail end
    i.lines = api.nvim_buf_get_lines(b, i.first, -1, false)
    return i
  end
  function W.detach(list)
    for _, d in ipairs(list) do
      local b = d[1]
      W.bufs[b] = nil -- the next callback returns true: nvim detaches
      if d[2] and api.nvim_buf_is_valid(b) and not vim.bo[b].buflisted and not vim.bo[b].modified
        and vim.fn.bufwinid(b) == -1 then
        pcall(api.nvim_buf_delete, b, {})
      end
    end
  end
  api.nvim_create_augroup('TermocodeWeb', { clear = true })
  api.nvim_create_autocmd({ 'BufModifiedSet', 'BufWritePost' }, { group = 'TermocodeWeb', callback = function(ev)
    if W.bufs[ev.buf] then notify('termocode_web_dirty', ev.buf, vim.bo[ev.buf].modified) end
  end })
end
`

// webLuaAttach attaches buffer b (once) and returns its state and lines
// (the last tail lines when tail > 0).
const webLuaAttach = webLuaPrelude + `
local b, tail = ...
return W.attach(b, tail)`

// webLuaOpen loads a file into a hidden buffer (bufadd + bufload: the
// laptop's windows do not change, the buffer is not listed) and attaches
// it. A swap file does not ask (shortmess A). A file over maxlines is
// refused and, when this call loaded it, unloaded again.
const webLuaOpen = webLuaPrelude + `
local path, maxlines = ...
local api = vim.api
local b = vim.fn.bufadd(path)
local loaded = api.nvim_buf_is_loaded(b)
if not loaded then
  local sm = vim.o.shortmess
  vim.o.shortmess = sm .. 'A'
  local ok, err = pcall(vim.fn.bufload, b)
  vim.o.shortmess = sm
  if not ok then return { err = 'load', msg = tostring(err) } end
end
if vim.bo[b].buftype ~= '' then return { err = 'special' } end
if api.nvim_buf_line_count(b) > maxlines then
  if not loaded then W.detach({ { b, true } }) end
  return { err = 'lines' }
end
local i = W.attach(b, 0)
i.created = not loaded
return i`

// webLuaDetach lets go of buffers: a list of {buf, unload}. unload drops
// a buffer the web loaded when it is still hidden, unlisted and saved.
const webLuaDetach = webLuaPrelude + `
local list = ...
W.detach(list)
return true`

// webLuaRows reads rows [from, to) of a terminal buffer (term.history),
// clamped to the buffer.
const webLuaRows = webLuaPrelude + `
local b, from, to = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or vim.bo[b].buftype ~= 'terminal' then return { err = 'gone' } end
local n = api.nvim_buf_line_count(b)
if to > n then to = n end
if from > to then from = to end
return { first = from, lines = api.nvim_buf_get_lines(b, from, to, false) }`

// webLuaSetText replaces a range of buffer b if its changedtick is still
// tick: one chunk, so nothing can come in between (nvim runs it at
// once). Positions are checked here, against the buffer itself: rows in
// the buffer, columns inside the line and not inside a multi-byte
// character. An undo break first makes each web edit one undo step.
const webLuaSetText = webLuaPrelude + `
local b, tick, sr, sc, er, ec, lines = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
if not vim.bo[b].modifiable or vim.bo[b].readonly then return { err = 'readonly' } end
local cur = api.nvim_buf_get_changedtick(b)
if cur ~= tick then return { err = 'conflict', tick = cur } end
local n = api.nvim_buf_line_count(b)
if sr >= n or er >= n then return { err = 'range' } end
local function col_ok(r, c)
  local l = api.nvim_buf_get_lines(b, r, r + 1, true)[1]
  if c > #l then return false end
  local byte = l:byte(c + 1)
  return not (byte and byte >= 0x80 and byte < 0xC0)
end
if not col_ok(sr, sc) or not col_ok(er, ec) then return { err = 'range' } end
vim.bo[b].undolevels = vim.bo[b].undolevels
local ok, e = pcall(api.nvim_buf_set_text, b, sr, sc, er, ec, lines)
if not ok then return { err = 'failed', msg = tostring(e) } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = vim.bo[b].modified }`

// webLuaSave writes buffer b the way :write on the laptop does (its
// BufWrite hooks run: format on save, EditorConfig). A file changed on
// disk since it was read is not overwritten: checktime first, and the
// laptop's FileChangedShell handler marks it (file_reload_lua.go).
const webLuaSave = webLuaPrelude + `
local b = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
if vim.bo[b].buftype ~= '' then return { err = 'special' } end
pcall(vim.cmd, 'checktime ' .. b)
local warned = vim.b[b].termocode_disk_warned
if warned and warned ~= 'deleted' and warned ~= 'mode' then return { err = 'disk' } end
local ok, e = pcall(api.nvim_buf_call, b, function() vim.cmd('silent write') end)
if not ok then return { err = 'write', msg = tostring(e) } end
if vim.bo[b].modified then return { err = 'write', msg = 'The file was not written.' } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = false }`

// webLuaUndo undoes one step on buffer b.
const webLuaUndo = webLuaPrelude + `
local b = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local before = vim.fn.undotree(b).seq_cur
local ok, e = pcall(api.nvim_buf_call, b, function() vim.cmd('silent undo') end)
if not ok then return { err = 'failed', msg = tostring(e) } end
if vim.fn.undotree(b).seq_cur == before then return { err = 'none' } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = vim.bo[b].modified }`
