package app

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/theme"
)

// themeLua builds the colourscheme for the embedded nvim from the active
// palette (theme.NvimColors): core UI, vim + tree-sitter + LSP semantic
// groups, diagnostics, diff and sign columns, AI ghost text, debug and
// test signs. It is re-run on every theme switch so the editor follows
// the chrome. The table is also published as _G.termocode_colors so other
// Lua chunks (diff views, conflict tints) reuse the same colours instead
// of hard-coding their own.
func themeLua(themeID string, light bool) string {
	bgMode := "dark"
	if light {
		bgMode = "light"
	}
	return "if vim.g.syntax_on then vim.cmd('syntax reset') end\n" +
		"vim.cmd('hi clear')\n" +
		"vim.opt.background = '" + bgMode + "'\n" +
		"vim.g.colors_name = 'termocode'\n" +
		"local c = " + theme.LuaTable(theme.NvimColors(themeID)) + "\n" +
		"_G.termocode_colors = c\n" +
		themeLuaBody
}

// applyNvimTheme pushes the active theme into the embedded nvim and
// recolours the terminals that are already open.
func (m *Model) applyNvimTheme() {
	m.applyNvimThemeLua()
	m.syncTermPalette()
}

// applyNvimThemeLua pushes the active theme's colourscheme into nvim. The
// nvim setup (attachCmd, a background goroutine) calls only this: the
// terminal palette below lives in the render grid.
func (m *Model) applyNvimThemeLua() {
	if m.nvim == nil {
		return
	}
	t, _ := theme.LookupTheme(m.themeID)
	_ = m.nvim.ExecLua(themeLua(t.ID, t.Light))
}

// syncTermPalette gives the editor grid the theme's ANSI colours 0–15.
// nvim reads g:terminal_color_N only when a terminal opens and has no API
// to change the palette of an open one, so the grid maps terminal palette
// cells (ext_hlstate kind "term") to these colours itself.
func (m *Model) syncTermPalette() {
	c := theme.NvimColors(m.themeID)
	var p [16]int
	for i := range p {
		p[i] = -1
		if v, err := strconv.ParseInt(strings.TrimPrefix(c["term_"+strconv.Itoa(i)], "#"), 16, 32); err == nil {
			p[i] = int(v)
		}
	}
	m.editor.SetTermPalette(p)
}

const themeLuaBody = `
local hi = function(group, opts) vim.api.nvim_set_hl(0, group, opts) end

-- Integrated terminal ANSI colours follow the theme (nvim reads them when
-- a terminal opens; terminals that are already open keep their colours).
for i = 0, 15 do
  vim.g['terminal_color_' .. i] = c['term_' .. i]
end

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
hi('MatchParen',      { fg = c.match_paren, bold = true })
hi('Substitute',      { fg = c.fg, bg = c.bg_hl })

-- popup menu / lists
hi('Pmenu',           { fg = c.fg, bg = c.bg_panel })
hi('PmenuSel',        { fg = c.fg_sel, bg = c.bg_list_act })
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
hi('DiffAdd',         { bg = c.diff_add })
hi('DiffChange',      { bg = c.diff_change })
hi('DiffDelete',      { fg = c.diff_del_fg, bg = c.diff_del })
hi('DiffText',        { bg = c.diff_text, bold = true })
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

-- Sign column: per-line change bars, debug and test markers.
hi('TermocodeGitAdd',    { fg = c.git_unt })
hi('TermocodeGitChange', { fg = c.git_mod })
hi('TermocodeGitDelete', { fg = c.git_del })
hi('DapStoppedLine',     { bg = c.dap_stop_bg })
hi('TermocodeDapBreakpoint', { fg = c.dap_bp })
hi('TermocodeDapStopped',    { fg = c.dap_stop })
hi('TermocodeTestPass',  { fg = c.test_pass })
hi('TermocodeTestFail',  { fg = c.test_fail })
hi('LspInlayHint',       { fg = c.inlay, italic = true })
hi('TermocodeAIGhost',   { fg = c.ai_ghost, italic = true })
hi('TermocodeAIAccent',  { fg = c.ai_accent, bold = true })
hi('LspReferenceText',   { bg = c.bg_list_in })
hi('LspReferenceRead',   { bg = c.bg_list_in })
hi('LspReferenceWrite',  { bg = c.bg_list_in })

-- Diff views (side-by-side + commit diffs) re-run this when they open, so
-- they always match the current theme.
function _G.termocode_diff_hl()
  local C = _G.termocode_colors or c
  hi('DiffAdd',      { bg = C.diff_add })
  hi('DiffChange',   { bg = C.diff_change })
  hi('DiffText',     { bg = C.diff_text, bold = true })
  hi('DiffDelete',   { bg = C.diff_del, fg = C.diff_del_fg })
  hi('WinBar',       { fg = C.winbar_fg, bg = C.winbar })
  hi('WinBarNC',     { fg = C.fg_muted, bg = C.winbar })
  hi('TcDiffHdr',    { fg = C.fg, bg = C.winbar, bold = true })
  hi('TcDiffHdrDim', { fg = C.fg_muted, bg = C.winbar })
  hi('TcDiffCount',  { fg = C.fg_muted, bg = C.winbar, bold = true })
  hi('TcDiffOk',     { fg = C.git_unt, bg = C.winbar })
  hi('TcDiffNav',    { fg = C.accent, bg = C.bg_list_in, bold = true })
  hi('diffAdded',    { fg = C.git_unt })
  hi('diffRemoved',  { fg = C.git_del })
  hi('diffLine',     { fg = C.keyword })
  hi('diffFile',     { fg = C['function'], bold = true })
  hi('diffIndexLine',{ fg = C.fg_muted })
end
_G.termocode_diff_hl()

-- Find Results page (find_results_lua.go draws with these).
hi('TermocodeFindIcon',    { fg = c.fg_muted })
hi('TermocodeFindQuery',   { fg = c.fg_sel, bg = c.bg_input })
hi('TermocodeFindChipOn',  { fg = c.fg_sel, bg = c.bg_list_in })
hi('TermocodeFindChipOff', { fg = c.fg_muted })
hi('TermocodeFindMeta',    { fg = c.inlay })
hi('TermocodeFindSummary', { fg = c.fg_muted })
hi('TermocodeFindWarn',    { fg = c.warn })
hi('TermocodeFindError',   { fg = c.error_ })
hi('TermocodeFindHair1',   { fg = c.accent })
hi('TermocodeFindHair2',   { fg = c.find_hair_mid })
hi('TermocodeFindHair3',   { fg = c.find_hair })
hi('TermocodeFindFold',    { fg = c.fg_muted })
hi('TermocodeFindDir',     { fg = c.fg_muted })
hi('TermocodeFindName',    { fg = c.fg })
hi('TermocodeFindCount',   { fg = c.fg_muted, bg = c.bg_input })
hi('TermocodeFindLineNr',  { fg = c.inlay })
hi('TermocodeFindMatch',   { fg = c.fg_sel, bg = c.bg_match })
hi('TermocodeFindCursor',  { bg = c.find_cursor })
hi('TermocodeFindFolded',  { fg = c.fg, bg = c.bg })
hi('TermocodeFindKeycap',  { fg = c.fg_sel, bg = c.bg_input })
hi('TermocodeFindHint',    { fg = c.inlay })
if _G._termocode_find_rehl then _G._termocode_find_rehl() end
-- HTTP client: .http buffers (http_buffer_lua.go) and the HTTP Response
-- page (http_page_lua.go). One bold only: the status number on the page.
hi('TermocodeHttpSep',         { fg = c.comment })
hi('TermocodeHttpSepTitle',    { fg = c.fg_sel })
hi('TermocodeHttpComment',     { fg = c.comment })
hi('TermocodeHttpDirective',   { fg = c.preproc })
hi('TermocodeHttpVarLine',     { fg = c.fg })
hi('TermocodeHttpVarName',     { fg = c.variable })
hi('TermocodeHttpVarValue',    { fg = c.string })
hi('TermocodeHttpRequest',     { fg = c.fg })
hi('TermocodeHttpMethod',      { fg = c.keyword })
hi('TermocodeHttpURL',         { fg = c.string })
hi('TermocodeHttpVersion',     { fg = c.comment })
hi('TermocodeHttpHeader',      { fg = c.fg })
hi('TermocodeHttpHeaderName',  { fg = c.variable })
hi('TermocodeHttpColon',       { fg = c.operator })
hi('TermocodeHttpPlaceholder', { fg = c.constant })
hi('TermocodeHttpBody',        { fg = c.fg })
hi('TermocodeHttpUnresolved',  { undercurl = true, sp = c.warn })
hi('TermocodeHttpLens',        { fg = c.inlay })
hi('TermocodeHttpLensBusy',    { fg = c.accent })
hi('TermocodeHttpLensOK',      { fg = c.test_pass })
hi('TermocodeHttpLensInfo',    { fg = c.info })
hi('TermocodeHttpLensWarn',    { fg = c.warn })
hi('TermocodeHttpLensErr',     { fg = c.error_ })
hi('TermocodeHttpStatusOK',    { fg = c.test_pass, bold = true })
hi('TermocodeHttpStatusInfo',  { fg = c.info, bold = true })
hi('TermocodeHttpStatusWarn',  { fg = c.warn, bold = true })
hi('TermocodeHttpStatusErr',   { fg = c.error_, bold = true })
hi('TermocodeHttpReq',         { fg = c.fg_sel })
hi('TermocodeHttpName',        { fg = c.fg_muted })
hi('TermocodeHttpByAI',        { fg = c.fg_muted })
hi('TermocodeHttpSummary',     { fg = c.fg_muted })
hi('TermocodeHttpChipOn',      { fg = c.fg_sel, bg = c.find_cursor })
hi('TermocodeHttpChipOff',     { fg = c.fg_muted })
hi('TermocodeHttpKeycap',      { fg = c.fg_sel, bg = c.bg_input })
hi('TermocodeHttpHint',        { fg = c.inlay })
hi('TermocodeHttpSection',     { fg = c.fg_sel })
hi('TermocodeHttpHdrName',     { fg = c.variable })
hi('TermocodeHttpHop',         { fg = c.fg_muted })
hi('TermocodeHttpFolded',      { fg = c.fg, bg = c.bg })
if _G.termocode_bracket_hl then _G.termocode_bracket_hl() end
`
