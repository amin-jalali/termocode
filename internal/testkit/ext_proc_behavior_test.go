//go:build unix

package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/extproc/extproctest"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// TestMain lets this test binary also be the fake Tier 2 extension
// (extproctest) and the fake MCP server (mcptest): the app starts them as
// separate processes. It also puts fail-closed docker / podman / kubectl
// stand-ins first on PATH: no test reaches the real Docker or cluster.
// After the run it fails when a test left an nvim running.
func TestMain(m *testing.M) {
	extproctest.RunIfHelper()
	git.RunRebaseHelperIfAsked() // git's editor in the rebase tests (scm_rebase_test.go)
	mcptest.RunIfHelper()
	os.Exit(runGuarded(m))
}

func runGuarded(m *testing.M) int {
	dir, err := os.MkdirTemp("", "termocode-cli-guard-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	g, err := servicestest.InstallGuard(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Setenv("PATH", g.PathEnv())
	cliGuard = g
	nvim.TrackClients()
	code := m.Run()
	if r := nvim.LeakReport(15 * time.Second); code == 0 && r != "" {
		fmt.Fprint(os.Stderr, "FAIL: "+r)
		code = 1
	}
	return code
}

// procWait is only a guard against a hang: every wait ends on what the
// screen shows.
const procWait = 20 * time.Second

func procDriver(t *testing.T, extra map[string]any) *Driver {
	t.Helper()
	return New(t, Options{Config: map[string]string{
		"extensions/fake/extension.json": extproctest.Manifest(extra),
	}})
}

// fakePid is the pid in the fake extension's status item ("fake pid N")
// on a fresh frame.
func fakePid(d *Driver) int {
	d.render()
	s := screen(d)
	i := strings.Index(s, "fake pid ")
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+len("fake pid "):])
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

func procAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	st := strings.TrimSpace(string(out))
	return err == nil && st != "" && !strings.HasPrefix(st, "Z")
}

// N6b: a process extension (any language, JSON-RPC over stdio) works
// like a Lua one: its command is in the palette, running it shows its
// toast, its status item and panel appear; a crash restarts it, and
// quitting termocode leaves no process behind.
func TestProcessExtensionBehavior(t *testing.T) {
	d := procDriver(t, nil)

	// Status item (registered at runtime by the process).
	if !d.WaitFor(func() bool { return fakePid(d) > 0 }, procWait) {
		t.Fatalf("no status item from the process extension:\n%s", screen(d))
	}
	pid := fakePid(d)
	if !procAlive(pid) {
		t.Fatalf("status item pid %d is not running", pid)
	}

	// Palette row.
	d.Key("f1")
	d.Type("Say Hello")
	if !d.WaitForText("Ext: Say Hello", procWait) {
		t.Fatalf("palette has no \"Ext: Say Hello\":\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitForText("Hello from fake", procWait) {
		t.Fatalf("running the command showed no toast:\n%s", screen(d))
	}

	// Panel: its activity-bar item opens the panel with its markup lines.
	x, y, ok := findIn(d, "Z", 0, activity.Width)
	if !ok {
		t.Fatalf("no activity-bar item \"Z\":\n%s", screen(d))
	}
	d.Click(x, y)
	if !d.WaitForText("line one", procWait) {
		t.Fatalf("the panel does not show the extension's lines:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "{{accent}}") {
		t.Errorf("panel shows raw markup:\n%s", screen(d))
	}
	// A click on a panel row goes to the extension.
	clickText(d, "line two")
	if !d.WaitForText("selected 2", procWait) {
		t.Errorf("a panel click did not reach the extension:\n%s", screen(d))
	}

	// Crash → restart: a new process registers its status item again.
	d.Palette("ext-fake.crash")
	if !d.WaitFor(func() bool { p := fakePid(d); return p > 0 && p != pid }, procWait) {
		t.Fatalf("no restart after the crash (old pid %d):\n%s", pid, screen(d))
	}
	if !strings.Contains(screen(d), "restart") {
		t.Errorf("no toast about the crash and restart:\n%s", screen(d))
	}
	pid2 := fakePid(d)

	// Exit: no process left.
	m := d.Model()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(procWait)
	for procAlive(pid2) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if procAlive(pid2) {
		t.Fatalf("extension process %d still runs after Close", pid2)
	}
}

// A lazy extension (commands declared in extension.json) shows its
// command before it runs, and the first run starts it.
func TestProcessExtensionLazyStart(t *testing.T) {
	d := procDriver(t, map[string]any{
		"contributes": map[string]any{"commands": []map[string]string{{"id": "hello", "title": "Say Hello"}}},
	})
	d.Settle(200 * time.Millisecond)
	if fakePid(d) != 0 {
		t.Fatalf("a lazy extension started at once:\n%s", screen(d))
	}
	d.Key("f1")
	d.Type("Say Hello")
	if !d.WaitForText("Ext: Say Hello", procWait) {
		t.Fatalf("contributed command not in the palette:\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitForText("Hello from fake", procWait) {
		t.Fatalf("first run did not start the extension:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return fakePid(d) > 0 }, procWait) {
		t.Errorf("no status item after the lazy start:\n%s", screen(d))
	}
}

// Editor events reach a process extension: opening and saving a file.
func TestProcessExtensionEventsNvim(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(), Config: map[string]string{
		"extensions/fake/extension.json": extproctest.Manifest(nil),
	}})
	if !d.WaitFor(func() bool { return fakePid(d) > 0 }, procWait) {
		t.Fatalf("extension did not start:\n%s", screen(d))
	}
	open(d, "a.txt")
	if !d.WaitForText("event open a.txt", procWait) {
		t.Errorf("no open event:\n%s", screen(d))
	}
	d.Key("ctrl+s")
	if !d.WaitForText("event save a.txt", procWait) {
		t.Errorf("no save event:\n%s", screen(d))
	}
}

// prompt from a process extension: the answer goes back to the process.
func TestProcessExtensionPrompt(t *testing.T) {
	d := procDriver(t, nil)
	if !d.WaitFor(func() bool { return fakePid(d) > 0 }, procWait) {
		t.Fatalf("extension did not start:\n%s", screen(d))
	}
	d.Palette("ext-fake.ask")
	if !d.WaitForText("Your name:", procWait) {
		t.Fatalf("no prompt:\n%s", screen(d))
	}
	d.Type("Bob")
	d.Key("enter")
	if !d.WaitForText("Hello, Bob", procWait) {
		t.Fatalf("the answer did not reach the extension:\n%s", screen(d))
	}
}
