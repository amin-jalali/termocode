package app

// dapManagedLua registers debug adapters installed by the managed installer
// (internal/lspinstall → tools/bin shims on nvim's PATH). It runs once after
// dapSetupLua at startup and again after every "DAP: Install Adapter…"
// success, so a fresh install works without a restart.
//
// Idempotent: adapters are (re)assigned, configurations are only added
// when no entry with the same name exists. A missing nvim-dap is a no-op.
const dapManagedLua = `
function _G._termocode_dap_register_managed()
  local ok, dap = pcall(require, 'dap')
  if not ok then return end

  local function add_config(ft, cfg)
    dap.configurations[ft] = dap.configurations[ft] or {}
    for _, c in ipairs(dap.configurations[ft]) do
      if c.name == cfg.name then return end
    end
    table.insert(dap.configurations[ft], cfg)
  end

  -- Go: delve (managed install lands on PATH via tools/bin).
  if dap.adapters.delve == nil and vim.fn.executable('dlv') == 1 then
    dap.adapters.delve = {
      type = 'server',
      port = '${port}',
      executable = { command = 'dlv', args = { 'dap', '-l', '127.0.0.1:${port}' } },
    }
    add_config('go', { type = 'delve', name = 'Debug current file', request = 'launch', program = '${file}' })
    add_config('go', { type = 'delve', name = 'Debug package', request = 'launch', program = '${fileDirname}' })
  end

  -- Python: managed debugpy venv, exposed as the debugpy-adapter shim.
  if vim.fn.executable('debugpy-adapter') == 1 then
    dap.adapters.python = { type = 'executable', command = 'debugpy-adapter' }
    add_config('python', {
      type = 'python', name = 'Launch current file', request = 'launch',
      program = '${file}', console = 'integratedTerminal',
    })
  end

  -- JS/TS: vscode-js-debug's DAP server (js-debug-adapter <port>).
  if vim.fn.executable('js-debug-adapter') == 1 then
    dap.adapters['pwa-node'] = {
      type = 'server',
      host = '127.0.0.1',
      port = '${port}',
      executable = { command = 'js-debug-adapter', args = { '${port}', '127.0.0.1' } },
    }
    for _, ft in ipairs({ 'javascript', 'typescript', 'javascriptreact', 'typescriptreact' }) do
      add_config(ft, {
        type = 'pwa-node', name = 'Launch current file (js-debug)', request = 'launch',
        program = '${file}', cwd = '${workspaceFolder}',
      })
    end
  end
end

_G._termocode_dap_register_managed()
`
