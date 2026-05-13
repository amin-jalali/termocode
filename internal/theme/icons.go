package theme

import (
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Icon carries three glyph variants and renders the one matching CurrentIconMode.
//
// Bold is an optional 2-row × 2-cell hand-crafted block. When a caller
// (e.g. the activity bar) has a 2×2 cell area to fill it can call
// BoldRender() to get the larger artwork; otherwise String() returns the
// single-cell glyph as before.
type Icon struct {
	NerdFont string    // codepoint that exists in Nerd Font patches
	Unicode  string    // basic Unicode glyph that renders in any monospace
	ASCII    string    // pure ASCII fallback for picky terminals
	Bold     [2]string // 2-row × 2-cell variant; each row exactly 2 visible cells
}

// Activity-bar icons. NerdFont codepoints are Font Awesome 4.x glyphs.
// FA glyphs render at a more uniform size than codicons across the
// common Nerd Font patches.
//
// Curated Unicode tier — chosen for being:
//   1. Geometric BMP glyphs (U+2200–U+27BF range, mostly).
//   2. Reported as east-asian-width=Neutral by Unicode (i.e. 1 cell on
//      every modern terminal that respects EAW, including kitty,
//      wezterm, ghostty, alacritty, iTerm2).
//   3. Visually iconic enough to convey the action without text.
//
// Bold tier — hand-crafted 2-row × 2-cell glyph blocks built strictly
// from the Unicode block-element set at U+2580–U+259F (single quadrants,
// half blocks, three-quadrants, diagonals, full block) plus regular
// space. Restricting the alphabet to one Unicode block makes the four
// cells "puzzle-fit" into a coherent silhouette — geometric circles or
// triangles can't be formed cleanly with quadrants alone, so each icon
// is an evocative shape rather than a literal render. Every row is
// exactly 2 cells wide on every modern terminal, regardless of icon
// mode. Callers (e.g. the activity bar) opt into the bold artwork via
// Icon.BoldRender().
//
// At runtime we still empirically probe each single-cell glyph (see
// Probe()) — the curated list is the candidate set, not the final answer.
var (
	// IconFiles — squared-with-horizontal-fill reads as a "file with lines"
	// in most monospace fonts. U+25A4 is in EAW=Neutral, 1 cell.
	// Bold: tabbed folder — small tab notch at the top (▗▄), solid
	// folder body below (▟█).
	IconFiles = Icon{
		NerdFont: "", Unicode: "▤", ASCII: "F", // fa-folder, ▤
		Bold: [2]string{"▗▄", "▟█"},
	}
	// IconSearch — telephone-recorder doubles as a magnifier outline at
	// monospace sizes. U+2315, EAW=Neutral.
	// Bold: rounded lens (▛▜) on top, lens body + handle dot (▙▖) below.
	IconSearch = Icon{
		NerdFont: "", Unicode: "⌕", ASCII: "S", // fa-search, ⌕
		Bold: [2]string{"▛▜", "▙▖"},
	}
	// IconGit — alternative-key symbol is a fork glyph used by some
	// editors for branch markers. U+2387, EAW=Neutral. Falls back to
	// U+2442 (ocr-branch) only if 2387 fails the probe.
	// Bold: two dots (▖▗) converging into solid wedges (▟▙) = merge / fork.
	IconGit = Icon{
		NerdFont: "", Unicode: "⎇", ASCII: "G", // fa-code-fork, ⎇
		Bold: [2]string{"▖▗", "▟▙"},
	}
	// IconOutline — identical-to ("hamburger menu"). U+2261, EAW=Neutral.
	// Bold: two heavy horizontal bars stacked = hamburger/list.
	IconOutline = Icon{
		NerdFont: "", Unicode: "≡", ASCII: "O", // fa-list, ≡
		Bold: [2]string{"▀▀", "▄▄"},
	}
	// IconRun — black right-pointing triangle. U+25B6, EAW=Neutral
	// (note: U+25B6 + VS16 becomes emoji-presentation; we send the
	// bare codepoint to keep it text-presentation = 1 cell).
	// Bold: triangle silhouette pointing right (▙ on top, ▛ on bottom,
	// blank right column). Pure quadrant blocks can't form a clean
	// triangle tip, so this is an evocative right-pointing wedge rather
	// than a literal play glyph.
	IconRun = Icon{
		NerdFont: "", Unicode: "▶", ASCII: "R", // fa-play, ▶
		Bold: [2]string{"▙ ", "▛ "},
	}
	// IconDebug — fisheye, reads as a "bug eye". U+25C9, EAW=Neutral.
	// Bold: solid bug body (▟▙) on top, two small leg/foot dots (▘▝) below.
	IconDebug = Icon{
		NerdFont: "", Unicode: "◉", ASCII: "D", // fa-bug, ◉
		Bold: [2]string{"▟▙", "▘▝"},
	}
	// IconExtensions — squared-plus, evokes a puzzle/add-piece. U+229E.
	// Bold: 4 three-quadrant blocks forming a cross-divided square /
	// puzzle module (▛▜ on top, ▙▟ on bottom).
	IconExtensions = Icon{
		NerdFont: "", Unicode: "⊞", ASCII: "X", // fa-puzzle-piece, ⊞
		Bold: [2]string{"▛▜", "▙▟"},
	}
	// IconTerminal — white-square-containing-black-small-square reads as
	// a console prompt. U+25A3, EAW=Neutral.
	// Bold: chevron tip (▙ + space) on top, underscore bar (▀▀) below = `>_`.
	IconTerminal = Icon{
		NerdFont: "", Unicode: "▣", ASCII: "T", // fa-terminal, ▣
		Bold: [2]string{"▙ ", "▀▀"},
	}
	// IconAccount — bullseye, evokes a user avatar circle. U+25CE.
	// Bold: head + shoulders silhouette via swapped 3-quadrants — narrow
	// at top (▟▙ = tucked-in head), wide at bottom (▛▜ = shoulders).
	// Differentiated from IconExtensions (▛▜/▙▟) by inversion.
	IconAccount = Icon{
		NerdFont: "", Unicode: "◎", ASCII: "U", // fa-user, ◎
		Bold: [2]string{"▟▙", "▛▜"},
	}
	// IconSettings — gear. U+2699 is the riskiest single glyph because
	// some fonts ship it only with VS16 emoji presentation, which can
	// render 2-cell. The probe will catch that and fall back to ASCII.
	// Bold: four corner pixels (▘▝ / ▖▗) = stylised gear teeth around hub.
	IconSettings = Icon{
		NerdFont: "", Unicode: "⚙", ASCII: "*", // fa-cog, ⚙
		Bold: [2]string{"▘▝", "▖▗"},
	}
)

// File-tree icons.
var (
	IconFolderClosed = Icon{NerdFont: "", Unicode: "▶", ASCII: ">"}
	IconFolderOpen   = Icon{NerdFont: "", Unicode: "▼", ASCII: "v"}
	IconFile         = Icon{NerdFont: "", Unicode: "·", ASCII: " "}
)

// Status-bar markers.
var (
	IconBranch  = Icon{NerdFont: "", Unicode: "⑂", ASCII: "git:"}
	IconError   = Icon{NerdFont: "", Unicode: "✗", ASCII: "E"}
	IconWarning = Icon{NerdFont: "", Unicode: "⚠", ASCII: "W"}
	IconDirty   = Icon{NerdFont: "●", Unicode: "●", ASCII: "*"}
)

// IconMode selects which variant Icon.String returns.
type IconMode int

const (
	IconModeNerdFont IconMode = iota
	IconModeUnicode
	IconModeASCII
)

// CurrentIconMode is read by Icon.String. Set it once at startup.
//
// Defaults to ASCII — the user reported the curated Unicode + 2×2 quadrant
// experiment looked broken on their setup. Plain ASCII letters render
// identically on every terminal and font, so we lock the default there.
// Users who want richer icons can opt in via TERMOCODE_ICON_MODE=unicode
// or TERMOCODE_ICON_MODE=nerd_font.
var CurrentIconMode IconMode = IconModeASCII

func (i Icon) String() string {
	switch CurrentIconMode {
	case IconModeNerdFont:
		return i.NerdFont
	case IconModeUnicode:
		return i.Unicode
	default:
		return i.ASCII
	}
}

// BoldRender returns the icon's 2-row × 2-cell variant.
//
// If the Icon has hand-crafted Bold artwork (either row non-empty), that
// artwork is returned verbatim — the caller is responsible for ensuring
// each row is exactly 2 visible cells (we ship every activity-bar icon
// with a 2-cell-wide Bold by hand, see the var block above).
//
// Fallback when no Bold is defined: render the active 1-cell glyph in
// the lower row, padded to 2 cells, with a blank upper row. Empty glyph
// (e.g. NerdFont mode with no codepoint) collapses to two blank rows.
func (i Icon) BoldRender() [2]string {
	// In ASCII mode the user explicitly opted out of Unicode artwork —
	// don't return the quadrant-block Bold even if it's defined. Fall
	// straight to the centered ASCII letter.
	if CurrentIconMode != IconModeASCII && (i.Bold[0] != "" || i.Bold[1] != "") {
		return i.Bold
	}
	g := i.String()
	if g == "" {
		return [2]string{"  ", "  "}
	}
	return [2]string{"  ", g + " "}
}

// envOverrideMode returns (mode, true) if TERMOCODE_ICON_MODE pins the
// answer. Honours the legacy TERMOCODE_NERD_FONT=1 toggle.
func envOverrideMode() (IconMode, bool) {
	if v := os.Getenv("TERMOCODE_ICON_MODE"); v != "" {
		switch strings.ToLower(v) {
		case "nerd", "nerd_font", "nerdfont":
			return IconModeNerdFont, true
		case "unicode":
			return IconModeUnicode, true
		case "ascii":
			return IconModeASCII, true
		}
	}
	if os.Getenv("TERMOCODE_NERD_FONT") == "1" {
		return IconModeNerdFont, true
	}
	return IconModeASCII, false
}

// DetectIconMode reads environment hints to guess the safest fallback.
//   - TERMOCODE_ICON_MODE wins if set: "nerd_font" | "unicode" | "ascii".
//   - Otherwise ASCII — plain letters render identically everywhere and
//     dodge the 2-cell Unicode glitches the user reported. Pretty icons
//     are now strictly opt-in via the env vars above.
func DetectIconMode() IconMode {
	if m, ok := envOverrideMode(); ok {
		return m
	}
	return IconModeASCII
}

// Probe runs the cursor-position probe against stdin/stdout and returns
// the safest IconMode for the current terminal.
//
// Fallback semantics:
//   - TERMOCODE_ICON_MODE / TERMOCODE_NERD_FONT win immediately.
//   - Otherwise ASCII (the user reported the Unicode glyphs looked
//     broken on their setup, so we don't auto-promote — pretty icons
//     are strictly opt-in via the env vars above).
//
// The CSI-6n probe code below is preserved for future re-enablement,
// but currently short-circuits at the env-override step.
//
// Protocol: for each candidate glyph we
//  1. ask the terminal for the current cursor column via CSI 6n,
//  2. write the glyph,
//  3. ask again,
//  4. compute the cell delta. delta==1 means the glyph rendered as
//     a single cell on the user's actual terminal/font combo.
//
// All probe output is hidden behind DECSC/DECRC (\x1b7 / \x1b8) and a
// trailing CSI K erase, so the user sees no flicker.
//
// Total wall-clock budget is ProbeDeadline (200 ms). Past that we
// fall back to ASCII regardless of how many glyphs were checked.
func Probe() IconMode {
	if m, ok := envOverrideMode(); ok {
		return m
	}
	// Default locked to ASCII per user request — Unicode / Nerd Font is
	// strictly opt-in via TERMOCODE_ICON_MODE. The CSI-6n probe
	// infrastructure (probeGlyph, parseCPR, etc.) is still compiled in
	// for future re-enablement.
	return IconModeASCII
}

// ProbeDeadline caps the wall-clock budget for the whole probe.
// Exposed for the test suite.
var ProbeDeadline = 200 * time.Millisecond

// unicodeCandidates returns every glyph that would actually be rendered
// by the activity bar in Unicode mode. Tier passes only if all of these
// probe as exactly 1 cell.
func unicodeCandidates() []string {
	icons := []Icon{
		IconFiles, IconSearch, IconGit, IconOutline, IconRun,
		IconDebug, IconExtensions, IconTerminal, IconAccount, IconSettings,
	}
	out := make([]string, 0, len(icons))
	for _, ic := range icons {
		if ic.Unicode != "" {
			out = append(out, ic.Unicode)
		}
	}
	return out
}

// probeStreams returns the (stdin, stdout) pair the probe should use, or
// ok=false if either side isn't a TTY (no CSI 6n response possible).
func probeStreams() (*os.File, *os.File, bool) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, nil, false
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, nil, false
	}
	return os.Stdin, os.Stdout, true
}

// probeGlyph writes one glyph between two cursor-position queries and
// returns the cell-width delta. Bails out (ok=false) if the deadline
// elapses or the terminal stops responding.
func probeGlyph(in *os.File, out *os.File, glyph string, deadline time.Time) (int, bool) {
	// Move to column 1 of the current row so the start col is known
	// even on a terminal that wrapped previous output. We saved the
	// real cursor with DECSC in the caller.
	if _, err := out.WriteString("\r"); err != nil {
		return 0, false
	}
	startCol, ok := readCursorCol(in, out, deadline)
	if !ok {
		return 0, false
	}
	if _, err := out.WriteString(glyph); err != nil {
		return 0, false
	}
	endCol, ok := readCursorCol(in, out, deadline)
	if !ok {
		return 0, false
	}
	// Roll back to column 1 so the next probe starts clean.
	_, _ = out.WriteString("\r\x1b[2K")
	return endCol - startCol, true
}

// readCursorCol writes CSI 6n and parses the \x1b[<row>;<col>R reply,
// returning the 1-based column. ok=false on timeout / bad input.
func readCursorCol(in *os.File, out *os.File, deadline time.Time) (int, bool) {
	if _, err := out.WriteString("\x1b[6n"); err != nil {
		return 0, false
	}
	_, col, err := readCPR(in, deadline)
	if err != nil {
		return 0, false
	}
	return col, true
}

// readCPR reads a Cursor-Position-Report response from r. The response
// is `\x1b[<row>;<col>R`. Times out (deadline) returning an error. The
// reader must be in raw mode or the bytes won't arrive until newline.
func readCPR(r *os.File, deadline time.Time) (int, int, error) {
	var buf [32]byte
	idx := 0
	// Step the parser through: ESC, '[', digits, ';', digits, 'R'.
	// Bail when the whole response is in or the deadline passes.
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return 0, 0, errors.New("probe deadline")
		}
		if err := r.SetReadDeadline(time.Now().Add(left)); err != nil {
			// Some platforms don't support SetReadDeadline on a TTY.
			// Without a deadline we'd hang forever, so bail.
			return 0, 0, err
		}
		var b [1]byte
		n, err := r.Read(b[:])
		if err != nil || n == 0 {
			return 0, 0, errors.New("probe read")
		}
		if idx < len(buf) {
			buf[idx] = b[0]
			idx++
		}
		if b[0] == 'R' {
			break
		}
	}
	return parseCPR(string(buf[:idx]))
}

// parseCPR parses a Cursor-Position-Report payload: it must contain
// "\x1b[<row>;<col>R" anywhere inside (some terminals interleave noise
// before the response). Returns row, col, error. Exposed for tests.
func parseCPR(s string) (int, int, error) {
	// Find the last ESC[ in the buffer — earlier bytes might be noise
	// (e.g. a stale keystroke the user mashed during startup).
	i := strings.LastIndex(s, "\x1b[")
	if i < 0 {
		return 0, 0, errors.New("no CSI in CPR")
	}
	body := s[i+2:]
	end := strings.IndexByte(body, 'R')
	if end < 0 {
		return 0, 0, errors.New("no R terminator")
	}
	body = body[:end]
	semi := strings.IndexByte(body, ';')
	if semi < 0 {
		return 0, 0, errors.New("no ; in CPR")
	}
	row, err := atoiStrict(body[:semi])
	if err != nil {
		return 0, 0, err
	}
	col, err := atoiStrict(body[semi+1:])
	if err != nil {
		return 0, 0, err
	}
	return row, col, nil
}

// atoiStrict parses a non-empty run of ASCII digits.
func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty number")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("non-digit")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
