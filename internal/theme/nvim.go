package theme

import (
	"math"
	"sort"
	"strconv"
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
		// Find Results page (find_results_lua.go).
		"find_cursor":   string(Wash(bg, WashSelected)),
		"find_hair_mid": Lerp(ToRGB(BorderFocus), ToRGB(AccentLavender), 0.5).Hex(),
		"find_hair":     mix(bg, BorderDefault, 0.75),
	}
	// Terminal ANSI colours (term_0 … term_15 → g:terminal_color_N) so
	// the integrated terminal follows the theme instead of nvim's harsh
	// built-in defaults (pure blue on black).
	bright := func(x Color256) string { return mix(x, TextWhite, 0.25) }
	if isLightBg() {
		bright = func(x Color256) string { return mix(x, TextPrimary, 0.2) }
	}
	ansi := []Color256{BgActivityBar, DiagError, AccentGreen, AccentAmber, AccentBlue, AccentMagenta, SyntaxType, TextSecondary}
	if isLightBg() {
		ansi[0], ansi[7] = TextPrimary, TextMuted
	}
	for i, col := range ansi {
		c[termKey(i)] = h(col)
		c[termKey(i+8)] = bright(col)
	}
	c[termKey(8)] = h(TextMuted)
	c[termKey(15)] = h(TextWhite)
	// Theme accents are tuned for chrome, not for text on the editor
	// background (pale amber on Latte, red on Solarized teal). Nudge every
	// text colour toward the foreground until it reads.
	for i := 1; i < 16; i++ {
		if i == 8 {
			continue
		}
		c[termKey(i)] = readableOn(c[termKey(i)], c["bg"], c["fg"], 3.2)
	}
	if normalizeThemeID(themeID) == "vscode-dark-plus" || themeID == "" {
		for k, v := range vscodeNvimColors {
			c[k] = v
		}
	}
	return c
}

// readableOn moves hex toward fg in small steps until its contrast ratio
// against bg reaches min (WCAG formula). Colours that already pass are
// returned unchanged.
func readableOn(hex, bg, fg string, min float64) string {
	c, ok1 := ParseHex(hex)
	b, ok2 := ParseHex(bg)
	f, ok3 := ParseHex(fg)
	if !ok1 || !ok2 || !ok3 {
		return hex
	}
	for t := 0.0; t <= 1.0; t += 0.05 {
		x := Lerp(c, f, t)
		if contrastRatio(x, b) >= min {
			return x.Hex()
		}
	}
	return f.Hex()
}

func contrastRatio(a, b RGB) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relLuminance(c RGB) float64 {
	ch := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func termKey(i int) string { return "term_" + strconv.Itoa(i) }

// isLightBg reports whether the active editor background is light.
func isLightBg() bool {
	r := ToRGB(BgEditor)
	return 0.299*float64(r.R)+0.587*float64(r.G)+0.114*float64(r.B) > 128
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
	// VSCode's default dark terminal palette.
	"term_0": "#000000", "term_1": "#cd3131", "term_2": "#0dbc79", "term_3": "#e5e510",
	"term_4": "#2472c8", "term_5": "#bc3fbc", "term_6": "#11a8cd", "term_7": "#e5e5e5",
	"term_8": "#666666", "term_9": "#f14c4c", "term_10": "#23d18b", "term_11": "#f5f543",
	"term_12": "#3b8eea", "term_13": "#d670d6", "term_14": "#29b8db", "term_15": "#e5e5e5",
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
