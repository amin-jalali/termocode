package specui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// wait is the default time a behaviour may take to show up.
const wait = 5 * time.Second

// screen returns the last frame without colours.
func screen(d *testkit.Driver) string { return ansi.Strip(d.Frame()) }

// locate finds s in the frame and returns its screen cell (x, y).
func locate(d *testkit.Driver, s string) (x, y int, ok bool) {
	for row, line := range strings.Split(screen(d), "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return ansi.StringWidth(line[:i]), row, true
		}
	}
	return 0, 0, false
}

// lineWith returns the first frame row that contains s ("" if none).
func lineWith(d *testkit.Driver, s string) string {
	for _, line := range strings.Split(screen(d), "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	return ""
}

// clickText clicks the first cell of s on screen.
func clickText(t *testing.T, d *testkit.Driver, s string) {
	t.Helper()
	x, y, ok := locate(d, s)
	if !ok {
		t.Fatalf("%q not on screen:\n%s", s, screen(d))
	}
	d.Click(x, y)
}

// mustSee waits for s on screen and fails the test (now) without it.
func mustSee(t *testing.T, d *testkit.Driver, s string) {
	t.Helper()
	if !d.WaitForText(s, wait) {
		t.Fatalf("%q never appeared:\n%s", s, screen(d))
	}
}

// mustNotSee fails when s is on screen after the frame settles.
func mustNotSee(t *testing.T, d *testkit.Driver, s string) {
	t.Helper()
	d.Settle(100 * time.Millisecond)
	if strings.Contains(screen(d), s) {
		t.Fatalf("%q should not be on screen:\n%s", s, screen(d))
	}
}

// waitFile waits until the file at rel (in the workspace) satisfies ok and
// returns its content.
func waitFile(t *testing.T, d *testkit.Driver, rel string, ok func(string) bool) string {
	t.Helper()
	var body string
	d.WaitFor(func() bool {
		b, err := os.ReadFile(filepath.Join(d.Dir, rel))
		body = string(b)
		return err == nil && ok(body)
	}, wait)
	return body
}

// configPath is the sandboxed config.json.
func configPath() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json")
}

var (
	activeTabRe = regexp.MustCompile(`▎ (\S+) x`)
	cursorRe    = regexp.MustCompile(`Ln \d+`)
)

// activeTab is the name on the highlighted editor tab ("" if none).
func activeTab(d *testkit.Driver) string {
	first, _, _ := strings.Cut(screen(d), "\n")
	// The sidebar header starts with the same bar; skip past it.
	if i := strings.Index(first, "▕"); i >= 0 {
		first = first[i:]
	}
	if m := activeTabRe.FindStringSubmatch(first); m != nil {
		return m[1]
	}
	return ""
}

// cursorLine is the status bar's "Ln N".
func cursorLine(d *testkit.Driver) string {
	return cursorRe.FindString(lineWith(d, "Ln "))
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// colorsAt returns the truecolor foreground and background ("#rrggbb",
// "" when unset) of the first cell of text on screen.
func colorsAt(d *testkit.Driver, text string) (fg, bg string, ok bool) {
	for _, raw := range strings.Split(d.Frame(), "\n") {
		plain := ansi.Strip(raw)
		if !strings.Contains(plain, text) {
			continue
		}
		// Walk the raw line, tracking SGR state, until the plain text
		// seen so far ends with text's first rune.
		var seen strings.Builder
		first := []rune(text)[0]
		target := strings.Index(plain, text)
		for i := 0; i < len(raw); {
			if loc := sgrRe.FindStringSubmatchIndex(raw[i:]); loc != nil && loc[0] == 0 {
				fg, bg = applySGR(raw[i+loc[2]:i+loc[3]], fg, bg)
				i += loc[1]
				continue
			}
			if seen.Len() == target && []rune(raw[i:])[0] == first {
				return fg, bg, true
			}
			r := []rune(raw[i:])[0]
			seen.WriteRune(r)
			i += len(string(r))
		}
	}
	return "", "", false
}

// applySGR updates fg / bg with one SGR parameter list.
func applySGR(params, fg, bg string) (string, string) {
	if params == "" || params == "0" {
		return "", ""
	}
	p := strings.Split(params, ";")
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case "0":
			fg, bg = "", ""
		case "39":
			fg = ""
		case "49":
			bg = ""
		case "38", "48":
			if i+4 < len(p) && p[i+1] == "2" {
				hex := "#" + hex2(p[i+2]) + hex2(p[i+3]) + hex2(p[i+4])
				if p[i] == "38" {
					fg = hex
				} else {
					bg = hex
				}
				i += 4
			}
		}
	}
	return fg, bg
}

func hex2(dec string) string {
	n := 0
	for _, c := range dec {
		n = n*10 + int(c-'0')
	}
	const digits = "0123456789abcdef"
	return string([]byte{digits[n>>4&15], digits[n&15]})
}

// slow skips a test in -short mode.
func slow(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow behaviour test (starts nvim)")
	}
}

// editorWith starts a driver with nvim and opens rel.
func editorWith(t *testing.T, files map[string]string, rel, shown string) *testkit.Driver {
	t.Helper()
	slow(t)
	d := testkit.New(t, testkit.Options{W: 120, H: 36, Nvim: true, Files: files})
	d.Action("open:" + rel)
	mustSee(t, d, shown)
	return d
}
