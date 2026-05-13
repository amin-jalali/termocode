package app

import (
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"

	"termocode/internal/toast"
)

// strPtr / boolPtr — glamour's StyleConfig uses pointer fields for
// optional values so it can distinguish "unset" from "zero". One-letter
// helpers keep the giant style table below readable.
func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }

// Palette anchors. We expand on the basic VSCode Dark+ scheme with a
// handful of richer accents so the rendered preview reads as colourful
// rather than mostly grey-on-blue.
const (
	editorBg    = "#1c1c1c" // matches the editor pane bg
	codeBg      = "#23262c" // softer than tab bg so it's tinted, not loud
	codeFg      = "#d7ba7d" // warm tan — readable but doesn't shout
	codeBlockBg = "#1f2227" // slightly cooler for fenced blocks
	quoteBg     = "#23232a" // subtle violet tint for block quotes
	hrFg        = "#3a3a3a"
	mutedFg     = "#9090a0"
)

// vscodeDarkPlusGlamourStyle is a glamour StyleConfig that mirrors the
// VSCode Dark+ palette we already feed to Neovim (see theme_lua.go).
// Every element sets BackgroundColor explicitly because glamour only
// honours the parent block's bg on cells *it* writes — anything missed
// shows the host terminal's default background, which is what made the
// preview look blotchy until we fixed it.
var vscodeDarkPlusGlamourStyle = ansi.StyleConfig{
	Document: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     "\n",
			BlockSuffix:     "\n",
			Color:           strPtr("#d0d0d0"),
			BackgroundColor: strPtr(editorBg),
		},
		Margin: uintPtr(1), // tighter — every column matters in a TUI
	},
	BlockQuote: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#b5cea8"),
			BackgroundColor: strPtr(quoteBg),
			Italic:          boolPtr(true),
		},
		Indent:      uintPtr(1),
		IndentToken: strPtr("┃ "),
	},
	Paragraph: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#d0d0d0"),
			BackgroundColor: strPtr(editorBg),
		},
	},
	List: ansi.StyleList{
		LevelIndent: 2,
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           strPtr("#d0d0d0"),
				BackgroundColor: strPtr(editorBg),
			},
		},
	},
	// Heading is the *base* style every Hn inherits. We override the
	// per-level prefix / colour individually below. BlockPrefix is the
	// block-leading text glamour emits *before* the first line.
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix:     "\n",
			Color:           strPtr("#569cd6"),
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	H1: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     " ",
			BlockSuffix:     " ",
			Prefix:          " ",
			Suffix:          " ",
			Color:           strPtr("#ffffff"),
			BackgroundColor: strPtr("#0e639c"), // VSCode "button" blue
			Bold:            boolPtr(true),
		},
	},
	H2: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "▌ ",
			Prefix:      "▌ ",
			// A grey rule line under each H2 so sections are visibly
			// chunked — colour baked into the format so paragraphs
			// after the heading don't inherit it.
			BlockSuffix:     "\n\x1b[38;2;58;58;58m" + strings.Repeat("─", 40) + "\x1b[0m",
			Color:           strPtr("#569cd6"),
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	H3: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     "▎ ",
			Prefix:          "▎ ",
			Color:           strPtr("#4ec9b0"), // teal
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	H4: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     "▏ ",
			Prefix:          "▏ ",
			Color:           strPtr("#dcdcaa"), // function yellow
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	H5: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     "· ",
			Prefix:          "· ",
			Color:           strPtr("#9cdcfe"),
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	H6: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix:     "· ",
			Prefix:          "· ",
			Color:           strPtr("#c586c0"),
			BackgroundColor: strPtr(editorBg),
			Bold:            boolPtr(true),
		},
	},
	Text: ansi.StylePrimitive{
		Color:           strPtr("#d0d0d0"),
		BackgroundColor: strPtr(editorBg),
	},
	Strikethrough: ansi.StylePrimitive{
		CrossedOut:      boolPtr(true),
		BackgroundColor: strPtr(editorBg),
	},
	Emph: ansi.StylePrimitive{
		// Was orange (#ce9178) — too close to the inline-code colour and
		// made italic phrases blur into code spans. Cyan reads as "this
		// is emphasised text" without competing with code tokens.
		Italic:          boolPtr(true),
		Color:           strPtr("#9cdcfe"),
		BackgroundColor: strPtr(editorBg),
	},
	Strong: ansi.StylePrimitive{
		Bold:            boolPtr(true),
		Color:           strPtr("#dcdcaa"),
		BackgroundColor: strPtr(editorBg),
	},
	HorizontalRule: ansi.StylePrimitive{
		Color:           strPtr(hrFg),
		BackgroundColor: strPtr(editorBg),
		// glamour ignores Color when the Format string is set with
		// inline ANSI. Bake the colour into the format itself so the
		// rule is always rendered grey, regardless of the surrounding
		// paragraph's foreground.
		Format: "\n\x1b[38;2;58;58;58m" + strings.Repeat("─", 40) + "\x1b[0m\n",
	},
	Item: ansi.StylePrimitive{
		BlockPrefix:     "• ",
		Color:           strPtr("#75beff"),
		BackgroundColor: strPtr(editorBg),
	},
	Enumeration: ansi.StylePrimitive{
		BlockPrefix:     ". ",
		Color:           strPtr("#75beff"),
		BackgroundColor: strPtr(editorBg),
	},
	Task: ansi.StyleTask{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#d0d0d0"),
			BackgroundColor: strPtr(editorBg),
		},
		Ticked:   "[✓] ",
		Unticked: "[ ] ",
	},
	Link: ansi.StylePrimitive{
		Color:           strPtr("#3794ff"),
		BackgroundColor: strPtr(editorBg),
		Underline:       boolPtr(true),
	},
	LinkText: ansi.StylePrimitive{
		Color:           strPtr("#4ec9b0"),
		BackgroundColor: strPtr(editorBg),
		Bold:            boolPtr(true),
	},
	Image: ansi.StylePrimitive{
		Color:           strPtr("#3794ff"),
		BackgroundColor: strPtr(editorBg),
		Underline:       boolPtr(true),
	},
	ImageText: ansi.StylePrimitive{
		Color:           strPtr("#9cdcfe"),
		BackgroundColor: strPtr(editorBg),
		Format:          "Image: {{.text}} →",
	},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix:          " ",
			Suffix:          " ",
			Color:           strPtr(codeFg), // warm tan, not flame orange
			BackgroundColor: strPtr(codeBg), // gentle tint, not loud highlight
		},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           strPtr("#d4d4d4"),
				BackgroundColor: strPtr(codeBlockBg),
			},
			Margin: uintPtr(2),
		},
		Chroma: &ansi.Chroma{
			Text:                ansi.StylePrimitive{Color: strPtr("#d0d0d0")},
			Error:               ansi.StylePrimitive{Color: strPtr("#f14c4c")},
			Comment:             ansi.StylePrimitive{Color: strPtr("#6a9955"), Italic: boolPtr(true)},
			CommentPreproc:      ansi.StylePrimitive{Color: strPtr("#bd63c5")},
			Keyword:             ansi.StylePrimitive{Color: strPtr("#569cd6")},
			KeywordReserved:     ansi.StylePrimitive{Color: strPtr("#569cd6")},
			KeywordNamespace:    ansi.StylePrimitive{Color: strPtr("#c586c0")},
			KeywordType:         ansi.StylePrimitive{Color: strPtr("#4ec9b0")},
			Operator:            ansi.StylePrimitive{Color: strPtr("#d0d0d0")},
			Punctuation:         ansi.StylePrimitive{Color: strPtr("#d0d0d0")},
			Name:                ansi.StylePrimitive{Color: strPtr("#9cdcfe")},
			NameBuiltin:         ansi.StylePrimitive{Color: strPtr("#4ec9b0")},
			NameTag:             ansi.StylePrimitive{Color: strPtr("#569cd6")},
			NameAttribute:       ansi.StylePrimitive{Color: strPtr("#9cdcfe")},
			NameClass:           ansi.StylePrimitive{Color: strPtr("#4ec9b0")},
			NameConstant:        ansi.StylePrimitive{Color: strPtr("#4fc1ff")},
			NameDecorator:       ansi.StylePrimitive{Color: strPtr("#dcdcaa")},
			NameFunction:        ansi.StylePrimitive{Color: strPtr("#dcdcaa")},
			LiteralNumber:       ansi.StylePrimitive{Color: strPtr("#b5cea8")},
			LiteralString:       ansi.StylePrimitive{Color: strPtr("#ce9178")},
			LiteralStringEscape: ansi.StylePrimitive{Color: strPtr("#d7ba7d")},
			GenericDeleted:      ansi.StylePrimitive{Color: strPtr("#c74e39")},
			GenericInserted:     ansi.StylePrimitive{Color: strPtr("#73c991")},
			GenericEmph:         ansi.StylePrimitive{Italic: boolPtr(true)},
			GenericStrong:       ansi.StylePrimitive{Bold: boolPtr(true)},
		},
	},
	Table: ansi.StyleTable{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr("#d4d4d4"),
				// Slightly lighter than the document bg so a table reads
				// as a distinct "card" — same step as the inline-code
				// background. Without this every cell merges into the
				// surrounding paragraphs.
				BackgroundColor: strPtr("#23262c"),
			},
		},
		CenterSeparator: strPtr("┼"),
		ColumnSeparator: strPtr("│"),
		RowSeparator:    strPtr("─"),
	},
	DefinitionList: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#d0d0d0"),
			BackgroundColor: strPtr(editorBg),
		},
	},
	DefinitionTerm: ansi.StylePrimitive{
		Color:           strPtr("#dcdcaa"),
		BackgroundColor: strPtr(editorBg),
		Bold:            boolPtr(true),
	},
	DefinitionDescription: ansi.StylePrimitive{
		BlockPrefix:     "\n  → ",
		Color:           strPtr("#d0d0d0"),
		BackgroundColor: strPtr(editorBg),
	},
	HTMLBlock: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#858585"),
			BackgroundColor: strPtr(editorBg),
		},
	},
	HTMLSpan: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("#858585"),
			BackgroundColor: strPtr(editorBg),
		},
	},
}

// PreviewMsg carries rendered preview content from the async fetch+render cmd.
type PreviewMsg struct {
	Title string
	Body  string
}

// openPreviewCmd fetches the active buffer's content and renders it as
// markdown using glamour. Only triggered when the active file looks like
// a markdown document — non-markdown files would render as raw text
// dressed up with markdown styling, which is just visual noise. We push
// a toast in that case so the user knows why nothing happened.
func (m *Model) openPreviewCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	path := m.editor.Path()
	if !isMarkdownFile(path) {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "Markdown preview only works on .md files")
		return toastCmd
	}
	c := m.nvim
	width := m.w
	if width <= 0 {
		width = 80
	}
	return func() tea.Msg {
		content, err := c.BufferContent()
		if err != nil {
			return ErrMsg{Err: err}
		}
		rendered, err := renderMarkdown(content, width-2)
		if err != nil {
			return ErrMsg{Err: err}
		}
		title := "Preview"
		if path != "" {
			title = filepath.Base(path) + "  (preview)"
		}
		return PreviewMsg{Title: title, Body: rendered}
	}
}

// isMarkdownFile reports whether `path` has an extension we treat as
// markdown for preview purposes. Case-insensitive on the extension.
func isMarkdownFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".mdown", ".mkd", ".mdx":
		return true
	}
	return false
}

func renderMarkdown(content string, width int) (string, error) {
	if width < 30 {
		width = 30
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(vscodeDarkPlusGlamourStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(content)
	if err != nil {
		return "", err
	}
	return paintStatusWords(paintTableRows(paintTableHeaders(out))), nil
}

// paintTableRows wraps every glamour-emitted table row in a single bg
// SGR pair so the gaps between cells (which glamour renders as plain
// space without a background) inherit the table's "card" colour
// instead of leaking the document background through. Detection is
// the same idea as paintTableHeaders: any line with two or more `│`
// glyphs, or a line composed entirely of separator chars.
//
// Cells that already carry their own bg (e.g. a coloured status word)
// keep it because their inner SGR overrides the row-wide default.
func paintTableRows(s string) string {
	const (
		bg    = "\x1b[48;2;35;38;44m" // matches Table StyleBlock bg
		reset = "\x1b[0m"
	)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		raw := stripANSI(line)
		isCell := strings.Count(raw, "│") >= 2
		isSep := isTableSeparatorRow(raw)
		if !isCell && !isSep {
			continue
		}
		lines[i] = bg + line + reset
	}
	return strings.Join(lines, "\n")
}

// paintStatusWords re-paints occurrences of common status words so they
// stand out at a glance in plan / TODO style markdown:
//   "done"  → green
//   "open"  → yellow
//   "TODO"  → orange
//   "DONE"  → green
// Word-boundary matched (preceded / followed by non-letter) so we don't
// recolour "openssl" or "fundone".
func paintStatusWords(s string) string {
	const (
		green  = "\x1b[38;2;115;201;145m"
		yellow = "\x1b[38;2;220;220;170m"
		orange = "\x1b[38;2;247;140;108m"
		reset  = "\x1b[0m"
	)
	subs := []struct {
		pattern *regexp.Regexp
		colour  string
	}{
		{regexp.MustCompile(`\b(done)\b`), green},
		{regexp.MustCompile(`\b(DONE)\b`), green},
		{regexp.MustCompile(`\b(open)\b`), yellow},
		{regexp.MustCompile(`\b(TODO)\b`), orange},
	}
	for _, sub := range subs {
		s = sub.pattern.ReplaceAllString(s, sub.colour+"$1"+reset)
	}
	return s
}

// paintTableHeaders re-inks the first row of every glamour-emitted table
// with a "list active" blue background so headers visually separate from
// body rows. glamour's StyleTable doesn't expose a header-row hook, so
// we post-process the rendered output: identify a row that's followed
// by a `│` row of dashes/cross characters and wrap it in ANSI bg.
//
// Detection rules: a candidate header line starts with `│`, contains
// other `│` separators, and the next line (after the trailing newline)
// is composed entirely of `│`, `─`, `┼`, and spaces. Both must be true
// for us to commit — guards against treating arbitrary `│` lines (e.g.
// box-drawing in code blocks) as table headers.
func paintTableHeaders(s string) string {
	const (
		headerBg = "\x1b[48;2;38;79;120m" // VSCode list-active blue
		reset    = "\x1b[0m"
	)
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines)-1; i++ {
		raw := stripANSI(lines[i])
		next := stripANSI(lines[i+1])
		if !strings.Contains(raw, "│") || strings.Count(raw, "│") < 2 {
			continue
		}
		if !isTableSeparatorRow(next) {
			continue
		}
		// Wrap the line in a bg span. We bracket the entire line so
		// every cell — including any whitespace padding glamour added —
		// gets the highlight.
		lines[i] = headerBg + lines[i] + reset
	}
	return strings.Join(lines, "\n")
}

// isTableSeparatorRow reports whether a stripped line is the row glamour
// emits between a table's header and its body — typically all `│`,
// `─`, `┼`, and spaces.
func isTableSeparatorRow(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case '│', '─', '┼', '├', '┤', ' ':
		default:
			return false
		}
	}
	// Has to contain at least one `─` to qualify (otherwise it's just a
	// row of empty cells with column separators).
	return strings.ContainsRune(s, '─')
}

// stripANSI removes CSI escape sequences from s. Used for table-header
// detection so colour codes glamour added don't break the structural
// match.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if c >= 0x40 && c <= 0x7e {
				in = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// isMarkdownPath reports whether path looks like a markdown file.
func isMarkdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".mdx", ".mdown":
		return true
	}
	return false
}
