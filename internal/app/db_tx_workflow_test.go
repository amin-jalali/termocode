package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// manualMode switches the connection under the tree cursor to manual
// transactions with t.
func manualMode(t *testing.T, d *testkit.Driver) {
	t.Helper()
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("g") // the connection row
	d.Key("t")
	if !d.WaitForText("Transaction mode for local", time.Second) {
		t.Fatalf("no mode picker:\n%s", ansi.Strip(d.Frame()))
	}
	d.Type("Manual")
	d.Key("enter")
	if !d.WaitForText("Manual transactions on local", time.Second) {
		t.Fatalf("mode not set:\n%s", ansi.Strip(d.Frame()))
	}
}

// consoleRun puts sql in the connection's console and sends it; a write
// gets its guard card, answered with Run.
func consoleRun(t *testing.T, d *testkit.Driver, sql string, write bool) {
	t.Helper()
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("g", "n")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 2*time.Second) {
		t.Fatalf("no console: %s", dbBufName(t, d))
	}
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0,0,-1,false,{'-- @conn local',...}); vim.cmd('stopinsert'); vim.cmd('silent write'); vim.api.nvim_win_set_cursor(0,{2,0}); return ''`, sql)
	d.Key("esc")
	dbSend(d)
	if write {
		if !d.WaitForText("Run this statement on local?", 2*time.Second) {
			t.Fatalf("no guard card:\n%s", ansi.Strip(d.Frame()))
		}
		d.Key("right", "enter")
	}
}

func chip(d *testkit.Driver) string {
	lines := strings.Split(ansi.Strip(d.Frame()), "\n")
	return lines[len(lines)-1]
}

func TestDBTxManualModeCommitsWithK(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	manualMode(t, d)
	consoleRun(t, d, "UPDATE users SET plan = 'pro' WHERE id = 2;", true)
	if !d.WaitFor(func() bool { return strings.Contains(chip(d), "manual commit · 1 not committed") }, 2*time.Second) {
		t.Fatalf("chip lacks the transaction:\n%s", chip(d))
	}
	if got := s.log(); len(got) < 2 || got[0] != "BEGIN" || s.has("COMMIT") {
		t.Fatalf("server saw %q", got)
	}
	dbWaitPage(t, d, "manual commit · 1 not committed")
	if !strings.Contains(dbText(t, d), "K commit") {
		t.Fatalf("hints lack K:\n%s", dbText(t, d))
	}
	// BEGIN / COMMIT typed in manual mode go through K / Z instead.
	consoleRun(t, d, "COMMIT;", false)
	if !d.WaitForText("Use Commit (K) or Roll back (Z)", time.Second) {
		t.Fatalf("COMMIT was not redirected:\n%s", ansi.Strip(d.Frame()))
	}
	if s.has("COMMIT") {
		t.Fatal("a typed COMMIT reached the server")
	}
	// A second write joins the same transaction.
	consoleRun(t, d, "DELETE FROM users WHERE id = 3;", true)
	if !d.WaitFor(func() bool { return strings.Contains(chip(d), "manual commit · 2 not committed") }, 2*time.Second) {
		t.Fatalf("chip:\n%s", chip(d))
	}
	begins := 0
	for _, e := range s.log() {
		if e == "BEGIN" {
			begins++
		}
	}
	if begins != 1 {
		t.Fatalf("%d BEGINs: %q", begins, s.log())
	}
	d.Key("K")
	if !d.WaitFor(func() bool { return s.has("COMMIT") }, 2*time.Second) {
		t.Fatalf("K did not commit: %q", s.log())
	}
	if !d.WaitFor(func() bool {
		c := chip(d)
		return strings.Contains(c, "manual") && !strings.Contains(c, "not committed")
	}, 2*time.Second) {
		t.Fatalf("chip after commit:\n%s", chip(d))
	}
}

func TestDBTxDataViewEditsJoinTheTransactionAndRollBack(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	manualMode(t, d)
	d.Key("down", "down", "down")
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	setCell(t, d, "tx@example.com")
	d.Key("w")
	if !d.WaitForText("Submit 1 change to local (open transaction)", time.Second) {
		t.Fatalf("card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { r, _ := s.row("2"); return r[1] == "tx@example.com" }, 2*time.Second) {
		t.Fatalf("not applied: %q", s.log())
	}
	if s.has("COMMIT") {
		t.Fatal("manual mode committed on its own")
	}
	if !s.has("SAVEPOINT termocode_edit") || !s.has("RELEASE SAVEPOINT termocode_edit") {
		t.Fatalf("no savepoint: %q", s.log())
	}
	// The reload reads inside the transaction (it sees the change).
	dbWaitPage(t, d, "tx@example.com")
	if !d.WaitFor(func() bool { return strings.Contains(chip(d), "manual commit · 1 not committed") }, 2*time.Second) {
		t.Fatalf("chip:\n%s", chip(d))
	}
	d.Key("Z")
	if !d.WaitFor(func() bool { r, _ := s.row("2"); return r[1] == "user-002@example.com" }, 2*time.Second) {
		t.Fatalf("Z did not roll back: %q", s.log())
	}
	dbWaitPage(t, d, "user-002@example.com")
}

func TestDBTxQuitAsksAboutUncommittedChanges(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	manualMode(t, d)
	consoleRun(t, d, "UPDATE users SET plan = 'pro' WHERE id = 2;", true)
	if !d.WaitFor(func() bool { return strings.Contains(chip(d), "1 not committed") }, 2*time.Second) {
		t.Fatalf("chip:\n%s", chip(d))
	}
	d.Key("ctrl+q")
	if !d.WaitForText("local has 1 uncommitted change", time.Second) {
		t.Fatalf("no quit card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if d.Quit() {
		t.Fatal("Cancel quit")
	}
	d.Key("ctrl+q")
	d.WaitForText("local has 1 uncommitted change", time.Second)
	d.Key("right", "enter") // Roll back
	if !d.WaitFor(func() bool { return d.Quit() }, 3*time.Second) {
		t.Fatalf("did not quit after Roll back:\n%s", ansi.Strip(d.Frame()))
	}
	if !s.has("ROLLBACK") || s.has("COMMIT") {
		t.Fatalf("server saw %q", s.log())
	}
}

func TestDBTxDisconnectAndModeSwitchAsk(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	manualMode(t, d)
	consoleRun(t, d, "UPDATE users SET plan = 'pro' WHERE id = 2;", true)
	d.WaitFor(func() bool { return strings.Contains(chip(d), "1 not committed") }, 2*time.Second)
	// Back to auto-commit: the card first; Commit ends the transaction.
	d.Key("f1")
	d.Type("Databases: Transaction Mode")
	d.Key("enter")
	d.WaitForText("Transaction mode for local", time.Second)
	d.Type("Auto")
	d.Key("enter")
	if !d.WaitForText("local has 1 uncommitted change", time.Second) {
		t.Fatalf("no card on the mode switch:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter") // Commit
	if !d.WaitForText("Auto-commit on local", 2*time.Second) || !s.has("COMMIT") {
		t.Fatalf("mode switch: %q\n%s", s.log(), ansi.Strip(d.Frame()))
	}
	// Manual again, a write, then x: the card names the changes.
	manualMode(t, d)
	consoleRun(t, d, "DELETE FROM users WHERE id = 4;", true)
	d.WaitFor(func() bool { return strings.Contains(chip(d), "1 not committed") }, 2*time.Second)
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("g", "x")
	if !d.WaitForText("local has 1 uncommitted change", time.Second) {
		t.Fatalf("no card on disconnect:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter") // Roll back, then disconnect
	if !d.WaitFor(func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closed > 0 }, 2*time.Second) {
		t.Fatalf("not disconnected: %q", s.log())
	}
	if l := s.log(); l[len(l)-1] != "ROLLBACK" {
		t.Fatalf("server saw %q", l)
	}
}

func TestDBTxHidingTheViewKeepsTheTransaction(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	manualMode(t, d)
	consoleRun(t, d, "UPDATE users SET plan = 'pro' WHERE id = 2;", true)
	d.WaitFor(func() bool { return strings.Contains(chip(d), "1 not committed") }, 2*time.Second)
	d.Key("ctrl+b") // the sidebar hides: the view closes its sessions
	d.Settle(300 * time.Millisecond)
	if strings.Contains(ansi.Strip(d.Frame()), "DATABASES") {
		t.Fatalf("the view still shows:\n%s", ansi.Strip(d.Frame()))
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed != 0 || s.has("ROLLBACK") {
		t.Fatalf("hiding the view ended the transaction (closed %d): %q", closed, s.log())
	}
	if !strings.Contains(chip(d), "1 not committed") {
		t.Fatalf("chip lost the transaction:\n%s", chip(d))
	}
	d.Key("f1")
	d.Type("Databases: Commit")
	d.Key("enter")
	if !d.WaitFor(func() bool { return s.has("COMMIT") }, 2*time.Second) {
		t.Fatalf("palette commit: %q", s.log())
	}
}
