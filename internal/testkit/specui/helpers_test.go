package specui

import (
	"os"
	"path/filepath"
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
