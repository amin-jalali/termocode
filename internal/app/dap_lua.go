package app

// dapSetupLua returns the Lua chunk that:
//  1. Prepends mfussenegger/nvim-dap to runtimepath so `require('dap')` resolves.
//  2. Auto-detects available debug adapters (delve / debugpy / node --inspect)
//     and registers them, plus a baseline configuration table per language.
//  3. Defines a small `_termocode_dap` helper module exposing functions Go
//     can call via EvalLuaString — `session_active()`, `format_stack()`,
//     `format_vars()`.
//
// Adapters that aren't installed are silently skipped: we just don't register
// them. Users get a sensible toast from the Go side ("install dlv …") when they
// try to start a session for an unsupported language.
//
// The chunk is idempotent: re-running it (e.g. on :luafile reload) just
// re-registers the same tables.
func dapSetupLua(pluginPath string) string {
	escaped := luaEscape(pluginPath)
	return `
local plugin_path = '` + escaped + `'
if plugin_path ~= '' and vim.fn.isdirectory(plugin_path) == 1 then
  vim.opt.rtp:prepend(plugin_path)
end

local ok, dap = pcall(require, 'dap')
if not ok then
  -- nvim-dap isn't on rtp (clone failed). Stub the helpers so Go-side
  -- EvalLuaString calls don't blow up; they'll just report "no session".
  _G._termocode_dap = {
    session_active = function() return false end,
    format_stack   = function() return 'DAP unavailable: nvim-dap not installed.' end,
    format_vars    = function() return 'DAP unavailable: nvim-dap not installed.' end,
  }
  return
end

-- Adapter: Go via delve. Spawn dlv in DAP server mode on a free port.
if vim.fn.executable('dlv') == 1 then
  dap.adapters.delve = {
    type = 'server',
    port = '${port}',
    executable = {
      command = 'dlv',
      args = { 'dap', '-l', '127.0.0.1:${port}' },
    },
  }
  dap.configurations.go = dap.configurations.go or {}
  table.insert(dap.configurations.go, {
    type    = 'delve',
    name    = 'Debug current file',
    request = 'launch',
    program = '${file}',
  })
  table.insert(dap.configurations.go, {
    type    = 'delve',
    name    = 'Debug package',
    request = 'launch',
    program = '${fileDirname}',
  })
end

-- Adapter: Python via debugpy.
local py = (vim.fn.executable('python3') == 1) and 'python3' or
           (vim.fn.executable('python')  == 1) and 'python'  or nil
if py then
  -- Best-effort feature check: only register if debugpy is importable.
  local check = vim.fn.system({ py, '-c', 'import debugpy; print("ok")' })
  if vim.v.shell_error == 0 and check:find('ok') then
    dap.adapters.python = {
      type    = 'executable',
      command = py,
      args    = { '-m', 'debugpy.adapter' },
    }
    dap.configurations.python = dap.configurations.python or {}
    table.insert(dap.configurations.python, {
      type    = 'python',
      name    = 'Launch current file',
      request = 'launch',
      program = '${file}',
      console = 'integratedTerminal',
    })
  end
end

-- Adapter: Node via node --inspect. Generic; users with vscode-js-debug can
-- override this; we just stake out the namespace.
if vim.fn.executable('node') == 1 then
  dap.adapters.node = {
    type    = 'executable',
    command = 'node',
    args    = { '--inspect' },
  }
  dap.configurations.javascript = dap.configurations.javascript or {}
  table.insert(dap.configurations.javascript, {
    type    = 'node',
    name    = 'Launch current file',
    request = 'launch',
    program = '${file}',
    cwd     = '${workspaceFolder}',
  })
  dap.configurations.typescript = dap.configurations.typescript or {}
  table.insert(dap.configurations.typescript, {
    type    = 'node',
    name    = 'Launch current file',
    request = 'launch',
    program = '${file}',
    cwd     = '${workspaceFolder}',
  })
end

-- Sign column markers for breakpoints / current execution position. Keep them
-- on the default highlight groups so the user's colorscheme tints them
-- naturally.
pcall(vim.fn.sign_define, 'DapBreakpoint',         { text = '●', texthl = 'ErrorMsg', linehl = '', numhl = '' })
pcall(vim.fn.sign_define, 'DapBreakpointCondition',{ text = '◆', texthl = 'ErrorMsg', linehl = '', numhl = '' })
pcall(vim.fn.sign_define, 'DapStopped',            { text = '▶', texthl = 'WarningMsg', linehl = 'CursorLine', numhl = '' })

-- Helper module the Go side queries through EvalLuaString.
_G._termocode_dap = {}

function _G._termocode_dap.session_active()
  local s = dap.session()
  return s ~= nil
end

function _G._termocode_dap.format_stack()
  local s = dap.session()
  if not s then return 'No active debug session.' end
  local frame = s.current_frame
  local thread = s.stopped_thread_id and s.threads[s.stopped_thread_id]
  local frames = (thread and thread.frames) or (frame and { frame }) or {}
  if #frames == 0 then return 'No call stack available (program is running).' end
  local lines = { 'Call stack:' }
  for i, f in ipairs(frames) do
    local name   = f.name or '?'
    local source = (f.source and (f.source.path or f.source.name)) or '?'
    local line   = f.line or 0
    table.insert(lines, string.format('  %2d  %s   %s:%d', i, name, source, line))
  end
  return table.concat(lines, '\n')
end

function _G._termocode_dap.format_vars()
  local s = dap.session()
  if not s then return 'No active debug session.' end
  local frame = s.current_frame
  if not frame or not frame.scopes then
    return 'No variables in scope (program may still be running).'
  end
  local lines = { 'Variables:' }
  for _, scope in ipairs(frame.scopes) do
    table.insert(lines, '── ' .. (scope.name or 'scope'))
    local vars = scope.variables
    if vars then
      for _, v in pairs(vars) do
        local val = tostring(v.value or '')
        if #val > 80 then val = val:sub(1, 79) .. '…' end
        table.insert(lines, string.format('  %s = %s', v.name or '?', val))
      end
    end
  end
  return table.concat(lines, '\n')
end
`
}
