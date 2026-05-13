package theme

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Color256 is an xterm-256 palette index (0–255). The package-level identifiers
// below are the canonical termocode palette: components reference them instead
// of hard-coding a color. Their values are no longer compile-time constants —
// they're filled in from the active Palette256 (see SetPalette / themes.go),
// so the user can switch themes at runtime without touching call sites.
type Color256 = int

// Palette256 bundles every named color used by the UI. A Theme (themes.go)
// builds one of these and hands it to SetPalette to make it active. Adding a
// new field here is the only place a new palette slot needs to be declared —
// the package-level vars are populated by setFromPalette below.
type Palette256 struct {
	// Backgrounds
	BgEditor      Color256
	BgSidebar     Color256
	BgActivityBar Color256
	BgTitleBar    Color256
	BgPanel       Color256
	BgStatusBar   Color256
	BgSelection   Color256
	BgHover       Color256
	BgActiveLine  Color256
	BgInactiveSel Color256
	BgSectionHdr  Color256
	// BgKeycap is the surface used for tiny "key cap" chips on chromeless
	// surfaces (welcome screen shortcut hints, footer help). One shade
	// lighter than BgEditor so the cap reads as a button.
	BgKeycap Color256

	// Foregrounds
	TextPrimary   Color256
	TextSecondary Color256
	TextMuted     Color256
	TextDim       Color256
	TextDimmer    Color256
	// TextQuaternary is the "very dim" tier — dimmer than TextDimmer —
	// used for sub-counters ("3 of 12") and the version/footer line on
	// the welcome screen. Sits below TextDimmer in the emphasis hierarchy.
	TextQuaternary Color256
	TextWhite      Color256
	Cursor         Color256
	ActiveLineNum  Color256

	// Accent palette (semantic) — used by chromeless surfaces (welcome
	// screen icons, file-extension tags, link text). Distinct from
	// SyntaxKeyword/etc. so theming the welcome screen doesn't require
	// repurposing a syntax slot.
	AccentBlue     Color256
	AccentGreen    Color256
	AccentAmber    Color256
	AccentRedCoral Color256
	AccentMagenta  Color256
	// AccentLavender (#bb86fc) is the unified purple accent used by chrome
	// decorations the user wants kept consistent with the recents modal:
	// menu checkmarks, file-icon tags, focus bars, link text. AccentBlue
	// remains for VSCode-style logo/badge spots; lavender replaces it
	// everywhere the recents modal sets the visual norm.
	AccentLavender Color256

	// Borders
	BorderDefault Color256
	BorderFocus   Color256
	BorderSubtle  Color256

	// Syntax
	SyntaxKeyword  Color256
	SyntaxControl  Color256
	SyntaxFunction Color256
	SyntaxVariable Color256
	SyntaxType     Color256
	SyntaxString   Color256
	SyntaxNumber   Color256
	SyntaxComment  Color256
	SyntaxRegex    Color256
	SyntaxConstant Color256
	SyntaxOperator Color256

	// Diagnostics
	DiagError   Color256
	DiagWarning Color256
	DiagInfo    Color256
	DiagHint    Color256

	// Git
	GitModified  Color256
	GitAdded     Color256
	GitUntracked Color256
	GitDeleted   Color256
	GitConflict  Color256
	GitIgnored   Color256
	GitSubmodule Color256

	// Bracket-pair colorization
	BracketPairs []Color256

	// IndexHex maps every Color256 used by this palette to a truecolor hex
	// string. Themes that introduce new indices add entries here so Hex() can
	// emit the precise RGB; entries missing from the map fall back to
	// "color<N>" (lipgloss's generic 256-color name).
	IndexHex map[Color256]string
}

// ─── Backgrounds ───────────────────────────────────────────────────────
var (
	BgEditor      Color256 = 234
	BgSidebar     Color256 = 236
	BgActivityBar Color256 = 237
	BgTitleBar    Color256 = 237
	BgPanel       Color256 = 233
	BgStatusBar   Color256 = 32
	BgSelection   Color256 = 24
	BgHover       Color256 = 235
	BgActiveLine  Color256 = 235
	BgInactiveSel Color256 = 237
	BgSectionHdr  Color256 = 237
	BgKeycap      Color256 = 236
)

// ─── Foregrounds ───────────────────────────────────────────────────────
var (
	TextPrimary    Color256 = 252
	TextSecondary  Color256 = 250
	TextMuted      Color256 = 102
	TextDim        Color256 = 242
	TextDimmer     Color256 = 240
	TextQuaternary Color256 = 238
	TextWhite      Color256 = 231
	Cursor         Color256 = 145
	ActiveLineNum  Color256 = 251
)

// ─── Accent palette (semantic) ─────────────────────────────────────────
var (
	AccentBlue     Color256 = 39  // #00afff
	AccentGreen    Color256 = 78  // #5fd787
	AccentAmber    Color256 = 178 // #d7af00
	AccentRedCoral Color256 = 203 // #ff5f5f
	AccentMagenta  Color256 = 170 // #d75fd7
	AccentLavender Color256 = 141 // #bb86fc (256 idx 141 ≈ #af87ff; truecolor exact)
)

// ─── Borders ───────────────────────────────────────────────────────────
var (
	BorderDefault Color256 = 238
	BorderFocus   Color256 = 32
	BorderSubtle  Color256 = 237
)

// ─── Syntax ────────────────────────────────────────────────────────────
var (
	SyntaxKeyword  Color256 = 74
	SyntaxControl  Color256 = 175
	SyntaxFunction Color256 = 187
	SyntaxVariable Color256 = 153
	SyntaxType     Color256 = 79
	SyntaxString   Color256 = 174
	SyntaxNumber   Color256 = 151
	SyntaxComment  Color256 = 65
	SyntaxRegex    Color256 = 167
	SyntaxConstant Color256 = 75
	SyntaxOperator Color256 = 252
)

// ─── Diagnostics ───────────────────────────────────────────────────────
var (
	DiagError   Color256 = 203
	DiagWarning Color256 = 178
	DiagInfo    Color256 = 111
	DiagHint    Color256 = 102
)

// ─── Git ───────────────────────────────────────────────────────────────
var (
	GitModified  Color256 = 180
	GitAdded     Color256 = 78
	GitUntracked Color256 = 78
	GitDeleted   Color256 = 167
	GitConflict  Color256 = 62
	GitIgnored   Color256 = 245
	GitSubmodule Color256 = 110
)

// BracketPairs are the rotating colors used by bracket-pair colorization.
var BracketPairs = []Color256{
	220,
	170,
	39,
}

// indexHex maps every Color256 currently in use to a truecolor hex string.
// SetPalette replaces this map atomically when the user switches themes.
var indexHex = defaultIndexHex()

func defaultIndexHex() map[Color256]string {
	return map[Color256]string{
		0:   "#000000",
		24:  "#005f87",
		32:  "#0087d7",
		39:  "#00afff",
		62:  "#5f5fd7",
		65:  "#5f875f",
		74:  "#5fafd7",
		75:  "#5fafff",
		78:  "#5fd787",
		79:  "#5fd7af",
		102: "#878787",
		110: "#87afd7",
		111: "#87afff",
		141: "#bb86fc",
		145: "#afafaf",
		151: "#afd7af",
		153: "#afd7ff",
		167: "#d75f5f",
		170: "#d75fd7",
		174: "#d78787",
		175: "#d787af",
		178: "#d7af00",
		180: "#d7af87",
		187: "#d7d7af",
		203: "#ff5f5f",
		220: "#ffd700",
		231: "#ffffff",
		233: "#121212",
		234: "#1c1c1c",
		235: "#262626",
		236: "#303030",
		237: "#3a3a3a",
		238: "#444444",
		240: "#585858",
		242: "#6c6c6c",
		245: "#8a8a8a",
		250: "#bcbcbc",
		251: "#c6c6c6",
		252: "#d0d0d0",
	}
}

// SetPalette swaps the active palette: every package-level identifier above
// (BgEditor, TextPrimary, etc.) is updated in-place so existing callers like
// theme.Bg(theme.BgEditor) automatically pick up the new theme on their next
// call. The view layer must trigger a re-render after this returns.
func SetPalette(p Palette256) {
	BgEditor = p.BgEditor
	BgSidebar = p.BgSidebar
	BgActivityBar = p.BgActivityBar
	BgTitleBar = p.BgTitleBar
	BgPanel = p.BgPanel
	BgStatusBar = p.BgStatusBar
	BgSelection = p.BgSelection
	BgHover = p.BgHover
	BgActiveLine = p.BgActiveLine
	BgInactiveSel = p.BgInactiveSel
	BgSectionHdr = p.BgSectionHdr
	if p.BgKeycap != 0 {
		BgKeycap = p.BgKeycap
	}

	TextPrimary = p.TextPrimary
	TextSecondary = p.TextSecondary
	TextMuted = p.TextMuted
	TextDim = p.TextDim
	TextDimmer = p.TextDimmer
	if p.TextQuaternary != 0 {
		TextQuaternary = p.TextQuaternary
	}
	TextWhite = p.TextWhite
	Cursor = p.Cursor
	ActiveLineNum = p.ActiveLineNum

	if p.AccentBlue != 0 {
		AccentBlue = p.AccentBlue
	}
	if p.AccentGreen != 0 {
		AccentGreen = p.AccentGreen
	}
	if p.AccentAmber != 0 {
		AccentAmber = p.AccentAmber
	}
	if p.AccentRedCoral != 0 {
		AccentRedCoral = p.AccentRedCoral
	}
	if p.AccentMagenta != 0 {
		AccentMagenta = p.AccentMagenta
	}
	if p.AccentLavender != 0 {
		AccentLavender = p.AccentLavender
	}

	BorderDefault = p.BorderDefault
	BorderFocus = p.BorderFocus
	BorderSubtle = p.BorderSubtle

	SyntaxKeyword = p.SyntaxKeyword
	SyntaxControl = p.SyntaxControl
	SyntaxFunction = p.SyntaxFunction
	SyntaxVariable = p.SyntaxVariable
	SyntaxType = p.SyntaxType
	SyntaxString = p.SyntaxString
	SyntaxNumber = p.SyntaxNumber
	SyntaxComment = p.SyntaxComment
	SyntaxRegex = p.SyntaxRegex
	SyntaxConstant = p.SyntaxConstant
	SyntaxOperator = p.SyntaxOperator

	DiagError = p.DiagError
	DiagWarning = p.DiagWarning
	DiagInfo = p.DiagInfo
	DiagHint = p.DiagHint

	GitModified = p.GitModified
	GitAdded = p.GitAdded
	GitUntracked = p.GitUntracked
	GitDeleted = p.GitDeleted
	GitConflict = p.GitConflict
	GitIgnored = p.GitIgnored
	GitSubmodule = p.GitSubmodule

	if len(p.BracketPairs) > 0 {
		BracketPairs = append(BracketPairs[:0], p.BracketPairs...)
	}
	if p.IndexHex != nil {
		indexHex = p.IndexHex
	}
}

// Hex returns the truecolor hex string for a 256-index. Falls back to the
// generic "color<N>" name lipgloss accepts when the index isn't in the map.
func Hex(c Color256) string {
	if v, ok := indexHex[c]; ok {
		return v
	}
	return fmt.Sprintf("color%d", c)
}

// LG turns a Color256 into a lipgloss.Color (truecolor where mapped).
func LG(c Color256) lipgloss.Color {
	return lipgloss.Color(Hex(c))
}

// Fg returns a lipgloss.Style with foreground set.
func Fg(c Color256) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(LG(c))
}

// Bg returns a lipgloss.Style with background set.
func Bg(c Color256) lipgloss.Style {
	return lipgloss.NewStyle().Background(LG(c))
}

// FgBg returns a style with both fore- and background.
func FgBg(fg, bg Color256) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(LG(fg)).Background(LG(bg))
}
