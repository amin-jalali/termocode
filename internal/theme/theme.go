package theme

import "github.com/charmbracelet/lipgloss"

// Theme holds the lipgloss styles and palette used across termocode,
// modeled on VSCode's "Dark+" theme.
type Theme struct {
	// Palette
	Bg          lipgloss.Color
	Fg          lipgloss.Color
	BgPanel     lipgloss.Color
	BgEditor    lipgloss.Color
	BgActive    lipgloss.Color
	FgMuted     lipgloss.Color
	FgDim       lipgloss.Color
	Accent      lipgloss.Color
	Border      lipgloss.Color
	BorderFocus lipgloss.Color

	// Composite styles
	Separator    lipgloss.Style
	Gutter       lipgloss.Style
	GutterActive lipgloss.Style
	IndentGuide  lipgloss.Style
	StatusBar    lipgloss.Style
	StatusKey    lipgloss.Style
	StatusValue  lipgloss.Style
	StatusDirty  lipgloss.Style
	StatusError  lipgloss.Style
	Muted        lipgloss.Style

	// Cursor (block) on focused/unfocused panes (used by explorer)
	CursorFocused   lipgloss.Style
	CursorUnfocused lipgloss.Style
}

func DarkPlus() Theme {
	bg := lipgloss.Color("#1c1c1c")
	fg := lipgloss.Color("#d0d0d0")
	bgPanel := lipgloss.Color("#262626")
	bgActive := lipgloss.Color("#2a2d2e")
	fgMuted := lipgloss.Color("#858585")
	fgDim := lipgloss.Color("#3a3a3a")
	accent := lipgloss.Color("#0087d7")
	border := lipgloss.Color("#3a3a3a")

	return Theme{
		Bg:          bg,
		Fg:          fg,
		BgPanel:     bgPanel,
		BgEditor:    bg,
		BgActive:    bgActive,
		FgMuted:     fgMuted,
		FgDim:       fgDim,
		Accent:      accent,
		Border:      border,
		BorderFocus: accent,

		Separator: lipgloss.NewStyle().
			Foreground(border),
		Gutter: lipgloss.NewStyle().
			Foreground(fgMuted),
		GutterActive: lipgloss.NewStyle().
			Foreground(fg).Bold(true),
		IndentGuide: lipgloss.NewStyle().
			Foreground(fgDim),

		StatusBar: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffffff")),
		StatusKey: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#cce6f4")),
		StatusValue: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffffff")).Bold(true),
		StatusDirty: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffd866")).Bold(true),
		StatusError: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ff8888")).Bold(true),
		Muted: lipgloss.NewStyle().
			Foreground(fgMuted),

		CursorFocused: lipgloss.NewStyle().
			Background(accent).Foreground(lipgloss.Color("#ffffff")),
		CursorUnfocused: lipgloss.NewStyle().
			Background(bgActive).Foreground(fg),
	}
}

// Default returns the default theme (Dark+).
func Default() Theme {
	return DarkPlus()
}

// GitHubDark returns the composite styles paired with the GitHub Dark palette.
// The shape mirrors DarkPlus so swapping themes is a drop-in replacement.
func GitHubDark() Theme {
	bg := lipgloss.Color("#0d1117")
	fg := lipgloss.Color("#c9d1d9")
	bgPanel := lipgloss.Color("#161b22")
	bgActive := lipgloss.Color("#1f242c")
	fgMuted := lipgloss.Color("#8b949e")
	fgDim := lipgloss.Color("#30363d")
	accent := lipgloss.Color("#1f6feb")
	border := lipgloss.Color("#30363d")
	return buildTheme(bg, fg, bgPanel, bgActive, fgMuted, fgDim, accent, border)
}

// OneDark returns the composite styles paired with the One Dark palette.
func OneDark() Theme {
	bg := lipgloss.Color("#282c34")
	fg := lipgloss.Color("#abb2bf")
	bgPanel := lipgloss.Color("#21252b")
	bgActive := lipgloss.Color("#2c313a")
	fgMuted := lipgloss.Color("#5c6370")
	fgDim := lipgloss.Color("#3e4451")
	accent := lipgloss.Color("#61afef")
	border := lipgloss.Color("#3e4451")
	return buildTheme(bg, fg, bgPanel, bgActive, fgMuted, fgDim, accent, border)
}

// SolarizedDark returns the composite styles paired with the Solarized Dark
// palette.
func SolarizedDark() Theme {
	bg := lipgloss.Color("#002b36")
	fg := lipgloss.Color("#839496")
	bgPanel := lipgloss.Color("#073642")
	bgActive := lipgloss.Color("#0a4452")
	fgMuted := lipgloss.Color("#586e75")
	fgDim := lipgloss.Color("#073642")
	accent := lipgloss.Color("#268bd2")
	border := lipgloss.Color("#073642")
	return buildTheme(bg, fg, bgPanel, bgActive, fgMuted, fgDim, accent, border)
}

// buildTheme assembles the composite styles in the same shape DarkPlus does,
// so the only per-theme variation is the eight base colors fed in.
func buildTheme(bg, fg, bgPanel, bgActive, fgMuted, fgDim, accent, border lipgloss.Color) Theme {
	return Theme{
		Bg:          bg,
		Fg:          fg,
		BgPanel:     bgPanel,
		BgEditor:    bg,
		BgActive:    bgActive,
		FgMuted:     fgMuted,
		FgDim:       fgDim,
		Accent:      accent,
		Border:      border,
		BorderFocus: accent,

		Separator:    lipgloss.NewStyle().Foreground(border),
		Gutter:       lipgloss.NewStyle().Foreground(fgMuted),
		GutterActive: lipgloss.NewStyle().Foreground(fg).Bold(true),
		IndentGuide:  lipgloss.NewStyle().Foreground(fgDim),

		StatusBar: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffffff")),
		StatusKey: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#cce6f4")),
		StatusValue: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffffff")).Bold(true),
		StatusDirty: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ffd866")).Bold(true),
		StatusError: lipgloss.NewStyle().
			Background(accent).
			Foreground(lipgloss.Color("#ff8888")).Bold(true),
		Muted: lipgloss.NewStyle().Foreground(fgMuted),

		CursorFocused: lipgloss.NewStyle().
			Background(accent).Foreground(lipgloss.Color("#ffffff")),
		CursorUnfocused: lipgloss.NewStyle().
			Background(bgActive).Foreground(fg),
	}
}
