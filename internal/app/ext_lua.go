package app

// extLua installs the extension host inside the embedded nvim (Group I).
//
// Public API — the global "termocode" table (see docs/extensions.md):
//
//	termocode.register_command{ id, title, run, hint? }
//	termocode.register_panel{ id, title, icon?, render(w, h) -> lines, on_select?(row, line) }
//	termocode.register_status_item{ id, text = string | fn() -> string, command? }
//	termocode.on(event, fn)            ready|save|open|changed|cursor|shutdown|<nvim autocmd>
//	termocode.notify(msg, level?, detail?)   level: info|warn|error
//	termocode.prompt({ title, label, default }, cb(value|nil))
//	termocode.pick({ title, items = { 'a' | { id, title, hint } } }, cb(item|nil))
//	termocode.preview(title, body)
//	termocode.open(path, line?, col?)
//	termocode.refresh(panel_id?)
//	termocode.run(command_id)
//	termocode.log(msg)
//	termocode.escape(text)             make text safe inside {{token}} markup
//	termocode.wrap(fn)                 guard a callback (timers, jobs)
//
// Private side — _G._termocode_ext, called by Go (ext.go):
//
//	load(json)  registry()  poll(panel, w, h)  run(id)  select(panel, row)
//	resolve(req, value)  emit(event)  reset()  log_text()
//
// Every call into extension code goes through H.guard: pcall + a time
// budget enforced with a debug count hook. LuaJIT does not run hooks in
// JIT-compiled traces, so every extension chunk (and every module it
// requires through the extension searcher) is jit.off()'d recursively —
// extension code runs in the interpreter, where the hook always fires.
// A C call that blocks (vim.fn.system('sleep 60')) cannot be interrupted;
// Go runs these calls off the UI goroutine so the UI stays responsive.
//
// No backticks anywhere in this chunk (Go raw string, see ADR 0002).
const extLua = `
local uv = vim.uv or vim.loop
_G.termocode = _G.termocode or {}
local T = _G.termocode
local H = _G._termocode_ext or {}
_G._termocode_ext = H

T.api_version = 1
H.budget_ms = 250
H.load_budget_ms = 2000
H.log_lines = H.log_lines or {}

local function pack(...) return { n = select('#', ...), ... } end

local function arr(t)
  if type(t) ~= 'table' or #t == 0 then return vim.NIL end
  return t
end

local function log(ext, msg)
  table.insert(H.log_lines, os.date('%H:%M:%S') .. '  [' .. tostring(ext or '?') .. '] ' .. tostring(msg))
  if #H.log_lines > 500 then table.remove(H.log_lines, 1) end
end
H.log = log

local function to_go(method, ...)
  if _G.termocode_notify then pcall(_G.termocode_notify, method, ...) end
end

function H.report(ext, what, err)
  local msg = tostring(err)
  log(ext, 'ERROR ' .. what .. ': ' .. msg)
  to_go('termocode_ext_error', tostring(ext or '?'), what, msg)
end

-- guard runs fn(...) as extension ext with a time budget (ms). Returns
-- ok, results... and reports failures (toast + log) itself.
function H.guard(ext, what, budget, fn, ...)
  if type(fn) ~= 'function' then return false end
  local deadline = uv.hrtime() + (budget or H.budget_ms) * 1e6
  local ph, pm, pc = debug.gethook()
  local prev = H.current
  H.current = ext
  debug.sethook(function()
    if uv.hrtime() > deadline then
      debug.sethook()
      error('timed out after ' .. tostring(budget or H.budget_ms) .. ' ms', 2)
    end
  end, '', 1000)
  local res = pack(pcall(fn, ...))
  if ph then debug.sethook(ph, pm, pc) else debug.sethook() end
  H.current = prev
  if not res[1] then
    H.report(ext, what, res[2])
    return false
  end
  return true, unpack(res, 2, res.n)
end

-- wrap makes fn safe to hand to nvim (autocmds, timers, job callbacks):
-- it runs guarded as ext.
local function wrap(ext, what, fn)
  return function(...)
    local r = pack(H.guard(ext, what, H.budget_ms, fn, ...))
    return unpack(r, 2, r.n)
  end
end
T.wrap = function(fn, what) return wrap(H.current or 'user', what or 'callback', fn) end

function H.reset()
  for name, _ in pairs(H.exts or {}) do
    pcall(vim.api.nvim_del_augroup_by_name, 'termocode_ext_' .. name)
  end
  for mod, _ in pairs(H.modules or {}) do package.loaded[mod] = nil end
  H.exts, H.order = {}, {}
  H.commands, H.command_order = {}, {}
  H.panels, H.panel_order = {}, {}
  H.status, H.status_order = {}, {}
  H.handlers = { ready = {} }
  H.pending, H.seq = {}, 0
  H.modules, H.module_dirs = {}, {}
  H.current = nil
end
if not H.exts then H.reset() end

-- require() support: <ext>/lua/<mod>.lua, loaded with the JIT off so the
-- time budget holds for module code too.
local function ext_searcher(modname)
  local rel = modname:gsub('%.', '/')
  for _, d in ipairs(H.module_dirs or {}) do
    for _, p in ipairs({ d.dir .. '/lua/' .. rel .. '.lua', d.dir .. '/lua/' .. rel .. '/init.lua' }) do
      if uv.fs_stat(p) then
        local chunk, err = loadfile(p)
        if not chunk then error(err, 0) end
        if jit then jit.off(chunk, true) end
        H.modules[modname] = d.ext
        return chunk
      end
    end
  end
  return '\n\tno termocode extension module ' .. modname
end
if not H.searcher_installed then
  local loaders = package.loaders or package.searchers
  table.insert(loaders, 2, ext_searcher)
  H.searcher_installed = true
end

local function owner() return H.current or 'user' end

local function qualify(ext, id)
  id = tostring(id or '')
  if id == '' then error('id is required', 3) end
  if not id:find('.', 1, true) then id = ext .. '.' .. id end
  return id
end

-- ── public API ──────────────────────────────────────────────────────────

function T.register_command(spec)
  if type(spec) ~= 'table' or type(spec.run) ~= 'function' then
    error('register_command{ id, title, run } needs a run function', 2)
  end
  local ext = owner()
  local id = qualify(ext, spec.id)
  if not H.commands[id] then table.insert(H.command_order, id) end
  H.commands[id] = { id = id, title = tostring(spec.title or id), hint = tostring(spec.hint or ''), ext = ext, run = spec.run }
  return id
end

function T.register_panel(spec)
  if type(spec) ~= 'table' or type(spec.render) ~= 'function' then
    error('register_panel{ id, title, render } needs a render function', 2)
  end
  local ext = owner()
  local id = qualify(ext, spec.id)
  if not H.panels[id] then table.insert(H.panel_order, id) end
  H.panels[id] = {
    id = id, ext = ext, title = tostring(spec.title or id), icon = tostring(spec.icon or ''),
    render = spec.render, on_select = spec.on_select, failures = 0,
  }
  return id
end

function T.register_status_item(spec)
  if type(spec) ~= 'table' or (type(spec.text) ~= 'string' and type(spec.text) ~= 'function') then
    error('register_status_item{ id, text } needs text (string or function)', 2)
  end
  local ext = owner()
  local id = qualify(ext, spec.id)
  if not H.status[id] then table.insert(H.status_order, id) end
  local cmd = spec.command and qualify(ext, spec.command) or ''
  H.status[id] = { id = id, ext = ext, text = spec.text, command = cmd, failures = 0 }
  return id
end

local EVENTS = {
  save = { 'BufWritePost' },
  open = { 'BufEnter' },
  changed = { 'TextChanged', 'TextChangedI' },
  cursor = { 'CursorHold', 'CursorHoldI' },
  shutdown = { 'VimLeavePre' },
}
local FILE_EVENTS = { save = true, open = true, changed = true }

function T.on(event, fn)
  if type(fn) ~= 'function' then error('on(event, fn) needs a function', 2) end
  local ext = owner()
  if event == 'ready' then
    table.insert(H.handlers.ready, { ext = ext, fn = fn })
    return
  end
  local aus = EVENTS[event]
  if not aus and type(event) == 'string' and event:match('^%u') then aus = { event } end
  if not aus then error('unknown event ' .. tostring(event), 2) end
  local group = vim.api.nvim_create_augroup('termocode_ext_' .. ext, { clear = false })
  vim.api.nvim_create_autocmd(aus, {
    group = group,
    callback = function(ev)
      if FILE_EVENTS[event] and vim.bo[ev.buf].buftype ~= '' then return end
      local path = ''
      if ev.file and ev.file ~= '' then path = vim.fn.fnamemodify(ev.file, ':p') end
      H.guard(ext, 'on(' .. event .. ')', H.budget_ms, fn,
        { event = event, buf = ev.buf, path = path, match = ev.match })
    end,
  })
end

function T.notify(msg, level, detail)
  level = tostring(level or 'info')
  to_go('termocode_ext_notify', level, tostring(msg or ''), tostring(detail or ''))
end

function T.log(msg) log(owner(), msg) end

local function new_request(cb, items)
  H.seq = H.seq + 1
  H.pending[H.seq] = { ext = owner(), cb = cb, items = items }
  return H.seq
end

function T.prompt(opts, cb)
  if type(opts) == 'string' then opts = { title = opts } end
  opts = opts or {}
  local id = new_request(cb)
  to_go('termocode_ext_prompt', id, tostring(opts.title or 'Input'),
    tostring(opts.label or ''), tostring(opts.default or ''))
end

function T.pick(opts, cb)
  opts = opts or {}
  local items = {}
  for i, it in ipairs(opts.items or {}) do
    if type(it) ~= 'table' then it = { id = tostring(it), title = tostring(it) } end
    items[i] = it
  end
  local wire = {}
  for i, it in ipairs(items) do
    wire[i] = { title = tostring(it.title or it.id or i), hint = tostring(it.hint or '') }
  end
  local id = new_request(cb, items)
  to_go('termocode_ext_pick', id, tostring(opts.title or 'Pick'), wire)
end

function T.preview(title, body)
  to_go('termocode_ext_preview', tostring(title or ''), tostring(body or ''))
end

function T.open(path, line, col)
  to_go('termocode_ext_open', tostring(path or ''), tonumber(line) or 0, tonumber(col) or 0)
end

function T.refresh(panel_id)
  local id = ''
  if panel_id then id = qualify(owner(), panel_id) end
  to_go('termocode_ext_panel_dirty', id)
end

function T.run(id)
  local c = H.commands[id] or H.commands[qualify(owner(), id)]
  if not c then error('unknown command ' .. tostring(id), 2) end
  return H.guard(c.ext, 'command ' .. c.id, H.budget_ms, c.run)
end

function T.extension() return owner() end

-- escape makes text safe to put inside panel / status markup.
function T.escape(s)
  return (tostring(s or ''):gsub('{{', '{{{{'))
end

-- ── called by Go ────────────────────────────────────────────────────────

-- load(json) loads every extension in the list [{name, dir, init,
-- version}] and returns the registry JSON.
function H.load(list_json)
  local ok, list = pcall(vim.json.decode, list_json)
  if not ok or type(list) ~= 'table' then list = {} end
  for _, e in ipairs(list) do
    local name = tostring(e.name)
    H.exts[name] = { name = name, version = e.version or '', ok = false, error = '' }
    table.insert(H.order, name)
    table.insert(H.module_dirs, { ext = name, dir = e.dir })
    local chunk, err = loadfile(e.init)
    if not chunk then
      H.exts[name].error = tostring(err)
      H.report(name, 'load', err)
    else
      if jit then jit.off(chunk, true) end
      local ran = H.guard(name, 'load', H.load_budget_ms, chunk)
      H.exts[name].ok = ran
      if ran then log(name, 'loaded ' .. (e.version ~= '' and ('v' .. e.version) or '')) end
      if not ran then H.exts[name].error = 'init.lua failed (see log)' end
    end
  end
  return H.registry()
end

function H.emit(event)
  for _, h in ipairs(H.handlers[event] or {}) do
    H.guard(h.ext, 'on(' .. event .. ')', H.budget_ms, h.fn, { event = event })
  end
end

function H.registry()
  local cmds, panels, status, exts = {}, {}, {}, {}
  for _, id in ipairs(H.command_order) do
    local c = H.commands[id]
    cmds[#cmds + 1] = { id = id, title = c.title, ext = c.ext, hint = c.hint }
  end
  for _, id in ipairs(H.panel_order) do
    local p = H.panels[id]
    panels[#panels + 1] = { id = id, title = p.title, icon = p.icon, ext = p.ext }
  end
  for _, id in ipairs(H.status_order) do
    local s = H.status[id]
    status[#status + 1] = { id = id, ext = s.ext, command = s.command }
  end
  for _, n in ipairs(H.order) do
    local e = H.exts[n]
    exts[#exts + 1] = { name = n, version = e.version, ok = e.ok, error = e.error }
  end
  return vim.json.encode({ commands = arr(cmds), panels = arr(panels), status = arr(status), extensions = arr(exts) })
end

local function clean_lines(t)
  local out = {}
  if type(t) == 'string' then t = vim.split(t, '\n', { plain = true }) end
  if type(t) ~= 'table' then return out end
  for i, l in ipairs(t) do
    if i > 2000 then break end
    out[#out + 1] = tostring(l)
  end
  return out
end

-- Repeated failures switch a render / status callback off until reload,
-- so one broken extension cannot spam toasts every heartbeat.
local MAX_FAILURES = 3

function H.poll(panel_id, w, h)
  local out = { status = {} }
  for _, id in ipairs(H.status_order) do
    local s = H.status[id]
    local text = s.text
    if type(text) == 'function' then
      if s.failures >= MAX_FAILURES then
        text = '{{error}}' .. s.ext .. ' ✘{{/}}'
      else
        local ok, v = H.guard(s.ext, 'status ' .. id, H.budget_ms, text)
        if ok then text = v else s.failures = s.failures + 1; text = '' end
      end
    end
    out.status[#out.status + 1] = { id = id, text = text == nil and '' or tostring(text) }
  end
  out.status = arr(out.status)
  local p = panel_id ~= '' and H.panels[panel_id] or nil
  if p then
    out.panel = panel_id
    if p.failures >= MAX_FAILURES then
      out.lines = { '{{error}}This panel failed ' .. p.failures .. ' times.{{/}}',
        '{{muted}}See Extensions: Show Log, then Extensions: Reload.{{/}}' }
    else
      local ok, lines = H.guard(p.ext, 'render ' .. panel_id, H.budget_ms, p.render, w, h)
      if ok then
        p.failures = 0
        out.lines = arr(clean_lines(lines))
      else
        p.failures = p.failures + 1
        out.lines = { '{{error}}render failed{{/}} {{muted}}(Extensions: Show Log){{/}}' }
      end
    end
  end
  return vim.json.encode(out)
end

function H.run(id)
  local c = H.commands[id]
  if not c then
    H.report('termocode', 'run', 'unknown command ' .. tostring(id))
    return
  end
  H.guard(c.ext, 'command ' .. id, H.budget_ms, c.run)
end

function H.select(panel_id, row, line)
  local p = H.panels[panel_id]
  if not p or type(p.on_select) ~= 'function' then return end
  H.guard(p.ext, 'select ' .. panel_id, H.budget_ms, p.on_select, row, line)
end

-- resolve finishes a prompt (value = string, or nil when cancelled) or a
-- pick (value = 1-based index, or nil).
function H.resolve(req, value)
  local r = H.pending[req]
  H.pending[req] = nil
  if not r or type(r.cb) ~= 'function' then return end
  local arg = value
  if r.items then arg = value and r.items[tonumber(value)] or nil end
  H.guard(r.ext, 'callback', H.budget_ms, r.cb, arg)
end

function H.log_text()
  return table.concat(H.log_lines, '\n')
end
`
