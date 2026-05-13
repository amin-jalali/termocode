package app

// vscodeDarkPlusLua defines a VSCode Dark+ inspired colorscheme by setting
// nvim highlight groups directly. Reapplied after `syntax on` so it
// overrides nvim's default scheme.
const vscodeDarkPlusLua = `
if vim.g.syntax_on then vim.cmd('syntax reset') end
vim.cmd('hi clear')
vim.opt.background = 'dark'
vim.g.colors_name = 'termocode'

-- VSCode "Dark+" canonical palette.
local c = {
  -- chrome
  bg          = '#1c1c1c', -- editor background
  bg_panel    = '#262626', -- sidebar background
  bg_activity = '#303030', -- activity bar background
  bg_title    = '#3a3a3a', -- title bar / borders
  bg_tab      = '#2d2d2d', -- inactive tab background
  bg_active   = '#2a2d2e', -- current line highlight
  bg_select   = '#005f87', -- selection
  bg_match    = '#515c6a', -- find match
  bg_hl       = '#ea5c00', -- find highlight (we approximate transparency)
  bg_input    = '#3a3a3a', -- input background
  bg_list_act = '#094771', -- active list item
  bg_list_in  = '#37373d', -- inactive list highlight
  bg_btn      = '#0e639c', -- button
  bg_btn_h    = '#1177bb', -- button hover

  -- text
  fg          = '#d0d0d0',
  fg_muted    = '#858585',
  fg_dim      = '#3a3a3a',
  fg_active_n = '#c6c6c6', -- active line number

  -- accent
  accent      = '#0087d7',
  border      = '#3a3a3a',
  focus       = '#007fd4',

  -- editor extras
  cursor      = '#aeafad',
  indent      = '#363636', -- dimmer for a thinner-looking guide
  indent_a    = '#5a5a5a',
  whitespace  = '#3b3a32',

  -- syntax
  comment     = '#6a9955',
  keyword     = '#569cd6',
  control     = '#c586c0',
  string      = '#ce9178',
  regex       = '#d16969',
  number      = '#b5cea8',
  ['function']= '#dcdcaa',
  type        = '#4ec9b0',
  constant    = '#4fc1ff',
  variable    = '#9cdcfe',
  operator    = '#d0d0d0',
  special     = '#d7ba7d',
  preproc     = '#bd63c5',

  -- diagnostics
  error_      = '#f14c4c',
  warn        = '#cca700',
  info        = '#75beff',
  hint        = '#3794ff',

  -- git
  git_mod     = '#e2c08d',
  git_unt     = '#73c991',
  git_del     = '#c74e39',
  git_conf    = '#6c6cc4',
  git_ign     = '#8c8c8c',
  git_add     = '#587c0c',

  -- bracket rainbow
  br1         = '#ffd700',
  br2         = '#da70d6',
  br3         = '#179fff',
}

local hi = function(group, opts) vim.api.nvim_set_hl(0, group, opts) end

-- ─── core UI ───────────────────────────────────────────────────────────
hi('Normal',          { fg = c.fg, bg = c.bg })
hi('NormalNC',        { fg = c.fg, bg = c.bg })
hi('NormalFloat',     { fg = c.fg, bg = c.bg_panel })
hi('FloatBorder',     { fg = c.border, bg = c.bg_panel })
hi('EndOfBuffer',     { fg = c.bg, bg = c.bg })
hi('NonText',         { fg = c.fg_dim })
hi('SpecialKey',      { fg = c.whitespace })
hi('Whitespace',      { fg = c.whitespace })
hi('Conceal',         { fg = c.fg_muted })

-- gutter and cursor
hi('Cursor',          { fg = c.bg, bg = c.cursor })
hi('iCursor',         { fg = c.bg, bg = c.cursor })
hi('lCursor',         { fg = c.bg, bg = c.cursor })
hi('CursorIM',        { fg = c.bg, bg = c.cursor })
hi('LineNr',          { fg = c.fg_muted, bg = c.bg })
hi('CursorLineNr',    { fg = c.fg_active_n, bg = c.bg_active, bold = true })
hi('CursorLine',      { bg = c.bg_active })
hi('CursorColumn',    { bg = c.bg_active })
hi('SignColumn',      { fg = c.fg_muted, bg = c.bg })
hi('FoldColumn',      { fg = c.fg_muted, bg = c.bg })
hi('Folded',          { fg = c.fg_muted, bg = c.bg_panel })
hi('ColorColumn',     { bg = c.bg_active })

-- splits and tabs
hi('VertSplit',       { fg = c.border, bg = c.bg })
hi('WinSeparator',    { fg = c.border, bg = c.bg })
hi('TabLine',         { fg = c.fg_muted, bg = c.bg_tab })
hi('TabLineSel',      { fg = c.fg, bg = c.bg, bold = true })
hi('TabLineFill',     { bg = c.bg_panel })

-- selection / search
hi('Visual',          { bg = c.bg_select })
hi('VisualNOS',       { bg = c.bg_select })
hi('Search',          { fg = c.fg, bg = c.bg_match })
hi('IncSearch',       { fg = c.fg, bg = c.bg_hl })
hi('CurSearch',       { fg = c.fg, bg = c.bg_hl })
hi('MatchParen',      { fg = '#ffd700', bold = true })
hi('Substitute',      { fg = c.fg, bg = c.bg_hl })

-- popup menu / lists
hi('Pmenu',           { fg = c.fg, bg = c.bg_panel })
hi('PmenuSel',        { fg = '#ffffff', bg = c.bg_list_act })
hi('PmenuSbar',       { bg = c.bg_active })
hi('PmenuThumb',      { bg = c.fg_muted })
hi('PmenuKind',       { fg = c.type, bg = c.bg_panel })
hi('PmenuKindSel',    { fg = c.type, bg = c.bg_list_act })
hi('PmenuExtra',      { fg = c.fg_muted, bg = c.bg_panel })
hi('PmenuExtraSel',   { fg = c.fg_muted, bg = c.bg_list_act })
hi('Question',        { fg = c.info })

-- status line. We render our OWN status bar in Go, so nvim's StatusLine
-- only ever shows up as a split-boundary highlight (e.g. above the
-- integrated terminal panel). Painting it with the accent (blue) made
-- that boundary glow blue between editor and terminal — keep it muted.
hi('StatusLine',      { fg = c.fg_muted, bg = c.bg_panel })
hi('StatusLineNC',    { fg = c.fg_muted, bg = c.bg_panel })
hi('MsgArea',         { fg = c.fg, bg = c.bg })
hi('MoreMsg',         { fg = c.info })
hi('ModeMsg',         { fg = c.fg })
hi('WarningMsg',      { fg = c.warn })
hi('ErrorMsg',        { fg = c.error_ })

-- diagnostics
hi('DiagnosticError',         { fg = c.error_ })
hi('DiagnosticWarn',          { fg = c.warn })
hi('DiagnosticInfo',          { fg = c.info })
hi('DiagnosticHint',          { fg = c.hint })
hi('DiagnosticUnderlineError',{ undercurl = true, sp = c.error_ })
hi('DiagnosticUnderlineWarn', { undercurl = true, sp = c.warn })
hi('DiagnosticUnderlineInfo', { undercurl = true, sp = c.info })
hi('DiagnosticUnderlineHint', { undercurl = true, sp = c.hint })
hi('DiagnosticVirtualTextError', { fg = c.error_, bg = c.bg })
hi('DiagnosticVirtualTextWarn',  { fg = c.warn, bg = c.bg })
hi('DiagnosticVirtualTextInfo',  { fg = c.info, bg = c.bg })
hi('DiagnosticVirtualTextHint',  { fg = c.hint, bg = c.bg })

-- diff / git signs
hi('DiffAdd',         { bg = '#1e3a1e' })
hi('DiffChange',      { bg = '#2e2e1e' })
hi('DiffDelete',      { fg = c.error_, bg = '#3a1e1e' })
hi('DiffText',        { bg = '#3e3e1e' })
hi('GitSignsAdd',     { fg = c.git_add })
hi('GitSignsChange',  { fg = c.git_mod })
hi('GitSignsDelete',  { fg = c.git_del })
hi('SignAdd',         { fg = c.git_add })
hi('SignChange',      { fg = c.git_mod })
hi('SignDelete',      { fg = c.git_del })

-- focus / inputs
hi('FloatTitle',      { fg = c.fg, bg = c.bg_panel, bold = true })
hi('Title',           { fg = c.fg, bold = true })
hi('Directory',       { fg = c.keyword })

-- ─── traditional vim syntax groups ─────────────────────────────────────
hi('Comment',         { fg = c.comment, italic = true })
hi('Constant',        { fg = c.constant })
hi('String',          { fg = c.string })
hi('Character',       { fg = c.string })
hi('Number',          { fg = c.number })
hi('Boolean',         { fg = c.keyword })
hi('Float',           { fg = c.number })
hi('Identifier',      { fg = c.variable })
hi('Function',        { fg = c['function'] })
hi('Statement',       { fg = c.control })
hi('Conditional',     { fg = c.control })
hi('Repeat',          { fg = c.control })
hi('Label',           { fg = c.control })
hi('Operator',        { fg = c.operator })
hi('Keyword',         { fg = c.keyword })
hi('Exception',       { fg = c.control })
hi('PreProc',         { fg = c.preproc })
hi('Include',         { fg = c.control })
hi('Define',          { fg = c.preproc })
hi('Macro',           { fg = c.preproc })
hi('PreCondit',       { fg = c.preproc })
hi('Type',            { fg = c.type })
hi('StorageClass',    { fg = c.keyword })
hi('Structure',       { fg = c.type })
hi('Typedef',         { fg = c.type })
hi('Special',         { fg = c.special })
hi('SpecialChar',     { fg = c.special })
hi('Tag',             { fg = c.keyword })
hi('Delimiter',       { fg = c.operator })
hi('SpecialComment',  { fg = c.comment, bold = true, italic = true })
hi('Debug',           { fg = c.string })
hi('Underlined',      { fg = c.keyword, underline = true })
hi('Error',           { fg = c.error_ })
hi('Todo',            { fg = c.string, bold = true })

-- ─── tree-sitter groups ────────────────────────────────────────────────
hi('@comment',                { fg = c.comment, italic = true })
hi('@keyword',                { fg = c.keyword })
hi('@keyword.function',       { fg = c.keyword })
hi('@keyword.return',         { fg = c.control })
hi('@keyword.operator',       { fg = c.keyword })
hi('@keyword.import',         { fg = c.control })
hi('@keyword.conditional',    { fg = c.control })
hi('@keyword.repeat',         { fg = c.control })
hi('@keyword.exception',      { fg = c.control })
hi('@keyword.directive',      { fg = c.preproc })
hi('@keyword.modifier',       { fg = c.keyword })
hi('@keyword.coroutine',      { fg = c.keyword })
hi('@conditional',            { fg = c.control })
hi('@repeat',                 { fg = c.control })
hi('@exception',              { fg = c.control })
hi('@string',                 { fg = c.string })
hi('@string.escape',          { fg = c.special })
hi('@string.regex',           { fg = c.regex })
hi('@string.regexp',          { fg = c.regex })
hi('@string.special',         { fg = c.special })
hi('@number',                 { fg = c.number })
hi('@boolean',                { fg = c.keyword })
hi('@float',                  { fg = c.number })
hi('@function',               { fg = c['function'] })
hi('@function.call',          { fg = c['function'] })
hi('@function.builtin',       { fg = c['function'] })
hi('@function.method',        { fg = c['function'] })
hi('@function.method.call',   { fg = c['function'] })
hi('@function.macro',         { fg = c.preproc })
hi('@method',                 { fg = c['function'] })
hi('@method.call',            { fg = c['function'] })
hi('@constructor',            { fg = c.type })
hi('@parameter',              { fg = c.variable })
hi('@variable.parameter',     { fg = c.variable })
hi('@variable.member',        { fg = c.variable })
hi('@type',                   { fg = c.type })
hi('@type.builtin',           { fg = c.type })
hi('@type.qualifier',         { fg = c.keyword })
hi('@constant',               { fg = c.constant })
hi('@constant.builtin',       { fg = c.constant })
hi('@variable',               { fg = c.variable })
hi('@variable.builtin',       { fg = c.constant })
hi('@property',               { fg = c.variable })
hi('@field',                  { fg = c.variable })
hi('@operator',               { fg = c.operator })
hi('@punctuation',            { fg = c.operator })
hi('@punctuation.delimiter',  { fg = c.operator })
hi('@punctuation.bracket',    { fg = c.operator })
hi('@punctuation.special',    { fg = c.special })
hi('@tag',                    { fg = c.keyword })
hi('@tag.attribute',          { fg = c.variable })
hi('@tag.delimiter',          { fg = c.operator })
hi('@text',                   { fg = c.fg })
hi('@text.literal',           { fg = c.string })
hi('@text.uri',               { fg = c.string, underline = true })
hi('@text.title',             { fg = c.keyword, bold = true })
hi('@text.strong',            { bold = true })
hi('@text.emphasis',          { italic = true })
hi('@namespace',              { fg = c.type })
hi('@module',                 { fg = c.type })
hi('@module.builtin',         { fg = c.type })
hi('@attribute',              { fg = c['function'] })

-- LSP semantic tokens
hi('@lsp.type.function',      { fg = c['function'] })
hi('@lsp.type.method',        { fg = c['function'] })
hi('@lsp.type.namespace',     { fg = c.type })
hi('@lsp.type.type',          { fg = c.type })
hi('@lsp.type.struct',        { fg = c.type })
hi('@lsp.type.class',         { fg = c.type })
hi('@lsp.type.interface',     { fg = c.type })
hi('@lsp.type.enum',          { fg = c.type })
hi('@lsp.type.parameter',     { fg = c.variable })
hi('@lsp.type.variable',      { fg = c.variable })
hi('@lsp.type.property',      { fg = c.variable })
hi('@lsp.type.enumMember',    { fg = c.constant })
hi('@lsp.type.macro',         { fg = c.preproc })
hi('@lsp.type.keyword',       { fg = c.keyword })
hi('@lsp.type.string',        { fg = c.string })
hi('@lsp.type.number',        { fg = c.number })
hi('@lsp.type.comment',       { fg = c.comment, italic = true })
hi('@lsp.type.decorator',     { fg = c['function'] })
hi('@lsp.type.modifier',      { fg = c.keyword })
hi('@lsp.typemod.function.defaultLibrary', { fg = c['function'] })

-- Bracket rainbow (works with rainbow-delimiters.nvim or treesitter rainbow).
hi('TermocodeBracket1', { fg = c.br1 })
hi('TermocodeBracket2', { fg = c.br2 })
hi('TermocodeBracket3', { fg = c.br3 })
hi('RainbowDelimiterRed',    { fg = c.br1 })
hi('RainbowDelimiterMagenta',{ fg = c.br2 })
hi('RainbowDelimiterBlue',   { fg = c.br3 })
hi('RainbowDelimiterCyan',   { fg = c.br3 })
hi('RainbowDelimiterGreen',  { fg = c.br1 })
hi('RainbowDelimiterYellow', { fg = c.br1 })
hi('RainbowDelimiterViolet', { fg = c.br2 })

-- Indent guide highlight (used by our extmark renderer).
hi('TermocodeIndentGuide',       { fg = c.indent })
hi('TermocodeIndentGuideActive', { fg = c.indent_a })
`
