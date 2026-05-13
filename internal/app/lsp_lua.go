package app

// lspSetupLua configures nvim's built-in LSP for common languages, attaches
// per-buffer keymaps on LspAttach, and wires up auto-trigger completion via
// omnifunc so termocode behaves VSCode-like.
//
// The Lua chunk is idempotent and safe to re-run.
const lspSetupLua = `
-- Server registry: name → { cmd, filetypes, root_markers }
local servers = {
  gopls = {
    cmd = { 'gopls' },
    filetypes = { 'go', 'gomod', 'gowork', 'gotmpl' },
    root = { 'go.mod', 'go.work', '.git' },
  },
  pyright = {
    cmd = { 'pyright-langserver', '--stdio' },
    filetypes = { 'python' },
    root = { 'pyproject.toml', 'setup.py', 'setup.cfg', 'requirements.txt', 'Pipfile', '.git' },
  },
  pylsp = {
    cmd = { 'pylsp' },
    filetypes = { 'python' },
    root = { 'pyproject.toml', 'setup.py', '.git' },
  },
  ts_ls = {
    cmd = { 'typescript-language-server', '--stdio' },
    filetypes = { 'javascript', 'javascriptreact', 'typescript', 'typescriptreact' },
    root = { 'package.json', 'tsconfig.json', 'jsconfig.json', '.git' },
  },
  rust_analyzer = {
    cmd = { 'rust-analyzer' },
    filetypes = { 'rust' },
    root = { 'Cargo.toml', '.git' },
  },
  clangd = {
    cmd = { 'clangd' },
    filetypes = { 'c', 'cpp', 'objc', 'objcpp' },
    root = { 'compile_commands.json', 'compile_flags.txt', '.clangd', '.git' },
  },
  lua_ls = {
    cmd = { 'lua-language-server' },
    filetypes = { 'lua' },
    root = { '.luarc.json', '.luarc.jsonc', '.luacheckrc', '.git' },
  },
}

-- Find the first ancestor of start_dir that contains any of the marker files/dirs.
local function find_root(start_dir, markers)
  if start_dir == nil or start_dir == '' then start_dir = vim.fn.getcwd() end
  local current = start_dir
  while current and current ~= '/' do
    for _, marker in ipairs(markers) do
      local path = current .. '/' .. marker
      if vim.fn.isdirectory(path) == 1 or vim.fn.filereadable(path) == 1 then
        return current
      end
    end
    local parent = vim.fn.fnamemodify(current, ':h')
    if parent == current then break end
    current = parent
  end
  return start_dir
end

local function ft_to_servers(ft)
  local out = {}
  for name, cfg in pairs(servers) do
    for _, x in ipairs(cfg.filetypes) do
      if x == ft then table.insert(out, { name = name, cfg = cfg }); break end
    end
  end
  return out
end

vim.api.nvim_create_augroup('TermocodeLSP', { clear = true })

-- Inlay hints: muted, italic foreground so virtual text fades into the
-- background. ` + "`TextDimmer`" + ` (#585858) is the same color the indent guides use.
pcall(vim.api.nvim_set_hl, 0, 'LspInlayHint', { fg = '#585858', italic = true })

vim.api.nvim_create_autocmd('FileType', {
  group = 'TermocodeLSP',
  callback = function(args)
    local ft = args.match
    -- Enable Tree-sitter highlighting if a parser is bundled / installed.
    pcall(vim.treesitter.start, args.buf, ft)
    for _, srv in ipairs(ft_to_servers(ft)) do
      if vim.fn.executable(srv.cfg.cmd[1]) ~= 1 then goto continue end
      local start_dir = vim.fn.expand('%:p:h')
      local root = find_root(start_dir, srv.cfg.root)
      vim.lsp.start({
        name = srv.name,
        cmd = srv.cfg.cmd,
        root_dir = root,
        capabilities = vim.lsp.protocol.make_client_capabilities(),
      })
      ::continue::
    end
  end,
})

vim.api.nvim_create_autocmd('LspAttach', {
  group = 'TermocodeLSP',
  callback = function(args)
    local buf = args.buf
    local opts = { buffer = buf, silent = true, noremap = true }

    vim.keymap.set('n', 'K',         vim.lsp.buf.hover,           opts)
    vim.keymap.set('n', 'gd',        vim.lsp.buf.definition,      opts)
    vim.keymap.set('n', 'gD',        vim.lsp.buf.declaration,     opts)
    vim.keymap.set('n', 'gi',        vim.lsp.buf.implementation,  opts)
    vim.keymap.set('n', 'gr',        vim.lsp.buf.references,      opts)
    vim.keymap.set('n', 'gt',        vim.lsp.buf.type_definition, opts)
    vim.keymap.set('n', '<F2>',      vim.lsp.buf.rename,          opts)
    vim.keymap.set('n', '<F12>',     vim.lsp.buf.definition,      opts)
    vim.keymap.set('n', '<S-F12>',   vim.lsp.buf.references,      opts)
    vim.keymap.set('n', '<C-Space>', function() vim.lsp.buf.signature_help() end, opts)
    vim.keymap.set('n', '<leader>f', function()
      vim.lsp.buf.format({ async = true })
    end, opts)

    -- Use LSP omnifunc for completion in this buffer.
    vim.bo[buf].omnifunc = 'v:lua.vim.lsp.omnifunc'

    -- Inlay hints (parameter names, inferred types, etc.) — best-effort.
    -- Neovim 0.10+ has ` + "`vim.lsp.inlay_hint.enable(true)`" + `; 0.9 had a buf-scoped
    -- ` + "`vim.lsp.buf.inlay_hint(buf, true)`" + `; older versions get nothing. Wrapped in
    -- pcall so a missing API on any version can't break LSP attach.
    local client = vim.lsp.get_client_by_id(args.data and args.data.client_id)
    if client and client.server_capabilities and client.server_capabilities.inlayHintProvider then
      if vim.fn.has('nvim-0.10') == 1 and vim.lsp.inlay_hint and vim.lsp.inlay_hint.enable then
        pcall(vim.lsp.inlay_hint.enable, true, { bufnr = buf })
      elseif vim.lsp.buf and vim.lsp.buf.inlay_hint then
        pcall(vim.lsp.buf.inlay_hint, buf, true)
      end
    end

    -- Folding: prefer the LSP-driven foldexpr (nvim 0.10+ — uses the server's
    -- folding ranges), fall back to Tree-sitter's foldexpr when a parser is
    -- loaded for this buffer, otherwise leave folds in 'manual' mode (no-op
    -- but harmless). Wrapped in pcall so a missing API on older nvim never
    -- breaks LspAttach.
    local fold_set = false
    if vim.lsp and vim.lsp.foldexpr then
      local ok = pcall(function()
        vim.api.nvim_set_option_value('foldmethod', 'expr', { win = 0 })
        vim.api.nvim_set_option_value('foldexpr', 'v:lua.vim.lsp.foldexpr()', { win = 0 })
      end)
      if ok then fold_set = true end
    end
    if not fold_set then
      local has_parser = false
      pcall(function()
        if vim.treesitter and vim.treesitter.language and vim.treesitter.language.get_lang then
          has_parser = vim.treesitter.language.get_lang(vim.bo[buf].filetype) ~= nil
        end
      end)
      if has_parser and vim.treesitter and vim.treesitter.foldexpr then
        pcall(function()
          vim.api.nvim_set_option_value('foldmethod', 'expr', { win = 0 })
          vim.api.nvim_set_option_value('foldexpr', 'v:lua.vim.treesitter.foldexpr()', { win = 0 })
        end)
      else
        pcall(function()
          vim.api.nvim_set_option_value('foldmethod', 'manual', { win = 0 })
        end)
      end
    end
  end,
})

-- Format on save: run LSP formatter, but quietly skip if no LSP / no formatter.
vim.api.nvim_create_autocmd('BufWritePre', {
  group = 'TermocodeLSP',
  callback = function()
    pcall(vim.lsp.buf.format, { async = false, timeout_ms = 2000 })
  end,
})

-- Diagnostic visuals.
vim.diagnostic.config({
  virtual_text = { prefix = '●', spacing = 2 },
  signs = true,
  underline = true,
  update_in_insert = false,
  severity_sort = true,
  float = { border = 'rounded' },
})

-- Sign column glyphs (use Nerd Font symbols when present, else letters).
local signs = { Error = '', Warn = '', Info = '', Hint = '' }
for type, icon in pairs(signs) do
  local hl = 'DiagnosticSign' .. type
  vim.fn.sign_define(hl, { text = icon, texthl = hl, numhl = '' })
end

-- Auto-trigger completion when typing word chars or trigger characters.
-- Skips when popup is already visible to avoid feedback loops.
vim.opt.completeopt = { 'menu', 'menuone', 'noselect' }
vim.opt.shortmess:append('c')

local _termocode_last_complete = 0
vim.api.nvim_create_autocmd('TextChangedI', {
  group = 'TermocodeLSP',
  callback = function()
    if vim.fn.pumvisible() == 1 then return end
    -- Only auto-trigger when an LSP-backed omnifunc is active for this buffer.
    if vim.bo.omnifunc == '' then return end
    -- 80 ms debounce so fast typing doesn't queue many requests.
    local now = vim.uv and vim.uv.now() or vim.loop.now()
    if now - _termocode_last_complete < 80 then return end
    _termocode_last_complete = now

    local line = vim.api.nvim_get_current_line()
    local col = vim.api.nvim_win_get_cursor(0)[2]
    if col == 0 then return end
    local prev = line:sub(col, col)
    if prev:match('[%w_]') or prev == '.' or prev == ':' then
      vim.schedule(function()
        if vim.fn.mode() == 'i' and vim.fn.pumvisible() == 0 and vim.bo.omnifunc ~= '' then
          local keys = vim.api.nvim_replace_termcodes('<C-x><C-o>', true, false, true)
          vim.api.nvim_feedkeys(keys, 'n', false)
        end
      end)
    end
  end,
})

-- Ctrl+Space: manual completion. Uses LSP omnifunc when available, falls back
-- to keyword completion (words from the current buffer) for unsupported files.
vim.keymap.set('i', '<C-Space>', function()
  if vim.bo.omnifunc ~= '' then
    return '<C-x><C-o>'
  end
  return '<C-x><C-n>'
end, { expr = true, silent = true })

-- In insert mode: Tab and Enter accept the popup if visible; otherwise behave normally.
vim.keymap.set('i', '<Tab>', function()
  if vim.fn.pumvisible() == 1 then
    return '<C-y>'
  end
  return '<Tab>'
end, { expr = true, silent = true })

vim.keymap.set('i', '<S-Tab>', function()
  if vim.fn.pumvisible() == 1 then
    return '<C-p>'
  end
  return '<S-Tab>'
end, { expr = true, silent = true })

vim.keymap.set('i', '<CR>', function()
  if vim.fn.pumvisible() == 1 then
    return '<C-y>'
  end
  return '<CR>'
end, { expr = true, silent = true })

vim.keymap.set('i', '<Down>', function()
  if vim.fn.pumvisible() == 1 then return '<C-n>' end
  return '<Down>'
end, { expr = true, silent = true })

vim.keymap.set('i', '<Up>', function()
  if vim.fn.pumvisible() == 1 then return '<C-p>' end
  return '<Up>'
end, { expr = true, silent = true })

-- Auto-pair brackets and quotes.
local _pair_open = { ['('] = ')', ['['] = ']', ['{'] = '}' }
local _pair_quote = { ['"'] = true, ["'"] = true, ['` + "`" + `'] = true }

local function _next_char()
  local line = vim.api.nvim_get_current_line()
  local col = vim.api.nvim_win_get_cursor(0)[2]
  if col >= #line then return '' end
  return line:sub(col + 1, col + 1)
end

local function _prev_char()
  local line = vim.api.nvim_get_current_line()
  local col = vim.api.nvim_win_get_cursor(0)[2]
  if col == 0 then return '' end
  return line:sub(col, col)
end

for open, close in pairs(_pair_open) do
  vim.keymap.set('i', open, function()
    return open .. close .. '<Left>'
  end, { expr = true, silent = true })

  vim.keymap.set('i', close, function()
    if _next_char() == close then
      return '<Right>'
    end
    return close
  end, { expr = true, silent = true })
end

for quote, _ in pairs(_pair_quote) do
  vim.keymap.set('i', quote, function()
    if _next_char() == quote then
      return '<Right>'
    end
    -- Don't pair after a word char (avoids closing on contractions like don't).
    local prev = _prev_char()
    if prev:match('[%w]') then
      return quote
    end
    return quote .. quote .. '<Left>'
  end, { expr = true, silent = true })
end

-- Toggle comment (Ctrl+/ on most terminals sends 0x1F == Ctrl+_).
-- Globals (_G.*) so Go-side ExecLua can call them by name. Without
-- the _G assignment the function is local to this Lua chunk and
-- vanishes after the chunk returns.
function _G._toggle_comment_line()
  local cs = vim.bo.commentstring
  if cs == nil or cs == '' then cs = '# %s' end
  local prefix = cs:match('^(.-)%%s')
  if not prefix then return end
  prefix = vim.trim(prefix)
  if prefix == '' then return end

  local line = vim.api.nvim_get_current_line()
  local stripped = line:match('^%s*(.*)$') or ''
  local indent = line:sub(1, #line - #stripped)

  if stripped:sub(1, #prefix) == prefix then
    local rest = stripped:sub(#prefix + 1)
    if rest:sub(1, 1) == ' ' then rest = rest:sub(2) end
    vim.api.nvim_set_current_line(indent .. rest)
  else
    if stripped == '' then
      vim.api.nvim_set_current_line(indent .. prefix .. ' ')
    else
      vim.api.nvim_set_current_line(indent .. prefix .. ' ' .. stripped)
    end
  end
end

vim.keymap.set('n', '<C-_>', _G._toggle_comment_line, { silent = true })
vim.keymap.set('i', '<C-_>', function()
  _G._toggle_comment_line()
end, { silent = true })

function _G._toggle_comment_visual()
  local cs = vim.bo.commentstring
  if cs == nil or cs == '' then cs = '# %s' end
  local prefix = cs:match('^(.-)%%s')
  if not prefix then return end
  prefix = vim.trim(prefix)
  if prefix == '' then return end

  local s_start = vim.fn.line("'<")
  local s_end = vim.fn.line("'>")
  if s_start == 0 or s_end == 0 then
    s_start = vim.fn.line('.')
    s_end = s_start
  end
  if s_start > s_end then s_start, s_end = s_end, s_start end

  -- decide whether to comment or uncomment based on first non-empty line
  local should_comment = false
  for i = s_start, s_end do
    local l = vim.fn.getline(i)
    local stripped = l:match('^%s*(.*)$') or ''
    if stripped ~= '' then
      if stripped:sub(1, #prefix) ~= prefix then should_comment = true end
      break
    end
  end

  for i = s_start, s_end do
    local l = vim.fn.getline(i)
    local stripped = l:match('^%s*(.*)$') or ''
    local indent = l:sub(1, #l - #stripped)
    if should_comment then
      if stripped == '' then
        vim.fn.setline(i, indent .. prefix .. ' ')
      else
        vim.fn.setline(i, indent .. prefix .. ' ' .. stripped)
      end
    else
      if stripped:sub(1, #prefix) == prefix then
        local rest = stripped:sub(#prefix + 1)
        if rest:sub(1, 1) == ' ' then rest = rest:sub(2) end
        vim.fn.setline(i, indent .. rest)
      end
    end
  end
end

vim.keymap.set('v', '<C-_>', _G._toggle_comment_visual, { silent = true })

-- Move line (Alt+Up / Alt+Down).
vim.keymap.set('n', '<M-Down>', ':m .+1<CR>==', { silent = true })
vim.keymap.set('n', '<M-Up>',   ':m .-2<CR>==', { silent = true })
vim.keymap.set('i', '<M-Down>', '<Esc>:m .+1<CR>==gi', { silent = true })
vim.keymap.set('i', '<M-Up>',   '<Esc>:m .-2<CR>==gi', { silent = true })
vim.keymap.set('v', '<M-Down>', ":m '>+1<CR>gv=gv", { silent = true })
vim.keymap.set('v', '<M-Up>',   ":m '<-2<CR>gv=gv", { silent = true })

-- Duplicate line (Shift+Alt+Up / Shift+Alt+Down).
vim.keymap.set('n', '<M-S-Down>', 'yyp', { silent = true })
vim.keymap.set('n', '<M-S-Up>',   'yyP', { silent = true })
vim.keymap.set('i', '<M-S-Down>', '<C-o>yy<C-o>p', { silent = true })
vim.keymap.set('i', '<M-S-Up>',   '<C-o>yy<C-o>P', { silent = true })
vim.keymap.set('v', '<M-S-Down>', ":'<,'>t '><CR>gv=gv", { silent = true })
vim.keymap.set('v', '<M-S-Up>',   ":'<,'>t '<-1<CR>gv=gv", { silent = true })

-- ─────────────────────────────────────────────────────────────────────────
-- Indent guides: render a faint │ at each indent level via extmarks.
-- ─────────────────────────────────────────────────────────────────────────
local _termocode_indent_ns = vim.api.nvim_create_namespace('termocode_indent')

-- Find the byte index in line whose visual column equals target.
-- Walks leading whitespace only; returns -1 if target is past the indent.
local function _byte_at_visual(line, target, sw)
  local visual = 0
  for c = 1, #line do
    if visual >= target then return c - 1 end
    local ch = line:sub(c, c)
    if ch == '\t' then
      visual = math.floor(visual / sw) * sw + sw
    elseif ch == ' ' then
      visual = visual + 1
    else
      return -1
    end
  end
  return -1
end

local function _indent_visual(line, sw)
  local visual = 0
  for c = 1, #line do
    local ch = line:sub(c, c)
    if ch == '\t' then
      visual = math.floor(visual / sw) * sw + sw
    elseif ch == ' ' then
      visual = visual + 1
    else
      return visual
    end
  end
  return visual
end

local function _render_indent_guides(buf)
  if not vim.api.nvim_buf_is_loaded(buf) then return end
  vim.api.nvim_buf_clear_namespace(buf, _termocode_indent_ns, 0, -1)

  local total = vim.api.nvim_buf_line_count(buf)
  if total > 5000 then return end

  local sw = vim.bo[buf].shiftwidth
  if sw == 0 then sw = vim.bo[buf].tabstop end
  if sw == 0 then sw = 4 end

  local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  for i, line in ipairs(lines) do
    if line ~= '' then
      local visual = _indent_visual(line, sw)
      for stop = sw, visual - 1, sw do
        local byte_col = _byte_at_visual(line, stop, sw)
        if byte_col >= 0 then
          pcall(vim.api.nvim_buf_set_extmark, buf, _termocode_indent_ns, i - 1, byte_col, {
            virt_text = { { '│', 'TermocodeIndentGuide' } },
            virt_text_pos = 'overlay',
            hl_mode = 'combine',
            priority = 1,
          })
        end
      end
    end
  end
end

local _termocode_indent_pending = {}
local function _schedule_indent_guides(buf)
  if _termocode_indent_pending[buf] then return end
  _termocode_indent_pending[buf] = true
  vim.defer_fn(function()
    _termocode_indent_pending[buf] = nil
    if vim.api.nvim_buf_is_loaded(buf) then
      _render_indent_guides(buf)
    end
  end, 80)
end

vim.api.nvim_create_autocmd({
  'BufEnter', 'BufWinEnter', 'BufReadPost', 'BufWritePost',
  'TextChanged', 'TextChangedI', 'WinEnter',
}, {
  group = 'TermocodeLSP',
  callback = function(args)
    _schedule_indent_guides(args.buf)
  end,
})

-- Run once at startup for the initial buffer.
vim.schedule(function()
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) then
      _render_indent_guides(b)
    end
  end
end)

-- Backspace inside an empty pair removes both halves.
vim.keymap.set('i', '<BS>', function()
  local prev = _prev_char()
  local next_ch = _next_char()
  if (prev == '(' and next_ch == ')')
    or (prev == '[' and next_ch == ']')
    or (prev == '{' and next_ch == '}')
    or (prev == '"' and next_ch == '"')
    or (prev == "'" and next_ch == "'")
    or (prev == '` + "`" + `' and next_ch == '` + "`" + `') then
    return '<BS><Del>'
  end
  return '<BS>'
end, { expr = true, silent = true })
`
