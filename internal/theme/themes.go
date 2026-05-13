package theme

import (
	"sort"
	"strings"
)

// NamedTheme bundles a human-readable label, a stable ID used in the
// persisted config, and the Palette256 + Theme styles to apply.
type NamedTheme struct {
	ID      string
	Name    string
	Palette Palette256
	Styles  Theme
}

// allThemes holds every built-in theme keyed by its lower-case ID. The lookup
// helpers below are tolerant of "VSCode Dark+" / "vscode-dark-plus" / etc.
var allThemes = map[string]NamedTheme{
	"vscode-dark-plus": {
		ID:      "vscode-dark-plus",
		Name:    "VSCode Dark+",
		Palette: vscodeDarkPlusPalette(),
		Styles:  DarkPlus(),
	},
	"github-dark": {
		ID:      "github-dark",
		Name:    "GitHub Dark",
		Palette: githubDarkPalette(),
		Styles:  GitHubDark(),
	},
	"one-dark": {
		ID:      "one-dark",
		Name:    "One Dark",
		Palette: oneDarkPalette(),
		Styles:  OneDark(),
	},
	"solarized-dark": {
		ID:      "solarized-dark",
		Name:    "Solarized Dark",
		Palette: solarizedDarkPalette(),
		Styles:  SolarizedDark(),
	},
}

// AvailableThemes returns the list of built-in themes in display order.
// VSCode Dark+ is always first (the default); the rest sort alphabetically by
// human-readable name so the picker reads naturally. If the user has a
// non-empty user_theme.json, "Custom" is appended at the end so the picker
// surfaces the user's own theme alongside the built-ins.
func AvailableThemes() []NamedTheme {
	out := make([]NamedTheme, 0, len(allThemes)+1)
	for _, t := range allThemes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == "vscode-dark-plus" {
			return true
		}
		if out[j].ID == "vscode-dark-plus" {
			return false
		}
		return out[i].Name < out[j].Name
	})
	if HasUserTheme() {
		out = append(out, customUserTheme())
	}
	return out
}

// customUserTheme constructs a NamedTheme for the user's overrides.
// Built atop VSCode Dark+ so any field the user hasn't customised
// inherits the canonical default — "Custom" is therefore safe to apply
// even from a single-key user_theme.json.
func customUserTheme() NamedTheme {
	pal := vscodeDarkPlusPalette()
	overrides := LoadUserOverrides()
	for k, hex := range overrides {
		applyHexOverride(&pal, k, hex)
	}
	return NamedTheme{
		ID:      UserThemeID,
		Name:    UserThemeName,
		Palette: pal,
		// Reuse DarkPlus composite styles — they don't expose the same
		// per-field knobs the user theme covers, and lipgloss styles
		// don't show up in the editor area itself (which reads from the
		// 256-index palette directly).
		Styles: DarkPlus(),
	}
}

// applyHexOverride mutates a Palette256 field by name, registering a fresh
// 256-index for the supplied hex so Hex() can return the precise truecolor
// string at render time. Unknown field names are silently dropped (so a
// stale user_theme.json from a future version doesn't blow up).
func applyHexOverride(p *Palette256, field, hex string) {
	if !IsHexColor(hex) {
		return
	}
	hex = strings.ToLower(strings.TrimSpace(hex))
	// Pick a fresh 256-index that doesn't already appear in defaultIndexHex
	// so the new hex doesn't accidentally collide with another color in
	// the palette. The actual index value is opaque — only the IndexHex
	// map drives rendering.
	idx := nextFreeIndex(p)
	if p.IndexHex == nil {
		p.IndexHex = mergeHex(defaultIndexHex(), nil)
	}
	p.IndexHex[idx] = hex
	switch field {
	case "BgEditor":
		p.BgEditor = idx
	case "BgSidebar":
		p.BgSidebar = idx
	case "BgActivityBar":
		p.BgActivityBar = idx
	case "BgTitleBar":
		p.BgTitleBar = idx
	case "BgPanel":
		p.BgPanel = idx
	case "BgStatusBar":
		p.BgStatusBar = idx
	case "BgSelection":
		p.BgSelection = idx
	case "BgHover":
		p.BgHover = idx
	case "BgActiveLine":
		p.BgActiveLine = idx
	case "BgInactiveSel":
		p.BgInactiveSel = idx
	case "BgSectionHdr":
		p.BgSectionHdr = idx
	case "BgKeycap":
		p.BgKeycap = idx
	case "TextPrimary":
		p.TextPrimary = idx
	case "TextSecondary":
		p.TextSecondary = idx
	case "TextMuted":
		p.TextMuted = idx
	case "TextDim":
		p.TextDim = idx
	case "TextDimmer":
		p.TextDimmer = idx
	case "TextQuaternary":
		p.TextQuaternary = idx
	case "TextWhite":
		p.TextWhite = idx
	case "Cursor":
		p.Cursor = idx
	case "ActiveLineNum":
		p.ActiveLineNum = idx
	case "BorderDefault":
		p.BorderDefault = idx
	case "BorderFocus":
		p.BorderFocus = idx
	case "BorderSubtle":
		p.BorderSubtle = idx
	case "AccentBlue":
		p.AccentBlue = idx
	case "AccentGreen":
		p.AccentGreen = idx
	case "AccentAmber":
		p.AccentAmber = idx
	case "AccentRedCoral":
		p.AccentRedCoral = idx
	case "AccentMagenta":
		p.AccentMagenta = idx
	case "AccentLavender":
		p.AccentLavender = idx
	case "SyntaxKeyword":
		p.SyntaxKeyword = idx
	case "SyntaxControl":
		p.SyntaxControl = idx
	case "SyntaxFunction":
		p.SyntaxFunction = idx
	case "SyntaxVariable":
		p.SyntaxVariable = idx
	case "SyntaxType":
		p.SyntaxType = idx
	case "SyntaxString":
		p.SyntaxString = idx
	case "SyntaxNumber":
		p.SyntaxNumber = idx
	case "SyntaxComment":
		p.SyntaxComment = idx
	case "SyntaxRegex":
		p.SyntaxRegex = idx
	case "SyntaxConstant":
		p.SyntaxConstant = idx
	case "SyntaxOperator":
		p.SyntaxOperator = idx
	case "DiagError":
		p.DiagError = idx
	case "DiagWarning":
		p.DiagWarning = idx
	case "DiagInfo":
		p.DiagInfo = idx
	case "DiagHint":
		p.DiagHint = idx
	case "GitModified":
		p.GitModified = idx
	case "GitAdded":
		p.GitAdded = idx
	case "GitUntracked":
		p.GitUntracked = idx
	case "GitDeleted":
		p.GitDeleted = idx
	case "GitConflict":
		p.GitConflict = idx
	case "GitIgnored":
		p.GitIgnored = idx
	case "GitSubmodule":
		p.GitSubmodule = idx
	default:
		// unknown field: drop the entry we just registered so we don't
		// litter the IndexHex map with orphaned indices on every load.
		delete(p.IndexHex, idx)
	}
}

// nextFreeIndex returns a 256-index that isn't already used by p.IndexHex
// nor by defaultIndexHex(). Falls back to monotonically increasing from
// 16 (skipping the standard 0–15 ANSI block) so reservations stay
// stable across many overrides.
func nextFreeIndex(p *Palette256) Color256 {
	used := map[Color256]bool{}
	for k := range defaultIndexHex() {
		used[k] = true
	}
	if p != nil {
		for k := range p.IndexHex {
			used[k] = true
		}
	}
	for i := 16; i < 256; i++ {
		if !used[i] {
			return i
		}
	}
	// Pathological fallback — we ran out of indices (shouldn't happen
	// since we have ~200 free slots). Reuse 255 deterministically; the
	// user gets a near-collision but no crash.
	return 255
}

// LookupTheme finds a theme by ID (case-insensitive, hyphen/space-tolerant).
// Returns the theme and true on hit, the default theme and false on miss.
// The user theme (UserThemeID) resolves only when a non-empty
// user_theme.json exists on disk; otherwise it falls through to the
// default-on-miss path so legacy callers still see a real theme.
func LookupTheme(id string) (NamedTheme, bool) {
	key := normalizeThemeID(id)
	if t, ok := allThemes[key]; ok {
		return t, true
	}
	if key == UserThemeID && HasUserTheme() {
		return customUserTheme(), true
	}
	return allThemes["vscode-dark-plus"], false
}

func normalizeThemeID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "+", "-plus")
	// collapse runs of "-" introduced by replacements
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// ApplyTheme swaps both the palette (the Color256 vars) and returns the
// composite Theme styles for the named theme. Missing IDs fall back to
// VSCode Dark+ silently.
func ApplyTheme(id string) (NamedTheme, bool) {
	t, ok := LookupTheme(id)
	SetPalette(t.Palette)
	return t, ok
}

// ─── Built-in palettes ─────────────────────────────────────────────────
//
// All four palettes are deliberately built atop the same xterm-256 indices
// where possible — only the bg/fg/syntax slots that are most visible on
// screen diverge. v1 prioritizes "obviously different at a glance" over
// pixel-accurate clones of the upstream themes.

func vscodeDarkPlusPalette() Palette256 {
	// This matches the legacy hard-coded palette so existing screenshots and
	// nvim Lua highlight groups stay pixel-identical when the user hasn't
	// picked a different theme.
	return Palette256{
		BgEditor:      234,
		BgSidebar:     236,
		BgActivityBar: 237,
		BgTitleBar:    237,
		BgPanel:       233,
		BgStatusBar:   32,
		BgSelection:   24,
		BgHover:       235,
		BgActiveLine:  235,
		BgInactiveSel: 237,
		BgSectionHdr:  237,
		BgKeycap:      236,

		TextPrimary:    252,
		TextSecondary:  250,
		TextMuted:      102,
		TextDim:        242,
		TextDimmer:     240,
		TextQuaternary: 238,
		TextWhite:      231,
		Cursor:         145,
		ActiveLineNum:  251,

		AccentBlue:     39,
		AccentGreen:    78,
		AccentAmber:    178,
		AccentRedCoral: 203,
		AccentMagenta:  170,
		AccentLavender: 141,

		BorderDefault: 238,
		BorderFocus:   32,
		BorderSubtle:  237,

		SyntaxKeyword:  74,
		SyntaxControl:  175,
		SyntaxFunction: 187,
		SyntaxVariable: 153,
		SyntaxType:     79,
		SyntaxString:   174,
		SyntaxNumber:   151,
		SyntaxComment:  65,
		SyntaxRegex:    167,
		SyntaxConstant: 75,
		SyntaxOperator: 252,

		DiagError:   203,
		DiagWarning: 178,
		DiagInfo:    111,
		DiagHint:    102,

		GitModified:  180,
		GitAdded:     78,
		GitUntracked: 78,
		GitDeleted:   167,
		GitConflict:  62,
		GitIgnored:   245,
		GitSubmodule: 110,

		BracketPairs: []Color256{220, 170, 39},
		IndexHex:     defaultIndexHex(),
	}
}

func githubDarkPalette() Palette256 {
	p := vscodeDarkPlusPalette()
	// GitHub Dark uses near-black bgs and slightly desaturated syntax.
	p.BgEditor = 232      // #080808
	p.BgSidebar = 233     // #121212
	p.BgActivityBar = 234 // #1c1c1c
	p.BgTitleBar = 234
	p.BgPanel = 232
	p.BgStatusBar = 24 // a deeper blue than VSCode's
	p.BgHover = 234
	p.BgActiveLine = 234
	p.BgInactiveSel = 235
	p.BgSectionHdr = 234

	p.SyntaxKeyword = 169  // pinkish red — keywords (e.g. function, return)
	p.SyntaxControl = 169  // import/package
	p.SyntaxFunction = 110 // muted blue
	p.SyntaxString = 109   // teal
	p.SyntaxConstant = 73
	p.SyntaxType = 109
	p.SyntaxComment = 102

	// Add the indices we just introduced to the hex map.
	p.IndexHex = mergeHex(p.IndexHex, map[Color256]string{
		232: "#080808",
		169: "#d75faf",
		109: "#87afaf",
		73:  "#5fafaf",
	})
	return p
}

func oneDarkPalette() Palette256 {
	p := vscodeDarkPlusPalette()
	// One Dark — Atom-style: warmer bg, purple keywords.
	p.BgEditor = 235      // #262626 (close to #282c34)
	p.BgSidebar = 236     // #303030
	p.BgActivityBar = 237 // #3a3a3a
	p.BgTitleBar = 237
	p.BgPanel = 234
	p.BgHover = 236
	p.BgActiveLine = 236
	p.BgStatusBar = 60 // muted purple-grey

	p.SyntaxKeyword = 176  // purple — if/for/return/func
	p.SyntaxControl = 176  // import/package — same purple
	p.SyntaxFunction = 39  // azure
	p.SyntaxString = 144   // soft green
	p.SyntaxType = 179     // soft yellow
	p.SyntaxConstant = 173 // orange
	p.SyntaxNumber = 173
	p.SyntaxComment = 244

	p.IndexHex = mergeHex(p.IndexHex, map[Color256]string{
		60:  "#5f5f87",
		176: "#d787d7",
		144: "#afaf87",
		179: "#d7af5f",
		173: "#d7875f",
		244: "#808080",
	})
	return p
}

func solarizedDarkPalette() Palette256 {
	p := vscodeDarkPlusPalette()
	// Solarized Dark — Ethan Schoonover's classic teal-bg scheme.
	p.BgEditor = 23       // #005f5f (base03-ish)
	p.BgSidebar = 23      // base02
	p.BgActivityBar = 22  // even darker teal
	p.BgTitleBar = 22
	p.BgPanel = 22
	p.BgHover = 24
	p.BgActiveLine = 24
	p.BgStatusBar = 60
	p.BgSelection = 60

	p.TextPrimary = 144   // base0
	p.TextSecondary = 144 // base0
	p.TextMuted = 66      // base01

	p.SyntaxKeyword = 64   // green
	p.SyntaxControl = 125  // magenta
	p.SyntaxFunction = 33  // blue
	p.SyntaxString = 37    // cyan
	p.SyntaxType = 136     // yellow
	p.SyntaxConstant = 166 // orange
	p.SyntaxNumber = 166
	p.SyntaxComment = 66
	p.SyntaxRegex = 125

	p.IndexHex = mergeHex(p.IndexHex, map[Color256]string{
		22:  "#005f00",
		23:  "#005f5f",
		33:  "#0087ff",
		37:  "#00afaf",
		60:  "#5f5f87",
		64:  "#5f8700",
		66:  "#5f8787",
		125: "#af005f",
		136: "#af8700",
		144: "#afaf87",
		166: "#d75f00",
	})
	return p
}

// mergeHex returns a fresh map combining base and overrides; overrides win.
// Always allocates a new map so callers can mutate the result without
// touching defaultIndexHex.
func mergeHex(base, overrides map[Color256]string) map[Color256]string {
	out := make(map[Color256]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
