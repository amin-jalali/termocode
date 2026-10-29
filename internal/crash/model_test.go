package crash

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type boomMsg struct{}
type cmdBoomMsg struct{}

// inner is a model that panics on boomMsg (in Update) or when viewBoom is
// set (in View), and returns a panicking command on cmdBoomMsg.
type inner struct {
	updates  *int
	viewBoom bool
}

func (m inner) Init() tea.Cmd { return nil }
func (m inner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	*m.updates++
	switch msg.(type) {
	case boomMsg:
		panic("update boom")
	case cmdBoomMsg:
		return m, func() tea.Msg { panic("cmd boom") }
	case tea.KeyMsg:
		m.viewBoom = true
	}
	return m, nil
}
func (m inner) View() string {
	if m.viewBoom {
		panic("view boom")
	}
	return "frame"
}

func TestWrapRecordsEventTypes(t *testing.T) {
	useTempState(t)
	n := 0
	w := Wrap(inner{updates: &n}, nil)
	w, _ = w.Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	ev := Events()
	if len(ev) != 1 || ev[0] != "tea.WindowSizeMsg" {
		t.Fatalf("events = %v", ev)
	}
}

// TestWrapUpdatePanic: the report is written and marked for the next
// start, onFatal gets it, and the broken model is never called again.
func TestWrapUpdatePanic(t *testing.T) {
	useTempState(t)
	n := 0
	var fatal []string
	w := Wrap(inner{updates: &n}, func(p string) { fatal = append(fatal, p) })
	w, _ = w.Update(boomMsg{})
	if len(fatal) != 1 {
		t.Fatalf("onFatal called %d times", len(fatal))
	}
	b, err := os.ReadFile(fatal[0])
	if err != nil || !strings.Contains(string(b), "update boom") || !strings.Contains(string(b), "crash.boomMsg") {
		t.Fatalf("report %v:\n%s", err, b)
	}
	if Pending() != fatal[0] {
		t.Fatalf("pending = %q", Pending())
	}
	w, cmd := w.Update(tea.WindowSizeMsg{})
	if n != 1 || cmd != nil {
		t.Fatalf("dead model updated (%d calls, cmd %v)", n, cmd)
	}
	if v := w.View(); v != "" {
		t.Fatalf("dead model view %q", v)
	}
	if len(fatal) != 1 {
		t.Fatalf("onFatal called again")
	}
}

// TestWrapViewPanic: a panic in View is fatal too; View then shows the
// last good frame.
func TestWrapViewPanic(t *testing.T) {
	useTempState(t)
	n := 0
	var fatal []string
	w := Wrap(inner{updates: &n}, func(p string) { fatal = append(fatal, p) })
	if v := w.View(); v != "frame" {
		t.Fatalf("view %q", v)
	}
	w, _ = w.Update(tea.KeyMsg{})
	if v := w.View(); v != "frame" {
		t.Fatalf("view after panic %q, want the last good frame", v)
	}
	if len(fatal) != 1 || !strings.Contains(readFile(t, fatal[0]), "view boom") {
		t.Fatalf("fatal = %v", fatal)
	}
}

// TestWrapGuardsCommands: a panic in a command the model returned is not
// fatal: it becomes a notice.
func TestWrapGuardsCommands(t *testing.T) {
	useTempState(t)
	drainNotices()
	n := 0
	fatal := 0
	w := Wrap(inner{updates: &n}, func(string) { fatal++ })
	_, cmd := w.Update(cmdBoomMsg{})
	if msg := cmd(); msg != nil {
		t.Fatalf("msg %v", msg)
	}
	nextNotice(t)
	if fatal != 0 {
		t.Fatal("a command panic was fatal")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
