package theme

import (
	"sort"
	"strings"
)

// NvimColors returns the colour table the embedded Neovim colourscheme is
// built from (the Lua `c` table in internal/app/theme_lua.go). Every entry
// is derived from the active palette, so chrome and editor always match.
//
// VSCode Dark+ keeps its hand-tuned table (vscodeNvimColors) so the
// default look stays pixel-identical to earlier releases.
func NvimColors(themeID string) map[string]string {
	h := func(c Color256) string { return ToRGB(c).Hex() }
	mix := func(a, b Color256, t float64) string { return Lerp(ToRGB(a), ToRGB(b), t).Hex() }
	bg := BgEditor
	c := map[string]string{
		"bg":          h(bg),
		"bg_panel":    h(BgSidebar),
		"bg_float":    h(BgFloat),
		"bg_activity": h(BgActivityBar),
		"bg_title":    h(BgTitleBar),
		"bg_tab":      h(BgSidebar),
		"bg_active":   h(BgActiveLine),
		"bg_select":   h(BgSelection),
		"bg_match":    mix(bg, AccentAmber, 0.28),
		"bg_hl":       mix(bg, AccentAmber, 0.55),
		"bg_input":    h(BgKeycap),
		"bg_list_act": mix(bg, BorderFocus, 0.35),
		"bg_list_in":  h(BgInactiveSel),
		"bg_btn":      h(BorderFocus),
		"bg_btn_h":    mix(BorderFocus, TextWhite, 0.15),
		"fg":          h(TextPrimary),
		"fg_muted":    h(TextMuted),
		"fg_dim":      h(BorderDefault),
		"fg_active_n": h(ActiveLineNum),
		"fg_sel":      h(TextWhite),
		"accent":      h(BorderFocus),
		"border":      h(BorderDefault),
		"focus":       h(BorderFocus),
		"cursor":      h(Cursor),
		"indent":      mix(bg, TextPrimary, 0.09),
		"indent_a":    mix(bg, TextPrimary, 0.28),
		"whitespace":  mix(bg, TextPrimary, 0.16),
		"inlay":       h(TextDimmer),
		"comment":     h(SyntaxComment),
		"keyword":     h(SyntaxKeyword),
		"control":     h(SyntaxControl),
		"string":      h(SyntaxString),
		"regex":       h(SyntaxRegex),
		"number":      h(SyntaxNumber),
		"function":    h(SyntaxFunction),
		"type":        h(SyntaxType),
		"constant":    h(SyntaxConstant),
		"variable":    h(SyntaxVariable),
		"operator":    h(SyntaxOperator),
		"special":     h(AccentAmber),
		"preproc":     h(SyntaxControl),
		"error_":      h(DiagError),
		"warn":        h(DiagWarning),
		"info":        h(DiagInfo),
		"hint":        h(DiagHint),
		"git_mod":     h(GitModified),
		"git_unt":     h(GitUntracked),
		"git_del":     h(GitDeleted),
		"git_conf":    h(GitConflict),
		"git_ign":     h(GitIgnored),
		"git_add":     h(GitAdded),
		"diff_add":    mix(bg, GitAdded, 0.16),
		"diff_add_t":  mix(bg, GitAdded, 0.32),
		"diff_change": mix(bg, GitModified, 0.12),
		"diff_text":   mix(bg, GitModified, 0.28),
		"diff_del":    mix(bg, GitDeleted, 0.16),
		"diff_del_t":  mix(bg, GitDeleted, 0.32),
		"diff_del_fg": mix(bg, GitDeleted, 0.45),
		"winbar":      h(BgSidebar),
		"winbar_fg":   h(TextSecondary),
		"ai_ghost":    h(AIGhost),
		"ai_accent":   h(AIAccent),
		"dap_bp":      h(DiagError),
		"dap_stop":    h(AccentAmber),
		"dap_stop_bg": mix(bg, AccentAmber, 0.14),
		"test_pass":   h(AccentGreen),
		"test_fail":   h(DiagError),
		"match_paren": h(AccentAmber),
		"br1":         h(bracket(0)),
		"br2":         h(bracket(1)),
		"br3":         h(bracket(2)),
	}
	if normalizeThemeID(themeID) == "vscode-dark-plus" || themeID == "" {
		for k, v := range vscodeNvimColors {
			c[k] = v
		}
	}
	return c
}

func bracket(i int) Color256 {
	if len(BracketPairs) == 0 {
		return AccentAmber
	}
	return BracketPairs[i%len(BracketPairs)]
}

// vscodeNvimColors is the canonical VSCode Dark+ editor palette (kept
// identical to the table the editor used before themes reached nvim).
var vscodeNvimColors = map[string]string{
	"bg": "#1c1c1c", "bg_panel": "#262626", "bg_activity": "#303030", "bg_title": "#3a3a3a",
	"bg_tab": "#2d2d2d", "bg_active": "#2a2d2e", "bg_select": "#005f87", "bg_match": "#515c6a",
	"bg_hl": "#ea5c00", "bg_input": "#3a3a3a", "bg_list_act": "#094771", "bg_list_in": "#37373d",
	"bg_btn": "#0e639c", "bg_btn_h": "#1177bb",
	"fg": "#d0d0d0", "fg_muted": "#858585", "fg_dim": "#3a3a3a", "fg_active_n": "#c6c6c6",
	"fg_sel": "#ffffff", "accent": "#0087d7", "border": "#3a3a3a", "focus": "#007fd4",
	"cursor": "#aeafad", "indent": "#363636", "indent_a": "#5a5a5a", "whitespace": "#3b3a32",
	"inlay":   "#585858",
	"comment": "#6a9955", "keyword": "#569cd6", "control": "#c586c0", "string": "#ce9178",
	"regex": "#d16969", "number": "#b5cea8", "function": "#dcdcaa", "type": "#4ec9b0",
	"constant": "#4fc1ff", "variable": "#9cdcfe", "operator": "#d0d0d0", "special": "#d7ba7d",
	"preproc": "#bd63c5",
	"error_":  "#f14c4c", "warn": "#cca700", "info": "#75beff", "hint": "#3794ff",
	"git_mod": "#e2c08d", "git_unt": "#73c991", "git_del": "#c74e39", "git_conf": "#6c6cc4",
	"git_ign": "#8c8c8c", "git_add": "#587c0c",
	"diff_add": "#16361f", "diff_add_t": "#1f5e34", "diff_change": "#2a2a18", "diff_text": "#4a4a1f",
	"diff_del": "#241317", "diff_del_t": "#5e2b3a", "diff_del_fg": "#4a2630",
	"winbar": "#26292e", "winbar_fg": "#cfd8e3", "match_paren": "#ffd700",
	"br1": "#ffd700", "br2": "#da70d6", "br3": "#179fff",
}

// LuaTable renders the colour map as a Lua table literal with sorted keys
// (stable output, easy to diff in tests).
func LuaTable(c map[string]string) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for _, k := range keys {
		b.WriteString("  ['" + k + "'] = '" + c[k] + "',\n")
	}
	b.WriteString("}")
	return b.String()
}
