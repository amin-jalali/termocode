package mdview

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

func sample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../devtools/shotkit/testdata/preview.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withMode sets a true-colour profile and the icon mode for one test.
func withMode(t *testing.T, mode theme.IconMode) {
	t.Helper()
	oldP, oldI := lipgloss.ColorProfile(), theme.CurrentIconMode
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme.CurrentIconMode = mode
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldP)
		theme.CurrentIconMode = oldI
	})
}

func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// unstyledCell returns the index of the first visible cell drawn without
// a background colour, or -1. It follows SGR (0 / 48 / 49) and skips OSC.
func unstyledCell(row string) int {
	bg, cell := false, 0
	for i := 0; i < len(row); {
		if row[i] == 0x1b && i+1 < len(row) && row[i+1] == '[' {
			j := i + 2
			for j < len(row) && row[j] != 'm' {
				j++
			}
			ps := strings.Split(row[i+2:j], ";")
			for k := 0; k < len(ps); k++ {
				switch ps[k] {
				case "", "0":
					bg = false
				case "49":
					bg = false
				case "48":
					bg = true
					if k+1 < len(ps) && ps[k+1] == "2" {
						k += 4
					} else {
						k += 2
					}
				case "38", "58":
					if k+1 < len(ps) && ps[k+1] == "2" {
						k += 4
					} else {
						k += 2
					}
				}
			}
			i = j + 1
			continue
		}
		if row[i] == 0x1b && i+1 < len(row) && row[i+1] == ']' {
			j := i + 2
			for j < len(row) && row[j] != 0x07 && !(row[j] == 0x1b && j+1 < len(row) && row[j+1] == '\\') {
				j++
			}
			if j < len(row) && row[j] == 0x1b {
				j++
			}
			i = j + 1
			continue
		}
		if !bg {
			return cell
		}
		_, n := decodeRune(row[i:])
		i += n
		cell++
	}
	return -1
}

func decodeRune(s string) (rune, int) {
	for n := 1; n <= len(s) && n <= 4; n++ {
		if r := []rune(s[:n]); len(r) == 1 && r[0] != 0xFFFD {
			return r[0], n
		}
	}
	return 0xFFFD, 1
}

// Spec: a code span that runs over a line break in the source stays on
// rendered rows (the break is a space, as in CommonMark); no row holds a
// raw newline.
func TestCodeSpanAcrossLineBreak(t *testing.T) {
	src := "it turns yellow: `no answer for 35 s — Esc\nto stop`. Then more.\n"
	for _, w := range []int{20, 40, 80} {
		rows := Render(src, Options{Width: w})
		for i, r := range rows {
			if strings.Contains(r, "\n") {
				t.Fatalf("width %d: row %d holds a newline: %q", w, i, ansi.Strip(r))
			}
			if got := ansi.StringWidth(r); got != w {
				t.Fatalf("width %d: row %d is %d cells: %q", w, i, got, ansi.Strip(r))
			}
		}
	}
	if all := ansi.Strip(strings.Join(Render(src, Options{Width: 80}), " ")); !strings.Contains(all, "Esc to stop") {
		t.Errorf("code span text lost: %q", all)
	}
}

// Every row is exactly the view width and every cell has a background,
// at any width and in every icon mode.
func TestRowsExactWidthAndStyled(t *testing.T) {
	src := sample(t) + "\n![remote](https://x/y.png)\n\n<div>raw html</div>\n\n" +
		"| a | very long header cell that will need to wrap in a narrow window |\n|---|---|\n| 1 | " +
		strings.Repeat("word ", 30) + "|\n\n" + strings.Repeat("x", 300) + "\n"
	for _, mode := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
		withMode(t, mode)
		for _, w := range []int{1, 8, 20, 40, 63, 80, 100, 150, 240} {
			rows := Render(src, Options{Width: w})
			for i, r := range rows {
				if got := ansi.StringWidth(r); got != w {
					t.Fatalf("mode %d width %d: row %d is %d cells: %q", mode, w, i, got, ansi.Strip(r))
				}
				if c := unstyledCell(r); c >= 0 {
					t.Fatalf("mode %d width %d: row %d cell %d has no background: %q", mode, w, i, c, r)
				}
			}
		}
	}
}

func TestLayoutCapsTheColumn(t *testing.T) {
	for _, c := range []struct{ w, left, col int }{
		{240, 70, 100}, {100, 4, 92}, {60, 2, 56}, {10, 0, 10},
	} {
		if l, col := Layout(c.w, 0); l != c.left || col != c.col {
			t.Errorf("Layout(%d) = %d,%d want %d,%d", c.w, l, col, c.left, c.col)
		}
	}
	if _, col := Layout(240, 72); col != 72 {
		t.Errorf("MaxColumn ignored: %d", col)
	}
	withMode(t, theme.IconModeUnicode)
	long := strings.Repeat("lorem ipsum dolor ", 40)
	left, col := Layout(240, 0)
	for i, r := range plain(Render(long, Options{Width: 240})) {
		text := strings.TrimRight(r, " ")
		if text == "" {
			continue
		}
		if !strings.HasPrefix(r, strings.Repeat(" ", left)) || len([]rune(text)) > left+col {
			t.Errorf("row %d leaves the reading column: %q", i, text)
		}
		if strings.Contains(text, "lorem") && strings.HasPrefix(strings.TrimLeft(text, " "), " ") {
			t.Errorf("row %d has a leading space", i)
		}
	}
}

func TestListHangingIndentAndMarkers(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "- first item that is long enough to wrap onto a second row here\n  - nested\n    - third\n\n" +
		"1. one\n2. two that also wraps around the narrow column width\n\n" +
		"- [x] done task\n- [ ] open task\n"
	rows := plain(Render(src, Options{Width: 40}))
	left, _ := Layout(40, 0)
	body := make([]string, 0, len(rows))
	for _, r := range rows {
		body = append(body, strings.TrimRight(r[min(len(r), left):], " "))
	}
	find := func(prefix string) int {
		for i, r := range body {
			if strings.HasPrefix(r, prefix) {
				return i
			}
		}
		t.Fatalf("no row starts with %q:\n%s", prefix, strings.Join(body, "\n"))
		return -1
	}
	i := find("  • first")
	if !strings.HasPrefix(body[i+1], "    ") || strings.HasPrefix(body[i+1], "     ") {
		t.Errorf("wrapped bullet row must hang under the text (4 cells): %q", body[i+1])
	}
	find("    ◦ nested")
	find("      ▪ third")
	j := find("  2. two")
	if !strings.HasPrefix(body[j+1], "     ") || strings.HasPrefix(body[j+1], "      ") {
		t.Errorf("wrapped number row must hang under the text (5 cells): %q", body[j+1])
	}
	find("  ☑ done task")
	find("  ☐ open task")

	theme.CurrentIconMode = theme.IconModeASCII
	out := strings.Join(plain(Render(src, Options{Width: 40})), "\n")
	for _, want := range []string{"- first", "* nested", "[x] done task", "[ ] open task"} {
		if !strings.Contains(out, want) {
			t.Errorf("ascii mode: missing %q", want)
		}
	}
	if strings.ContainsAny(out, "•◦▪☑☐↪▸") {
		t.Errorf("ascii mode uses unicode markers:\n%s", out)
	}
}

func TestHeadingsRulesAndSpacing(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "# One\n\ntext\n\n## Two\n\n### Three\n\n#### Four\n\nend\n"
	_, col := Layout(120, 0)
	rows := plain(Render(src, Options{Width: 120}))
	trim := func(s string) string { return strings.TrimSpace(s) }
	at := func(text string) int {
		for i, r := range rows {
			if strings.HasSuffix(trim(r), text) {
				return i
			}
		}
		t.Fatalf("missing %q", text)
		return -1
	}
	if r := trim(rows[at("One")+1]); r != strings.Repeat("━", col) {
		t.Errorf("H1 rule = %q (want %d ━)", r, col)
	}
	if r := trim(rows[at("Two")+1]); r != strings.Repeat("─", col) {
		t.Errorf("H2 rule = %q (want %d ─)", r, col)
	}
	if r := trim(rows[at("Three")]); r != "Three" { // UI review #11: no chevron (it suggests folding)
		t.Errorf("H3 row = %q", r)
	}
	if r := trim(rows[at("Three")+1]); r != "" {
		t.Errorf("H3 must have no rule, got %q", r)
	}
	for i := 1; i < len(rows); i++ {
		if trim(rows[i]) == "" && trim(rows[i-1]) == "" {
			t.Errorf("doubled blank rows at %d", i)
		}
	}
	all := strings.Join(rows, "\n")
	if strings.ContainsAny(all, "▌▎#") {
		t.Errorf("heading bars or # marks left:\n%s", all)
	}
}

var cellBorder = regexp.MustCompile(`[│┼┬┴├┤┌┐└┘]`)

func TestTableLayout(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "| Key | Action | Mode |\n|:--|:-:|--:|\n| j | Scroll down | normal |\n| esc | Close | any |\n"
	rows := plain(Render(src, Options{Width: 100}))
	var tbl []string
	for _, r := range rows {
		if cellBorder.MatchString(r) {
			tbl = append(tbl, strings.TrimRight(r, " "))
		}
	}
	if len(tbl) != 6 {
		t.Fatalf("want 6 table rows (top, head, sep, 2 body, bottom), got %d:\n%s", len(tbl), strings.Join(tbl, "\n"))
	}
	pos := func(s string) []int {
		var out []int
		for i, r := range []rune(s) {
			if cellBorder.MatchString(string(r)) {
				out = append(out, i)
			}
		}
		return out
	}
	want := pos(tbl[0])
	for _, r := range tbl[1:] {
		if got := pos(r); !equalInts(got, want) {
			t.Errorf("columns not aligned: %v vs %v\n%s", got, want, strings.Join(tbl, "\n"))
		}
	}
	if !strings.Contains(tbl[3], "│ normal │") || !strings.Contains(tbl[4], "│    any │") {
		t.Errorf("right alignment lost:\n%s", strings.Join(tbl, "\n"))
	}
	if !strings.Contains(tbl[4], "│    Close    │") {
		t.Errorf("centre alignment lost:\n%s", strings.Join(tbl, "\n"))
	}

	// Too wide for the window: columns shrink and cells wrap.
	long := "| a | b |\n|---|---|\n| " + strings.Repeat("alpha ", 20) + "| " + strings.Repeat("beta ", 20) + "|\n"
	for _, r := range Render(long, Options{Width: 50}) {
		if ansi.StringWidth(r) != 50 {
			t.Fatalf("row width %d", ansi.StringWidth(r))
		}
	}
	n := 0
	for _, r := range plain(Render(long, Options{Width: 50})) {
		if strings.Contains(r, "alpha") {
			n++
		}
	}
	if n < 3 {
		t.Errorf("long cells should wrap over several rows, got %d", n)
	}
}

func equalInts(a, b []int) bool {
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

func TestCodeBlock(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "```go\nfunc main() {\n\tx := \"" + strings.Repeat("s", 120) + "\"\n}\n```\n"
	raw := Render(src, Options{Width: 80})
	rows := plain(raw)
	all := strings.Join(rows, "\n")
	if !strings.Contains(all, "go  ") {
		t.Errorf("language label missing:\n%s", all)
	}
	if !strings.Contains(all, "↪ ") {
		t.Errorf("long line must soft-wrap with ↪:\n%s", all)
	}
	if !strings.Contains(all, "      x := \"sss") {
		t.Errorf("tab must expand to 4 spaces after 2 cells of padding:\n%s", all)
	}
	// Syntax colours: more than one foreground in the func row.
	for i, r := range rows {
		if strings.Contains(r, "func main") {
			if n := len(regexp.MustCompile(`38;2;\d+;\d+;\d+`).FindAllString(raw[i], -1)); n < 3 {
				t.Errorf("code row has %d colours, want syntax colours", n)
			}
		}
	}
	// No wrap of the label row into the text column: every row same width.
	for _, r := range raw {
		if ansi.StringWidth(r) != 80 {
			t.Fatalf("width %d", ansi.StringWidth(r))
		}
	}
}

func TestLinksAndInlineCode(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "See [docs](https://example.com/a/b) and [guide](guide.md), use `go test`, open the file.\n"
	out := strings.Join(Render(src, Options{Width: 100}), "\n")
	if !strings.Contains(out, "\x1b]8;;https://example.com/a/b\x07") {
		t.Error("web link must be an OSC 8 hyperlink")
	}
	if strings.Contains(out, "]8;;guide.md") {
		t.Error("relative link must not be an OSC 8 hyperlink")
	}
	txt := ansi.Strip(out)
	if !strings.Contains(txt, "docs example.com/a/b and guide, use  go test ,") {
		t.Errorf("link / code text = %q", strings.TrimSpace(txt))
	}
	// Plain words like "open" keep the text style (no status-word paint).
	if strings.Count(out, theme.FgSeq(theme.AccentAmber)+"open") > 0 {
		t.Error("plain word painted")
	}
}

func TestImageHook(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	called := ""
	opt := Options{Width: 60, Image: func(dest, alt string, w int) []string {
		called = dest + "|" + alt
		return []string{strings.Repeat("█", w+5)} // too wide: must be cut
	}}
	rows := Render("![a cat](cat.png)\n\ntext ![inline](x.png) more\n", opt)
	if called != "cat.png|a cat" {
		t.Errorf("hook called with %q", called)
	}
	for _, r := range rows {
		if ansi.StringWidth(r) != 60 {
			t.Fatalf("width %d", ansi.StringWidth(r))
		}
	}
	if !strings.Contains(strings.Join(plain(rows), "\n"), "[image: inline]") {
		t.Error("inline image placeholder missing")
	}
}
