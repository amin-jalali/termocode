package webproto

// Terminal colours (WEB-40, web-ui-design §2.5): best effort, for the rows
// the laptop shows on its screen right now. They are read off the
// laptop's own screen (never from the shell's bytes: no escape sequence
// reaches the browser) and only for rows whose text still matches the
// buffer. A span is [row, c0, c1, s]: buffer row, UTF-16 columns of that
// row, and an index into Styles.

// TermColorsRes is the result of term.colors. Shown is false when the
// terminal is not on the laptop's screen (no colours then).
type TermColorsRes struct {
	Tick   int         `json:"tick"`
	Shown  bool        `json:"shown"`
	Styles []TermStyle `json:"styles"`
	Spans  [][]int     `json:"spans"`
}

// TermStyle is one look: colours as "#rrggbb" ("" is the page's own),
// bold, italic, underline, strikethrough.
type TermStyle struct {
	Fg string `json:"fg,omitempty"`
	Bg string `json:"bg,omitempty"`
	B  bool   `json:"b,omitempty"`
	I  bool   `json:"i,omitempty"`
	U  bool   `json:"u,omitempty"`
	S  bool   `json:"s,omitempty"`
}

// MaxTermStyles caps the looks of one answer; MaxTermSpans its spans.
const (
	MaxTermStyles = 256
	MaxTermSpans  = 20000
)

func init() {
	registerResult(OpTermColors, TermColorsRes{})
}
