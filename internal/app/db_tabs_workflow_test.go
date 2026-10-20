package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// pageRow1 is the first line of the result page.
func pageRow1(t *testing.T, d *testkit.Driver) string {
	t.Helper()
	return strings.SplitN(dbText(t, d), "\n", 2)[0]
}

// runFile sends every statement of the console (Databases: Run File).
func runFile(t *testing.T, d *testkit.Driver, lines ...string) {
	t.Helper()
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("g", "n")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 2*time.Second) {
		t.Fatalf("no console: %s", dbBufName(t, d))
	}
	all := append([]any{"-- @conn local"}, toAny(lines)...)
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0,0,-1,false,{...}); vim.cmd('stopinsert'); vim.cmd('silent write'); return ''`, all...)
	d.Key("esc", "f1")
	d.Type("Databases: Run File")
	d.Key("enter")
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func TestDBResultTabsPinSwitchAndReplace(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	d.Key("s")
	dbWaitPage(t, d, "LIMIT 100")
	if strings.Contains(pageRow1(t, d), "[1]") {
		t.Fatalf("one result shows a tab strip: %s", pageRow1(t, d))
	}
	d.Key("p")
	runFile(t, d, "SELECT * FROM users WHERE id = 4;")
	if !d.WaitFor(func() bool { return strings.Contains(pageRow1(t, d), "1p [2]") }, 2*time.Second) {
		t.Fatalf("tabs: %s", pageRow1(t, d))
	}
	if !strings.Contains(pageRow1(t, d), "WHERE id = 4") {
		t.Fatalf("row 1: %s", pageRow1(t, d))
	}
	d.Key("1")
	if r1 := pageRow1(t, d); !strings.Contains(r1, "[1p] 2") || !strings.Contains(r1, "LIMIT 100") {
		t.Fatalf("1 did not switch: %s", r1)
	}
	d.Key("2")
	if r1 := pageRow1(t, d); !strings.Contains(r1, "WHERE id = 4") {
		t.Fatalf("2 did not switch back: %s", r1)
	}
	// A new run replaces the unpinned tab, never the pinned one.
	runFile(t, d, "SELECT * FROM users WHERE id = 5;")
	if !d.WaitFor(func() bool {
		r := pageRow1(t, d)
		return strings.Contains(r, "1p [2]") && strings.Contains(r, "id = 5")
	}, 2*time.Second) {
		t.Fatalf("tabs: %s", pageRow1(t, d))
	}
	if strings.Contains(pageRow1(t, d), "3") {
		t.Fatalf("a third tab: %s", pageRow1(t, d))
	}
}

func TestDBResultTabsOnePerStatementAndContinue(t *testing.T) {
	s := newEditSession(5)
	s.failOn = "users_missing"
	d := editDriver(t, 120, 30, s, "")
	runFile(t, d,
		"SELECT * FROM users WHERE id = 1;",
		"UPDATE users SET plan = 'x' WHERE id = 1;",
		"SELECT * FROM users_missing;",
		"SELECT * FROM users WHERE id = 3;")
	if !d.WaitForText("Run this statement on local?", 2*time.Second) {
		t.Fatalf("no guard:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter")
	if !d.WaitForText("Statement 3 of 4 failed", 2*time.Second) {
		t.Fatalf("no continue card:\n%s", ansi.Strip(d.Frame()))
	}
	frame := ansi.Strip(d.Frame())
	if !strings.Contains(frame, "duplicate key") || !strings.Contains(frame, "Continue") {
		t.Fatalf("card:\n%s", frame)
	}
	if s.has("SELECT * FROM users WHERE id = 3") {
		t.Fatal("ran past the error before Continue")
	}
	d.Key("right", "enter") // Continue
	if !d.WaitFor(func() bool { return s.has("SELECT * FROM users WHERE id = 3") }, 2*time.Second) {
		t.Fatalf("Continue did not run the rest: %q", s.log())
	}
	// One tab per statement with rows (and the error); the UPDATE gave its
	// tab to the next statement.
	if !d.WaitFor(func() bool { return strings.Contains(pageRow1(t, d), "1 2 [3]") }, 2*time.Second) {
		t.Fatalf("tabs: %s", pageRow1(t, d))
	}
	d.Key("2")
	if !strings.Contains(dbText(t, d), "duplicate key") {
		t.Fatalf("tab 2 is not the error:\n%s", dbText(t, d))
	}
	d.Key("1")
	if !strings.Contains(pageRow1(t, d), "id = 1") {
		t.Fatalf("tab 1: %s", pageRow1(t, d))
	}
}

func TestDBResultTabWithEditsIsKeptByANewRun(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "kept@example.com")
	dbWaitPage(t, d, "1 change")
	runFile(t, d, "SELECT * FROM users WHERE id = 4;")
	if !d.WaitFor(func() bool { return strings.Contains(pageRow1(t, d), "1* [2]") }, 2*time.Second) {
		t.Fatalf("tabs: %s", pageRow1(t, d))
	}
	d.Key("1")
	if text := dbText(t, d); !strings.Contains(text, "kept@example.com") || !strings.Contains(text, "1 change") {
		t.Fatalf("the edits were lost:\n%s", text)
	}
}
