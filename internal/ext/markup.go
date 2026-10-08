package ext

import "strings"

// ── Panel / status markup ────────────────────────────────────────────────
//
// Extensions never emit raw ANSI. They style text with a tiny tag markup
// whose names map to theme tokens, so every extension follows the user's
// colour theme:
//
//	"{{accent bold}}TODO{{/}} main.go:12  {{muted}}fix this{{/}}"
//
// Rules:
//   - "{{names}}" opens a style; names are space-separated Tokens.
//   - "{{/}}" closes the most recent open style (styles nest).
//   - A tag with ANY unknown name is not a tag: it stays as literal text.
//     So "{{token}}" in an HTTP template prints as-is.
//   - "{{{{" is a literal "{{" (termocode.escape(s) does this for you).
//   - Unclosed styles end at the end of the line.

// Tokens lists every style name the markup understands. Colour names map
// to theme palette entries (render.go, tokenColor); the last four are
// text attributes.
var Tokens = []string{
	// text
	"primary", "secondary", "muted", "dim", "white",
	// accents
	"accent", "blue", "green", "amber", "yellow", "red", "magenta", "lavender",
	// status
	"error", "warning", "info", "hint", "success",
	// syntax
	"keyword", "function", "string", "number", "type", "comment", "constant", "variable",
	// git
	"added", "modified", "deleted", "untracked",
	// attributes
	"bold", "italic", "underline", "faint",
}

var tokenSet = func() map[string]bool {
	m := make(map[string]bool, len(Tokens))
	for _, t := range Tokens {
		m[t] = true
	}
	return m
}()

// IsToken reports whether name is a known markup token.
func IsToken(name string) bool { return tokenSet[name] }

// Span is a run of text with the style names active over it (outermost
// first). Styles is nil for plain text.
type Span struct {
	Text   string
	Styles []string
}

// ParseMarkup splits one line into styled spans. Adjacent text with the
// same style stack is merged. Control characters (including ESC) are
// dropped so an extension can never inject terminal escapes.
func ParseMarkup(line string) []Span {
	var (
		out   []Span
		stack [][]string
		buf   strings.Builder
	)
	active := func() []string {
		var all []string
		for _, s := range stack {
			all = append(all, s...)
		}
		return all
	}
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		st := active()
		text := buf.String()
		buf.Reset()
		if n := len(out); n > 0 && sameStyles(out[n-1].Styles, st) {
			out[n-1].Text += text
			return
		}
		out = append(out, Span{Text: text, Styles: st})
	}
	i := 0
	for i < len(line) {
		if strings.HasPrefix(line[i:], "{{{{") {
			buf.WriteString("{{")
			i += 4
			continue
		}
		if strings.HasPrefix(line[i:], "{{") {
			end := strings.Index(line[i+2:], "}}")
			if end >= 0 {
				inner := strings.TrimSpace(line[i+2 : i+2+end])
				if inner == "/" {
					flush()
					if len(stack) > 0 {
						stack = stack[:len(stack)-1]
					}
					i += end + 4
					continue
				}
				if names := strings.Fields(inner); len(names) > 0 && allTokens(names) {
					flush()
					stack = append(stack, names)
					i += end + 4
					continue
				}
			}
		}
		c := line[i]
		switch {
		case c == '\t':
			buf.WriteString("    ")
		case c < 0x20 || c == 0x7f:
			// drop control characters (ESC, CR, …)
		default:
			buf.WriteByte(c)
		}
		i++
	}
	flush()
	return out
}

// PlainText returns the line with all markup tags removed.
func PlainText(line string) string {
	var b strings.Builder
	for _, s := range ParseMarkup(line) {
		b.WriteString(s.Text)
	}
	return b.String()
}

func allTokens(names []string) bool {
	for _, n := range names {
		if !tokenSet[n] {
			return false
		}
	}
	return true
}

func sameStyles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
