package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Behaviour tests for the Welcome dashboard (the start screen in the
// editor area): a click on a card or a recent file must work wherever the
// layout puts it — with the AI chat panel open on the right, with the
// sidebar hidden, in a narrow window and after the AI panel was resized
// by drag. The hit areas must come from the same layout the view draws.

// welcomeCard is one START card: its label and the text a click on it
// must add to the screen (one more time than it was shown before).
type welcomeCard struct {
	label string
	shows string
}

// welcomeCards are the cards that work without nvim.
var welcomeCards = []welcomeCard{
	{"Open file", "Go to file"},
	{"Command palette", "Command Palette"},
	{"Find in files", "Find in Files"},
	{"Open workspace", "Open Recent Workspace"},
	{"Clone repository", "Clone Repository"},
}

// welcomeOpts is a w×40 app with one recent workspace, so the "Open
// workspace" card opens the workspace picker.
func welcomeOpts(w int) Options {
	return Options{W: w, H: 40, Config: map[string]string{
		"workspaces.json": `[{"path":"/","opened_at":"2026-01-01T00:00:00Z"}]`,
	}}
}

// openAIPanel opens the AI chat panel (Alt+A) and moves the focus back
// to the editor, so the welcome screen is what a click talks to.
func openAIPanel(d *Driver) {
	d.T.Helper()
	d.Key("alt+a")
	if !d.WaitForText("AI CHAT", 2*time.Second) {
		d.T.Fatalf("AI panel did not open:\n%s", screen(d))
	}
	d.Key("esc")
}

// aiPanelEdge is the screen column of the AI panel's left edge (its drag
// handle): the header row is " <spark> AI CHAT".
func aiPanelEdge(d *Driver) int {
	d.T.Helper()
	x, y, ok := find(d, "AI CHAT")
	if !ok || y != 0 {
		d.T.Fatalf("no AI panel header:\n%s", screen(d))
	}
	return x - 3
}

// clickCard clicks the welcome card that shows label, left of maxX (so
// a word in the AI panel is never hit).
func clickCard(d *Driver, label string, maxX int) {
	d.T.Helper()
	x, y, ok := findIn(d, label, 0, maxX)
	if !ok {
		d.T.Fatalf("card %q not on screen:\n%s", label, screen(d))
	}
	d.Click(x+1, y)
}

// checkCards clicks every card in its own fresh app (set up by start)
// and checks that the card's action ran.
func checkCards(t *testing.T, start func(t *testing.T) *Driver) {
	for _, c := range welcomeCards {
		t.Run(c.label, func(t *testing.T) {
			d := start(t)
			before := strings.Count(screen(d), c.shows)
			edge := d.W
			if strings.Contains(screen(d), "AI CHAT") {
				edge = aiPanelEdge(d)
			}
			clickCard(d, c.label, edge)
			if !d.WaitFor(func() bool { return strings.Count(screen(d), c.shows) > before }, 2*time.Second) {
				t.Fatalf("click on %q did nothing (want %q):\n%s", c.label, c.shows, screen(d))
			}
		})
	}
}

func TestWelcomeCardsClickWithoutAIPanel(t *testing.T) {
	checkCards(t, func(t *testing.T) *Driver {
		return New(t, welcomeOpts(160))
	})
}

func TestWelcomeCardsClickWithAIPanel(t *testing.T) {
	checkCards(t, func(t *testing.T) *Driver {
		d := New(t, welcomeOpts(160))
		openAIPanel(d)
		return d
	})
}

func TestWelcomeCardsClickWithAIPanelSidebarHidden(t *testing.T) {
	checkCards(t, func(t *testing.T) *Driver {
		d := New(t, welcomeOpts(160))
		d.Key("ctrl+b")
		if strings.Contains(screen(d), "EXPLORER") {
			t.Fatalf("sidebar still shown:\n%s", screen(d))
		}
		openAIPanel(d)
		return d
	})
}

func TestWelcomeCardsClickWithAIPanelNarrowWindow(t *testing.T) {
	checkCards(t, func(t *testing.T) *Driver {
		d := New(t, welcomeOpts(110))
		openAIPanel(d)
		return d
	})
}

func TestWelcomeCardsClickAfterAIPanelDragResize(t *testing.T) {
	checkCards(t, func(t *testing.T) *Driver {
		d := New(t, welcomeOpts(160))
		openAIPanel(d)
		edge := aiPanelEdge(d)
		d.Drag(edge, 5, edge+20, 5)
		if got := aiPanelEdge(d); got != edge+20 {
			t.Fatalf("drag moved the AI panel edge to %d, want %d:\n%s", got, edge+20, screen(d))
		}
		return d
	})
}

// A hover moves the cursor to the row under the pointer (the ▎ bar), also
// with the AI panel open.
func TestWelcomeCardHoverWithAIPanel(t *testing.T) {
	d := New(t, welcomeOpts(160))
	openAIPanel(d)
	x, y, ok := findIn(d, "Clone repository", 0, aiPanelEdge(d))
	if !ok {
		t.Fatalf("no Clone card:\n%s", screen(d))
	}
	d.Move(x+1, y)
	line := strings.Split(screen(d), "\n")[y]
	if !strings.Contains(line, "▎  Clone repository") && !strings.Contains(line, ">  Clone repository") {
		t.Fatalf("hovered card not lit, row %d: %q\n%s", y, line, screen(d))
	}
}

// writeRecents seeds recents.json in the sandboxed config dir.
func writeRecents(t testing.TB, dir string, rel ...string) {
	t.Helper()
	type entry struct {
		Path     string    `json:"path"`
		OpenedAt time.Time `json:"opened_at"`
	}
	var es []entry
	for _, r := range rel {
		es = append(es, entry{filepath.Join(dir, r), time.Now()})
	}
	b, err := json.Marshal(es)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "recents.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A click on a recent file opens it, also with the AI panel open.
func TestWelcomeRecentFileClickWithAIPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, dir string) { writeRecents(t, dir, "b.txt") }})
	openAIPanel(d)
	clickCard(d, "b.txt", aiPanelEdge(d))
	if !waitTabs(d, "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("recent b.txt not opened, tabs %v:\n%s", got, screen(d))
	}
}

// "New terminal" needs nvim (the terminal runs inside it).
func TestWelcomeOpenShellClickWithAIPanel(t *testing.T) {
	d := editorDriver(t, nil)
	openAIPanel(d)
	clickCard(d, "New terminal", aiPanelEdge(d))
	if !d.WaitForText("Terminal ›", 5*time.Second) {
		t.Fatalf("New terminal click did not open the terminal:\n%s", screen(d))
	}
}

// More recent files than the page lists: the "All N recent files…" row
// opens the recent files picker (the same as Ctrl+R).
func TestWelcomeAllRecentRowOpensPicker(t *testing.T) {
	d := New(t, Options{W: 160, H: 40, Files: textFiles(),
		Setup: func(t testing.TB, dir string) {
			writeRecents(t, dir, "a.txt", "b.txt", "c.txt", "notes/todo.md", "notes/done.md", "x.txt", "y.txt")
		}})
	if !d.WaitForText("All 7 recent files", 2*time.Second) {
		t.Fatalf("no All… row:\n%s", screen(d))
	}
	clickCard(d, "All 7 recent files", d.W)
	if !d.WaitForText("Open Recent File", 2*time.Second) {
		t.Fatalf("All… row did not open the recent files picker:\n%s", screen(d))
	}
}
