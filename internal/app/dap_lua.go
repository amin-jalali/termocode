package app

// dapSetupLua returns the Lua chunk that:
//  1. Prepends mfussenegger/nvim-dap to runtimepath so require('dap') resolves.
//  2. Auto-detects available debug adapters (delve / debugpy / node) and
//     registers them, plus a baseline configuration table per language.
//  3. Hooks dap.listeners so every session event reaches Go through
//     _G.termocode_notify('termocode_dap', <kind>, <json>) (debug_state.go).
//  4. Defines the _G._termocode_dap helper module. Every query returns JSON
//     (decoded by debug_state.go) — no text formatting happens in Lua:
//     state()                   whole-session snapshot
//     frames() / threads() / scopes()
//     variables(reqid, ref)     async → termocode_dap_result(reqid, json)
//     evaluate(reqid, expr, ctx) async → termocode_dap_result(reqid, json)
//     breakpoints()             live breakpoints of every loaded buffer
//     set_stored(json)          persisted breakpoints (restored on BufReadPost)
//
// Adapters that aren't installed are silently skipped. The chunk is
// idempotent: re-running it re-registers the same tables and listeners.
func dapSetupLua(pluginPath string) string {
	escaped := luaEscape(pluginPath)
	return `
local plugin_path = '` + escaped + `'
if plugin_path ~= '' and vim.fn.isdirectory(plugin_path) == 1 then
  vim.opt.rtp:prepend(plugin_path)
end

local H = {}
_G._termocode_dap = H

local function enc(v)
  local ok, s = pcall(vim.json.encode, v)
  if ok then return s end
  return 'null'
end

local function emit(method, ...)
  if _G.termocode_notify then pcall(_G.termocode_notify, method, ...) end
end

local function trunc(s, n)
  s = tostring(s or '')
  if #s > n then
    return vim.fn.strcharpart(s, 0, n) .. '…'
  end
  return s
end

local function nonempty(s)
  if type(s) == 'string' and s ~= '' then return s end
  return nil
end

-- relocate re-checks a persisted breakpoint line against the buffer's
-- current lines: the stored line text wins (searched up to 50 lines away),
-- then the stored number when it is still inside the file. Blank lines move
-- to the next code line (within 5). nil = drop the breakpoint.
function H.relocate(lines, line, text)
  if type(line) ~= 'number' or line < 1 then return nil end
  local function at(i)
    local l = lines[i]
    if l == nil then return nil end
    return vim.trim(l)
  end
  text = vim.trim(text or '')
  if text ~= '' then
    if at(line) == text then return line end
    for d = 1, 50 do
      if at(line - d) == text then return line - d end
      if at(line + d) == text then return line + d end
    end
  end
  if line > #lines then return nil end
  if at(line) == '' then
    for d = 1, 5 do
      local l = at(line + d)
      if l and l ~= '' then return line + d end
    end
    return nil
  end
  return line
end

local ok, dap = pcall(require, 'dap')
H.available = function() return ok end
if not ok then
  -- nvim-dap isn't on rtp (clone failed). Stub the helpers so Go-side
  -- calls don't blow up; they just report "unavailable".
  local function unavailable(reqid)
    emit('termocode_dap_result', reqid, enc({ error = 'nvim-dap not installed' }))
  end
  H.session_active = function() return false end
  H.state       = function() return enc({ available = false, active = false }) end
  H.frames      = function() return '[]' end
  H.threads     = function() return '[]' end
  H.scopes      = function() return '[]' end
  H.breakpoints = function() return enc({ loaded = {}, bps = vim.empty_dict() }) end
  H.variables   = unavailable
  H.evaluate    = unavailable
  H.set_stored  = function() end
  H.restore     = function() end
  H.run         = function() return 'nvim-dap not installed' end
  H.run_ft      = H.run
  H.configs     = function() return '[]' end
  return
end

local bpmod = require('dap.breakpoints')

-- Adapter: Go via delve. Spawn dlv in DAP server mode on a free port.
local function add_config(ft, cfg)
  dap.configurations[ft] = dap.configurations[ft] or {}
  for _, c in ipairs(dap.configurations[ft]) do
    if c.name == cfg.name then return end
  end
  table.insert(dap.configurations[ft], cfg)
end

if vim.fn.executable('dlv') == 1 then
  dap.adapters.delve = {
    type = 'server',
    port = '${port}',
    executable = {
      command = 'dlv',
      args = { 'dap', '-l', '127.0.0.1:${port}' },
    },
  }
  add_config('go', { type = 'delve', name = 'Debug current file', request = 'launch', program = '${file}' })
  add_config('go', { type = 'delve', name = 'Debug package', request = 'launch', program = '${fileDirname}' })
end

-- Adapter: Python via debugpy. internalConsole keeps program output in the
-- Debug Console (integratedTerminal would open a stray nvim split).
local py = (vim.fn.executable('python3') == 1) and 'python3' or
           (vim.fn.executable('python')  == 1) and 'python'  or nil
if py then
  local check = vim.fn.system({ py, '-c', 'import debugpy; print("ok")' })
  if vim.v.shell_error == 0 and check:find('ok') then
    dap.adapters.python = {
      type    = 'executable',
      command = py,
      args    = { '-m', 'debugpy.adapter' },
    }
    add_config('python', {
      type = 'python', name = 'Launch current file', request = 'launch',
      program = '${file}', console = 'internalConsole',
    })
  end
end

-- Adapter: Node. Users with vscode-js-debug get the managed pwa-node
-- adapter (dap_install_lua.go); this just stakes out the namespace.
if vim.fn.executable('node') == 1 then
  dap.adapters.node = {
    type    = 'executable',
    command = 'node',
    args    = { '--inspect' },
  }
  for _, ft in ipairs({ 'javascript', 'typescript' }) do
    add_config(ft, {
      type = 'node', name = 'Launch current file', request = 'launch',
      program = '${file}', cwd = '${workspaceFolder}',
    })
  end
end

-- Sign column markers for breakpoints / current execution position. The
-- editor's custom statuscolumn ('%=%l') draws no sign column, so numhl
-- colours the line number itself (red = breakpoint, amber = stopped line).
pcall(vim.fn.sign_define, 'DapBreakpoint',          { text = '●', texthl = 'ErrorMsg', linehl = '', numhl = 'ErrorMsg' })
pcall(vim.fn.sign_define, 'DapBreakpointCondition', { text = '◆', texthl = 'ErrorMsg', linehl = '', numhl = 'ErrorMsg' })
pcall(vim.fn.sign_define, 'DapLogPoint',            { text = '◇', texthl = 'ErrorMsg', linehl = '', numhl = 'ErrorMsg' })
pcall(vim.fn.sign_define, 'DapBreakpointRejected',  { text = '○', texthl = 'Comment',  linehl = '', numhl = '' })
pcall(vim.fn.sign_define, 'DapStopped',             { text = '▶', texthl = 'WarningMsg', linehl = 'CursorLine', numhl = 'WarningMsg' })

-- ── JSON queries ─────────────────────────────────────────────────────────

local MAX_VARS, MAX_FRAMES = 300, 200

local function frame_tbl(f)
  local src = f.source or {}
  return {
    id = f.id, name = trunc(f.name or '?', 200),
    path = src.path or '', source = src.name or '',
    line = f.line or 0, col = f.column or 0,
  }
end

local function var_tbl(v)
  return {
    name = trunc(v.name, 200), value = trunc(v.value, 500),
    type = trunc(v.type or '', 100), ref = v.variablesReference or 0,
  }
end

local function stopped_thread(s)
  if not (s and s.stopped_thread_id and s.threads) then return nil end
  return s.threads[s.stopped_thread_id]
end

local function threads_tbl()
  local s, out = dap.session(), {}
  if s and s.threads then
    for id, t in pairs(s.threads) do
      table.insert(out, { id = id, name = t.name or tostring(id), stopped = t.stopped == true })
    end
    table.sort(out, function(a, b) return a.id < b.id end)
  end
  return out
end

local function frames_tbl()
  local out = {}
  local t = stopped_thread(dap.session())
  if t and t.frames then
    for i, f in ipairs(t.frames) do
      if i > MAX_FRAMES then break end
      table.insert(out, frame_tbl(f))
    end
  end
  return out
end

local function scopes_tbl()
  local s, out = dap.session(), {}
  local f = s and s.current_frame
  if not (f and f.scopes) then return out end
  for _, sc in ipairs(f.scopes) do
    local vars = nil -- nil = not fetched (expensive scope)
    if sc.variables then
      vars = {}
      for i, v in ipairs(sc.variables) do
        if i > MAX_VARS then break end
        table.insert(vars, var_tbl(v))
      end
    end
    table.insert(out, {
      name = sc.name or 'scope', ref = sc.variablesReference or 0,
      expensive = sc.expensive == true, loaded = vars ~= nil, variables = vars,
    })
  end
  return out
end

function H.session_active() return dap.session() ~= nil end
function H.threads() return enc(threads_tbl()) end
function H.frames() return enc(frames_tbl()) end
function H.scopes() return enc(scopes_tbl()) end

function H.state()
  local s = dap.session()
  if not s then return enc({ available = true, active = false }) end
  return enc({
    available = true, active = true,
    stopped = s.stopped_thread_id ~= nil,
    name = (s.config and s.config.name) or '',
    thread = s.stopped_thread_id or 0,
    frame = (s.current_frame and s.current_frame.id) or 0,
    threads = threads_tbl(), frames = frames_tbl(), scopes = scopes_tbl(),
  })
end

local function fmt_err(err)
  local okf, msg = pcall(function() return require('dap.utils').fmt_error(err) end)
  if okf and msg then return tostring(msg) end
  if type(err) == 'table' then return tostring(err.message or 'error') end
  return tostring(err)
end

-- variables(reqid, ref): async children of a variablesReference.
function H.variables(reqid, ref)
  local s = dap.session()
  if not s then
    emit('termocode_dap_result', reqid, enc({ error = 'No active debug session' }))
    return
  end
  s:request('variables', { variablesReference = ref }, function(err, resp)
    if err then
      emit('termocode_dap_result', reqid, enc({ error = fmt_err(err) }))
      return
    end
    local vars = {}
    for i, v in ipairs((resp and resp.variables) or {}) do
      if i > MAX_VARS then break end
      table.insert(vars, var_tbl(v))
    end
    emit('termocode_dap_result', reqid, enc({ variables = vars }))
  end)
end

-- evaluate(reqid, expr, context): async; context is repl | watch | hover.
function H.evaluate(reqid, expr, context)
  local s = dap.session()
  if not s then
    emit('termocode_dap_result', reqid, enc({ error = 'No active debug session' }))
    return
  end
  s:evaluate({ expression = expr, context = context or 'repl' }, function(err, resp)
    if err then
      emit('termocode_dap_result', reqid, enc({ error = fmt_err(err) }))
      return
    end
    emit('termocode_dap_result', reqid, enc({
      result = trunc(resp and resp.result, 4000),
      type = trunc(resp and resp.type or '', 100),
      ref = (resp and resp.variablesReference) or 0,
    }))
  end)
end

-- select_frame(id): make a frame of the stopped thread current (jumps and
-- refreshes its scopes; the scopes/variables listeners notify Go).
function H.select_frame(id)
  local s = dap.session()
  local t = stopped_thread(s)
  if not (t and t.frames) then return end
  for _, f in ipairs(t.frames) do
    if f.id == id then
      s:_frame_set(f)
      return
    end
  end
end

-- ── Starting sessions ────────────────────────────────────────────────────

local aliases = { go = 'delve', debugpy = 'python', node = 'pwa-node', ['pwa-node'] = 'node' }

-- run(json): start a session from a launch.json-style config. Returns ''
-- on success or a human-readable error.
function H.run(json)
  local okd, cfg = pcall(vim.json.decode, json)
  if not okd or type(cfg) ~= 'table' then return 'invalid configuration' end
  local t = cfg.type or ''
  if dap.adapters[t] == nil and aliases[t] and dap.adapters[aliases[t]] then
    cfg.type = aliases[t]
  end
  if dap.adapters[cfg.type] == nil then
    return 'no debug adapter for type "' .. tostring(t) .. '"'
  end
  cfg.name = cfg.name or cfg.type
  cfg.request = cfg.request or 'launch'
  local okr, err = pcall(dap.run, cfg)
  if not okr then return tostring(err) end
  return ''
end

-- configs(ft): nvim-dap's own configurations for a filetype, JSON-safe.
function H.configs(ft)
  local out = {}
  for i, c in ipairs(dap.configurations[ft] or {}) do
    if type(c) == 'table' then
      table.insert(out, { index = i, name = tostring(c.name or ('config ' .. i)), type = tostring(c.type or '') })
    end
  end
  return enc(out)
end

-- run_ft(ft, index): start dap.configurations[ft][index] as-is (keeps any
-- function-valued fields a user config may carry).
function H.run_ft(ft, index)
  local c = (dap.configurations[ft] or {})[index]
  if not c then return 'configuration not found' end
  if dap.adapters[c.type] == nil then
    return 'no debug adapter for type "' .. tostring(c.type) .. '"'
  end
  local okr, err = pcall(dap.run, c)
  if not okr then return tostring(err) end
  return ''
end

-- ── Breakpoints (persisted by Go in breakpoints.json) ────────────────────

local function buf_path(buf)
  if not vim.api.nvim_buf_is_valid(buf) or vim.bo[buf].buftype ~= '' then return nil end
  local name = vim.api.nvim_buf_get_name(buf)
  if name == '' then return nil end
  return vim.fn.fnamemodify(name, ':p')
end

local function push_to_sessions(buf)
  local bps = bpmod.get(buf)
  if bps[buf] == nil then bps[buf] = {} end
  for _, s in pairs(dap.sessions()) do
    pcall(function() s:set_breakpoints(bps) end)
  end
end

-- breakpoints(): every loaded file buffer plus its live breakpoints (sign
-- positions follow edits, so lines are current).
function H.breakpoints()
  local loaded, bps = {}, {}
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(buf) then
      local p = buf_path(buf)
      if p then table.insert(loaded, p) end
    end
  end
  for buf, list in pairs(bpmod.get()) do
    local p = buf_path(buf)
    if p and #list > 0 then
      local arr = {}
      for _, bp in ipairs(list) do
        local text = vim.api.nvim_buf_get_lines(buf, bp.line - 1, bp.line, false)[1] or ''
        local verified = nil
        if bp.state then verified = bp.state.verified == true end
        table.insert(arr, {
          line = bp.line, text = vim.trim(text),
          condition = nonempty(bp.condition), hitCondition = nonempty(bp.hitCondition),
          logMessage = nonempty(bp.logMessage), verified = verified,
        })
      end
      table.sort(arr, function(a, b) return a.line < b.line end)
      bps[p] = arr
    end
  end
  if next(bps) == nil then bps = vim.empty_dict() end
  return enc({ loaded = loaded, bps = bps })
end

-- toggle(cond, log): toggle at the cursor; a non-empty cond / log replaces
-- the breakpoint on the line with a conditional one / a logpoint.
function H.toggle(cond, log)
  cond, log = nonempty(cond), nonempty(log)
  if cond or log then
    dap.set_breakpoint(cond, nil, log)
  else
    dap.toggle_breakpoint()
  end
end

-- remove(path, line): drop one breakpoint of a loaded buffer.
function H.remove(path, line)
  local buf = vim.fn.bufnr(path)
  if buf < 1 or not vim.api.nvim_buf_is_loaded(buf) then return end
  bpmod.remove(buf, line)
  push_to_sessions(buf)
end

function H.clear()
  dap.clear_breakpoints()
end

H._stored = H._stored or {}

-- set_stored(json): the persisted map { [abs path] = { {line, text, …} } }.
function H.set_stored(json)
  local okd, t = pcall(vim.json.decode, json)
  if okd and type(t) == 'table' then H._stored = t end
end

-- restore(buf): place the persisted breakpoints of buf (once — skipped when
-- the buffer already has breakpoints), re-checking every line.
function H.restore(buf)
  local p = buf_path(buf)
  if not p then return 0 end
  local list = H._stored[p]
  if type(list) ~= 'table' or #list == 0 then return 0 end
  local have = bpmod.get(buf)[buf]
  if have and #have > 0 then return 0 end
  local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  local placed, n = {}, 0
  -- Two passes: breakpoints whose line text was found first, so a guessed
  -- position never takes the line of an exact match.
  for pass = 1, 2 do
    for _, bp in ipairs(list) do
      local line = H.relocate(lines, bp.line, bp.text)
      local exact = line ~= nil and vim.trim(lines[line] or '') == vim.trim(bp.text or '')
      if line and not placed[line] and ((pass == 1) == exact) then
        placed[line] = true
        n = n + 1
        bpmod.set({
          condition = nonempty(bp.condition), hit_condition = nonempty(bp.hitCondition),
          log_message = nonempty(bp.logMessage),
        }, buf, line)
      end
    end
  end
  if n > 0 then push_to_sessions(buf) end
  return n
end

-- restore_all(): restore every already-loaded buffer (startup).
function H.restore_all()
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(buf) then pcall(H.restore, buf) end
  end
end

local grp = vim.api.nvim_create_augroup('termocode_dap', { clear = true })
vim.api.nvim_create_autocmd('BufReadPost', {
  group = grp,
  callback = function(a)
    if pcall(H.restore, a.buf) then
      -- moved / dropped lines get written back
      local p = buf_path(a.buf)
      if p and H._stored[p] then emit('termocode_dap', 'breakpoints', '{}') end
    end
  end,
})
vim.api.nvim_create_autocmd('BufWritePost', {
  group = grp,
  callback = function(a)
    local have = bpmod.get(a.buf)[a.buf]
    local p = buf_path(a.buf)
    if (have and #have > 0) or (p and H._stored[p]) then
      emit('termocode_dap', 'breakpoints', '{}')
    end
  end,
})

-- ── Session events → Go ──────────────────────────────────────────────────

local K = 'termocode'
local function ev(kind, payload)
  emit('termocode_dap', kind, enc(payload or vim.empty_dict()))
end

dap.listeners.on_session = dap.listeners.on_session or {}
dap.listeners.on_session[K] = function(_, new)
  ev('session', { active = new ~= nil, name = (new and new.config and new.config.name) or '' })
end
dap.listeners.after.event_initialized[K] = function(s)
  ev('initialized', { name = (s.config and s.config.name) or '' })
end
dap.listeners.after.event_stopped[K] = function(_, body)
  body = body or {}
  ev('stopped', {
    reason = body.reason or '', thread = body.threadId or 0,
    text = body.description or body.text or '',
  })
end
dap.listeners.after.event_continued[K] = function() ev('continued') end
dap.listeners.after.event_terminated[K] = function()
  ev('terminated')
  vim.schedule(function() ev('refresh') end)
end
dap.listeners.after.event_exited[K] = function(_, body)
  ev('exited', { code = (body and body.exitCode) or 0 })
end
dap.listeners.after.event_output[K] = function(_, body)
  if not body or body.category == 'telemetry' then return end
  ev('output', { category = body.category or 'console', output = body.output or '' })
end
for _, cmd in ipairs({ 'continue', 'next', 'stepIn', 'stepOut', 'stepBack', 'reverseContinue' }) do
  dap.listeners.after[cmd][K] = function() ev('continued') end
end
for _, cmd in ipairs({ 'stackTrace', 'scopes', 'variables', 'threads' }) do
  dap.listeners.after[cmd][K] = function() ev('refresh') end
end
dap.listeners.after.setBreakpoints[K] = function() ev('breakpoints') end
dap.listeners.after.disconnect[K] = function()
  vim.schedule(function() ev('refresh') end)
end

-- nvim-dap reports problems through vim.notify(msg, level, {title='DAP'}).
-- With cmdheight=0 those would turn into hit-enter prompts; route them to
-- the Debug Console (and a toast) instead. Other notifications pass through.
if not H._orig_notify then H._orig_notify = vim.notify end
vim.notify = function(msg, level, opts)
  if type(opts) == 'table' and opts.title == 'DAP' then
    ev('message', { level = level or vim.log.levels.INFO, text = tostring(msg) })
    return
  end
  return H._orig_notify(msg, level, opts)
end
`
}
