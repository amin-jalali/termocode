package testkit

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// Perf guards (STAB-5): each measures one number and compares it with
// its budget in internal/perfbudget/budgets.txt. They run only with
// TERMOCODE_PERF=1 (see package perfbudget); .github/workflows/perf.yml
// runs them without -race.

// ms is d in milliseconds, one decimal.
func ms(d time.Duration) float64 { return float64(d.Microseconds()/100) / 10 }

// percentile is the p-th percentile (0–100) of ds.
func percentile(ds []time.Duration, p int) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	i := (len(s)*p + 99) / 100
	return s[max(0, min(len(s)-1, i-1))]
}

// waitFrame delivers msg and waits (rendering after each message) until
// the stripped frame passes cond. It returns how long that took.
func waitFrame(d *Driver, msg tea.Msg, cond func(string) bool, timeout time.Duration) (time.Duration, bool) {
	d.T.Helper()
	t0 := time.Now()
	d.deliver(msg)
	ok := d.WaitFor(func() bool { return cond(ansi.Strip(d.render())) }, timeout)
	return time.Since(t0), ok
}

// Keystroke to frame in the editor (real nvim): the p95 of the time from
// a typed letter to a frame that shows it.
func TestPerfEditorKeyToFrame(t *testing.T) {
	perfbudget.Require(t)
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Settle(time.Second)
	const letters = "qwertyuiopasdfghjklzxcvbnm"
	typed := ""
	var took []time.Duration
	for i := 0; i < 40; i++ {
		r := rune(letters[i%len(letters)])
		typed += string(r)
		want := typed[max(0, len(typed)-8):] + "alpha"
		dt, ok := waitFrame(d, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}},
			func(s string) bool { return strings.Contains(s, want) }, 5*time.Second)
		if !ok {
			t.Fatalf("key %d (%q) never showed %q:\n%s", i, r, want, screen(d))
		}
		took = append(took, dt)
		d.Settle(50 * time.Millisecond)
	}
	t.Logf("key to frame: p50 %v, p95 %v, max %v", percentile(took, 50), percentile(took, 95), percentile(took, 100))
	perfbudget.Check(t, "editor_key_to_frame_p95", ms(percentile(took, 95)))
}

// Opening a 50 000-line file: from the open request to its first lines
// on screen.
func TestPerfOpen50kLineFile(t *testing.T) {
	perfbudget.Require(t)
	var b strings.Builder
	for i := 1; i <= 50000; i++ {
		fmt.Fprintf(&b, "line %05d: the quick brown fox jumps over the lazy dog\n", i)
	}
	files := textFiles()
	files["big.txt"] = b.String()
	d := editorDriver(t, files)
	d.Settle(time.Second)
	dt, ok := waitFrame(d, app.DevActionMsg{ID: "open:" + filepath.Join(d.Dir, "big.txt")}, func(s string) bool {
		_, a := tabs(d)
		return a == "big.txt" && strings.Contains(s, "line 00001:") && strings.Contains(s, "line 00010:")
	}, 20*time.Second)
	if !ok {
		t.Fatalf("big.txt did not show:\n%s", screen(d))
	}
	perfbudget.Check(t, "open_50k_line_file", ms(dt))
}

// Opening the command palette with every command listed: the median of
// five opens.
func TestPerfPaletteOpen(t *testing.T) {
	perfbudget.Require(t)
	d := New(t, Options{W: 120, H: 40})
	var took []time.Duration
	for i := 0; i < 5; i++ {
		dt, ok := waitFrame(d, KeyMsg("f1"), func(s string) bool { return strings.Contains(s, "Command Palette") }, 5*time.Second)
		if !ok {
			t.Fatalf("F1 did not open the palette:\n%s", screen(d))
		}
		took = append(took, dt)
		d.Key("esc")
		if strings.Contains(screen(d), "Command Palette") {
			t.Fatalf("Esc did not close the palette:\n%s", screen(d))
		}
	}
	perfbudget.Check(t, "palette_open_all", ms(percentile(took, 50)))
}

// A workspace with 5 000 files in one folder: from the start of the app
// to the explorer showing them.
func TestPerfExplorer5kFiles(t *testing.T) {
	perfbudget.Require(t)
	files := map[string]string{"README.md": "# many\n"}
	for i := 0; i < 5000; i++ {
		files[fmt.Sprintf("many/f%04d.txt", i)] = "x\n"
	}
	d := New(t, Options{W: 120, H: 40, Files: files})
	x, y, ok := find(d, "many")
	if !ok {
		t.Fatalf("no many/ folder in the explorer:\n%s", screen(d))
	}
	// A click on the folder: press, then the release (timed) opens it.
	d.deliver(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	t0 := time.Now()
	_, ok = waitFrame(d, tea.MouseMsg{X: x, Y: y, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
		func(s string) bool { return strings.Contains(s, "f0000.txt") }, 10*time.Second)
	dt := time.Since(t0)
	if !ok {
		t.Fatalf("many/ did not open:\n%s", screen(d))
	}
	perfbudget.Check(t, "explorer_5k_files", ms(dt))
}
