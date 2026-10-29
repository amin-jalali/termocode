package theme

// Spec describes a theme by its upstream colour names (Catppuccin-style
// "base / mantle / crust / surface / overlay / text" ladder plus accents).
// FromSpec turns it into a Palette256, so a new theme is one Spec literal:
// every chrome token and every nvim highlight group is derived from it.
type Spec struct {
	ID, Name string
	Light    bool

	// Background ladder: Base is the editor, Mantle the sidebars, Crust
	// the deepest chrome (activity bar, status bar, panel).
	Base, Mantle, Crust string
	// Surfaces: hover / inactive selection / strong borders.
	Surface0, Surface1, Surface2 string
	// Dim text ladder, dimmest first.
	Overlay0, Overlay1 string
	Subtext0, Subtext1 string
	Text               string
	Cursor             string // optional (defaults to Text)
	LineNr             string // optional active line number (defaults to Text)

	Accent    string // focus ring, status pill, selection bar
	Selection string // Visual / selection background

	Red, Orange, Yellow, Green, Teal, Cyan, Blue, Purple, Pink string

	// Syntax roles.
	Keyword, Control, Function, Variable, Type, String, Number string
	Comment, Regex, Constant, Operator                         string
}

// FromSpec builds the palette for s. Colour indices are opaque keys into
// IndexHex (allocated from 16 up, one per distinct hex), so lipgloss gets
// the exact truecolor value and downgrades it itself on 256/16-colour
// terminals.
func FromSpec(s Spec) Palette256 {
	hex := map[Color256]string{}
	byHex := map[string]Color256{}
	next := Color256(16)
	idx := func(h string) Color256 {
		if h == "" {
			return 0
		}
		c, ok := ParseHex(h)
		if !ok {
			return 0
		}
		h = c.Hex()
		if i, ok := byHex[h]; ok {
			return i
		}
		i := next
		next++
		byHex[h] = i
		hex[i] = h
		return i
	}
	rgb := func(h string) RGB { c, _ := ParseHex(h); return c }
	mix := func(a, b string, t float64) Color256 { return idx(Lerp(rgb(a), rgb(b), t).Hex()) }
	or := func(v, d string) string {
		if v != "" {
			return v
		}
		return d
	}

	white, black := "#ffffff", "#000000"
	if s.Light {
		s = lightReadable(s)
	}
	p := Palette256{
		BgEditor:      idx(s.Base),
		BgSidebar:     idx(s.Mantle),
		BgActivityBar: idx(s.Crust),
		BgTitleBar:    idx(s.Mantle),
		BgPanel:       idx(s.Crust),
		BgStatusBar:   idx(s.Accent),
		BgSelection:   idx(s.Selection),
		BgHover:       mix(s.Base, s.Surface0, 0.6),
		BgActiveLine:  mix(s.Base, s.Surface0, 0.45),
		BgInactiveSel: idx(s.Surface0),
		BgSectionHdr:  idx(s.Mantle),
		BgKeycap:      idx(s.Surface0),

		TextPrimary:    idx(s.Text),
		TextSecondary:  idx(s.Subtext1),
		TextMuted:      idx(s.Overlay1),
		TextDim:        idx(s.Overlay0),
		TextDimmer:     idx(s.Surface2),
		TextQuaternary: idx(s.Surface1),
		Cursor:         idx(or(s.Cursor, s.Text)),
		ActiveLineNum:  idx(or(s.LineNr, s.Text)),

		AccentBlue:     idx(s.Blue),
		AccentGreen:    idx(s.Green),
		AccentAmber:    idx(s.Yellow),
		AccentRedCoral: idx(s.Red),
		AccentMagenta:  idx(s.Pink),
		AccentLavender: idx(s.Purple),

		AIGhost:  idx(s.Overlay0),
		AIAccent: idx(s.Purple),

		BorderDefault: idx(s.Surface1),
		BorderFocus:   idx(s.Accent),
		BorderSubtle:  idx(s.Surface0),

		SyntaxKeyword:  idx(s.Keyword),
		SyntaxControl:  idx(s.Control),
		SyntaxFunction: idx(s.Function),
		SyntaxVariable: idx(s.Variable),
		SyntaxType:     idx(s.Type),
		SyntaxString:   idx(s.String),
		SyntaxNumber:   idx(s.Number),
		SyntaxComment:  idx(s.Comment),
		SyntaxRegex:    idx(s.Regex),
		SyntaxConstant: idx(s.Constant),
		SyntaxOperator: idx(s.Operator),

		DiagError:   idx(s.Red),
		DiagWarning: idx(s.Yellow),
		DiagInfo:    idx(s.Cyan),
		DiagHint:    idx(s.Teal),

		GitModified:  idx(s.Orange),
		GitAdded:     idx(s.Green),
		GitUntracked: idx(s.Teal),
		GitDeleted:   idx(s.Red),
		GitConflict:  idx(s.Purple),
		GitIgnored:   idx(s.Overlay0),
		GitSubmodule: idx(s.Blue),
	}
	p.BracketPairs = []Color256{idx(s.Yellow), idx(s.Pink), idx(s.Blue)}
	if s.Light {
		p.TextWhite = mix(s.Text, black, 0.45)
		p.OnAccent = idx(s.Base)
		p.BgFloat = mix(s.Base, white, 0.55)
		p.Shadow = mix(s.Crust, s.Overlay0, 0.55)
	} else {
		p.TextWhite = mix(s.Text, white, 0.5)
		p.OnAccent = idx(s.Crust)
		p.BgFloat = mix(s.Base, s.Surface0, 0.42)
		p.Shadow = mix(s.Crust, black, 0.55)
	}
	p.IndexHex = hex
	return p
}

// lightReadable adjusts a light spec so the chrome reads (contrast_test.go):
// the sidebar and panel (Mantle, Crust) are never brighter than the editor
// (Base) — upstream Rosé Pine Dawn has them brighter, which turns the panes
// inside out — and the dim text tiers and the pane divider are moved
// toward Text until they reach their contrast on every background.
func lightReadable(s Spec) Spec {
	lum := func(h string) float64 { c, _ := ParseHex(h); return relLuminance(c) }
	darker := func(h string) string { c, _ := ParseHex(h); return Lerp(c, RGB{}, 0.04).Hex() }
	if lum(s.Mantle) > lum(s.Base) {
		s.Mantle = darker(s.Base)
	}
	if lum(s.Crust) > lum(s.Mantle) {
		s.Crust = darker(s.Mantle)
	}
	reach := func(h string, min float64) string {
		for _, bg := range []string{s.Base, s.Mantle, s.Crust} {
			h = readableOn(h, bg, s.Text, min)
		}
		return h
	}
	s.Overlay0 = reach(s.Overlay0, 3.0)
	s.Overlay1 = reach(s.Overlay1, 3.0)
	s.Surface1 = reach(s.Surface1, 2.2)
	s.Surface0 = readableOn(s.Surface0, s.Mantle, s.Text, 1.15)
	return s
}

func specTheme(s Spec) NamedTheme {
	return NamedTheme{ID: s.ID, Name: s.Name, Palette: FromSpec(s), Light: s.Light}
}

// ─── Spec themes ───────────────────────────────────────────────────────
// Colours are the published upstream palettes; syntax roles follow each
// project's own Neovim port.

var catppuccinMocha = Spec{
	ID: "catppuccin-mocha", Name: "Catppuccin Mocha",
	Base: "#1e1e2e", Mantle: "#181825", Crust: "#11111b",
	Surface0: "#313244", Surface1: "#45475a", Surface2: "#585b70",
	Overlay0: "#6c7086", Overlay1: "#7f849c",
	Subtext0: "#a6adc8", Subtext1: "#bac2de", Text: "#cdd6f4",
	Cursor: "#f5e0dc", LineNr: "#b4befe",
	Accent: "#cba6f7", Selection: "#45475a",
	Red: "#f38ba8", Orange: "#fab387", Yellow: "#f9e2af", Green: "#a6e3a1",
	Teal: "#94e2d5", Cyan: "#89dceb", Blue: "#89b4fa", Purple: "#cba6f7", Pink: "#f5c2e7",
	Keyword: "#cba6f7", Control: "#cba6f7", Function: "#89b4fa", Variable: "#cdd6f4",
	Type: "#f9e2af", String: "#a6e3a1", Number: "#fab387", Comment: "#9399b2",
	Regex: "#f5c2e7", Constant: "#fab387", Operator: "#89dceb",
}

var catppuccinLatte = Spec{
	ID: "catppuccin-latte", Name: "Catppuccin Latte", Light: true,
	Base: "#eff1f5", Mantle: "#e6e9ef", Crust: "#dce0e8",
	Surface0: "#ccd0da", Surface1: "#bcc0cc", Surface2: "#acb0be",
	Overlay0: "#9ca0b0", Overlay1: "#8c8fa1",
	Subtext0: "#6c6f85", Subtext1: "#5c5f77", Text: "#4c4f69",
	Cursor: "#dc8a78", LineNr: "#7287fd",
	Accent: "#8839ef", Selection: "#ccd0da",
	Red: "#d20f39", Orange: "#fe640b", Yellow: "#df8e1d", Green: "#40a02b",
	Teal: "#179299", Cyan: "#04a5e5", Blue: "#1e66f5", Purple: "#8839ef", Pink: "#ea76cb",
	Keyword: "#8839ef", Control: "#8839ef", Function: "#1e66f5", Variable: "#4c4f69",
	Type: "#df8e1d", String: "#40a02b", Number: "#fe640b", Comment: "#7c7f93",
	Regex: "#ea76cb", Constant: "#fe640b", Operator: "#04a5e5",
}

var tokyoNight = Spec{
	ID: "tokyo-night", Name: "Tokyo Night",
	Base: "#1a1b26", Mantle: "#16161e", Crust: "#12131a",
	Surface0: "#292e42", Surface1: "#3b4261", Surface2: "#545c7e",
	Overlay0: "#565f89", Overlay1: "#737aa2",
	Subtext0: "#a9b1d6", Subtext1: "#b4bce0", Text: "#c0caf5",
	LineNr: "#737aa2",
	Accent: "#7aa2f7", Selection: "#283457",
	Red: "#f7768e", Orange: "#ff9e64", Yellow: "#e0af68", Green: "#9ece6a",
	Teal: "#73daca", Cyan: "#7dcfff", Blue: "#7aa2f7", Purple: "#bb9af7", Pink: "#ff007c",
	Keyword: "#9d7cd8", Control: "#bb9af7", Function: "#7aa2f7", Variable: "#c0caf5",
	Type: "#2ac3de", String: "#9ece6a", Number: "#ff9e64", Comment: "#565f89",
	Regex: "#b4f9f8", Constant: "#ff9e64", Operator: "#89ddff",
}

var rosePine = Spec{
	ID: "rose-pine", Name: "Rosé Pine",
	Base: "#191724", Mantle: "#1f1d2e", Crust: "#14121d",
	Surface0: "#26233a", Surface1: "#403d52", Surface2: "#524f67",
	Overlay0: "#6e6a86", Overlay1: "#908caa",
	Subtext0: "#908caa", Subtext1: "#b8b5cf", Text: "#e0def4",
	Cursor: "#524f67", LineNr: "#e0def4",
	Accent: "#c4a7e7", Selection: "#403d52",
	Red: "#eb6f92", Orange: "#ebbcba", Yellow: "#f6c177", Green: "#9ccfd8",
	Teal: "#31748f", Cyan: "#9ccfd8", Blue: "#31748f", Purple: "#c4a7e7", Pink: "#ebbcba",
	Keyword: "#31748f", Control: "#31748f", Function: "#ebbcba", Variable: "#e0def4",
	Type: "#9ccfd8", String: "#f6c177", Number: "#f6c177", Comment: "#6e6a86",
	Regex: "#c4a7e7", Constant: "#f6c177", Operator: "#908caa",
}

var rosePineDawn = Spec{
	ID: "rose-pine-dawn", Name: "Rosé Pine Dawn", Light: true,
	Base: "#faf4ed", Mantle: "#fffaf3", Crust: "#f2e9e1",
	Surface0: "#f4ede8", Surface1: "#dfdad9", Surface2: "#cecacd",
	Overlay0: "#9893a5", Overlay1: "#797593",
	Subtext0: "#797593", Subtext1: "#686583", Text: "#575279",
	Cursor: "#cecacd", LineNr: "#575279",
	Accent: "#907aa9", Selection: "#dfdad9",
	Red: "#b4637a", Orange: "#d7827e", Yellow: "#ea9d34", Green: "#56949f",
	Teal: "#286983", Cyan: "#56949f", Blue: "#286983", Purple: "#907aa9", Pink: "#d7827e",
	Keyword: "#286983", Control: "#286983", Function: "#d7827e", Variable: "#575279",
	Type: "#56949f", String: "#ea9d34", Number: "#ea9d34", Comment: "#9893a5",
	Regex: "#907aa9", Constant: "#ea9d34", Operator: "#797593",
}

// SpecThemes lists the Spec-based built-ins (registered in allThemes).
var SpecThemes = []Spec{catppuccinMocha, catppuccinLatte, tokyoNight, rosePine, rosePineDawn}
