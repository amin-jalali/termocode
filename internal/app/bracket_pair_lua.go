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
// logged and silently skipped so termocode still starts.
//
// We deliberately set the highlight groups *after* adding the plugin to
// runtimepath so any defaults the plugin ships with are overridden, and
// we cycle our 3-color palette across all 7 RainbowDelimiter* groups so
// deeper nestings keep rotating instead of falling back to plugin colors.
const bracketPairLua = `
local plugin_root = vim.fn.expand('~/.local/share/termocode/plugins')
local plugin_dir  = plugin_root .. '/rainbow-delimiters.nvim'

-- Bootstrap clone (idempotent, silent, non-fatal).
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
      vim.notify(
        'termocode: rainbow-delimiters bootstrap failed (offline?), bracket colorization disabled. ' .. (out or ''),
        vim.log.levels.WARN
      )
      return
    end
  else
    vim.notify(
      'termocode: git not found; bracket colorization disabled.',
      vim.log.levels.WARN
    )
    return
  end
end

-- Add to runtimepath. Prepend so plugin/*.lua loads before any user rtp.
vim.opt.runtimepath:prepend(plugin_dir)

-- Palette: gold, magenta, azure — cycled across the 7 highlight groups
-- the plugin uses, so deeper nestings still rotate through our 3 colors.
local palette = { '#FFD700', '#D75FD7', '#00AFFF' }
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
  for i, group in ipairs(groups) do
    local color = palette[((i - 1) % #palette) + 1]
    vim.api.nvim_set_hl(0, group, { fg = color, default = false })
  end
end
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
  vim.notify(
    'termocode: rainbow-delimiters require failed: ' .. tostring(err),
    vim.log.levels.WARN
  )
end
`
