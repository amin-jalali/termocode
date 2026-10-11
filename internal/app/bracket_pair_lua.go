package app

// bracketPairLua bootstraps and configures HiPhish/rainbow-delimiters.nvim,
// a Tree-sitter based bracket pair colorizer. It clones the plugin to
// ~/.local/share/termocode/plugins/rainbow-delimiters.nvim on first run,
// adds it to nvim's runtimepath, then overrides its highlight groups so
// `()`, `[]`, and `{}` cycle through the termocode BracketPairs palette
// (gold → magenta → azure) at each nesting level.
//
// The chunk is idempotent: subsequent runs short-circuit when the plugin
// is already on the filesystem. A clone failure (e.g. no network) is
// recorded in vim.g.termocode_bracket_pair_error and skipped so termocode
// still starts. It never calls vim.notify: with cmdheight=0 a warning
// becomes a hit-enter prompt that blocks every RPC and key, so a failed
// clone (or a half-cloned plugin dir) froze the whole editor.
//
// We deliberately set the highlight groups *after* adding the plugin to
// runtimepath so any defaults the plugin ships with are overridden, and
// we cycle our 3-color palette across all 7 RainbowDelimiter* groups so
// deeper nestings keep rotating instead of falling back to plugin colors.
const bracketPairLua = `
local plugin_root = vim.fn.expand('~/.local/share/termocode/plugins')
local plugin_dir  = plugin_root .. '/rainbow-delimiters.nvim'

-- Never vim.notify here (see the Go doc comment): record and move on.
local function disable(why)
  vim.g.termocode_bracket_pair_error = why
end

-- Bootstrap clone (idempotent, silent, non-fatal). A dir without the Lua
-- module is a half-finished clone (interrupted first start): start over.
local module_dir = plugin_dir .. '/lua/rainbow-delimiters'
if vim.fn.isdirectory(plugin_dir) == 1 and vim.fn.isdirectory(module_dir) == 0 then
  vim.fn.delete(plugin_dir, 'rf')
end
if vim.fn.isdirectory(plugin_dir) == 0 then
  vim.fn.mkdir(plugin_root, 'p')
  if vim.fn.executable('git') == 1 then
    local out = vim.fn.system({
      'git', 'clone', '--depth=1',
      '--branch', 'v0.10.0',
      'https://github.com/HiPhish/rainbow-delimiters.nvim.git',
      plugin_dir,
    })
    if vim.v.shell_error ~= 0 then
      vim.fn.delete(plugin_dir, 'rf')
      return disable('rainbow-delimiters bootstrap failed (offline?): ' .. (out or ''))
    end
  else
    return disable('git not found')
  end
end

-- Add to runtimepath. Prepend so plugin/*.lua loads before any user rtp.
vim.opt.runtimepath:prepend(plugin_dir)

-- Palette: gold, magenta, azure — cycled across the 7 highlight groups
-- the plugin uses, so deeper nestings still rotate through our 3 colors.
-- Colours come from the active theme (_G.termocode_colors, theme_lua.go).
local function palette()
  local C = _G.termocode_colors or {}
  return { C.br1, C.br2, C.br3 }
end
local groups = {
  'RainbowDelimiterRed',
  'RainbowDelimiterYellow',
  'RainbowDelimiterBlue',
  'RainbowDelimiterOrange',
  'RainbowDelimiterGreen',
  'RainbowDelimiterCyan',
  'RainbowDelimiterViolet',
}
local function apply_highlights()
  local pal = palette()
  for i, group in ipairs(groups) do
    local color = pal[((i - 1) % #pal) + 1]
    if color then vim.api.nvim_set_hl(0, group, { fg = color, default = false }) end
  end
end
_G.termocode_bracket_hl = apply_highlights
apply_highlights()
-- Re-apply on ColorScheme so a future :colorscheme can't wipe us out.
vim.api.nvim_create_autocmd('ColorScheme', {
  group = vim.api.nvim_create_augroup('TermocodeBracketPair', { clear = true }),
  callback = apply_highlights,
})

-- Configure the plugin: global strategy, default query. Setting vim.g
-- before requiring the module is the documented configuration path.
vim.g.rainbow_delimiters = {
  strategy = {
    [''] = 'rainbow-delimiters.strategy.global',
  },
  query = {
    [''] = 'rainbow-delimiters',
  },
  highlight = groups,
}

-- Loading the module wires the plugin into Tree-sitter; any FileType that
-- already started a parser (via TermocodeLSP's vim.treesitter.start) will
-- pick it up on the next BufEnter / TSEnable.
local ok, err = pcall(require, 'rainbow-delimiters.setup')
if not ok then
  disable('rainbow-delimiters require failed: ' .. tostring(err))
end
`
