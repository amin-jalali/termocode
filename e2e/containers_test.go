//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// CP-11: the Containers view in the real binary with a fake `docker` on
// PATH (a shell script that answers from the shared testdata). No real
// engine and no network are used.

const fakeDockerScript = `#!/bin/sh
D='@DATA@'
printf '%s\n' "$*" >> "$D/argv.log"
case "$1" in
  version) cat "$D/version.json" ;;
  ps) cat "$D/ps.json" ;;
  images) cat "$D/images.json" ;;
  network) cat "$D/network-ls.json" ;;
  volume) cat "$D/volume-ls.json" ;;
  context) cat "$D/context-ls.json" ;;
  events)
    echo $$ > "$D/events.pid"
    touch "$D/events.log"
    exec tail -n 0 -f "$D/events.log"
    ;;
esac
exit 0
`

// fakeDocker installs the fake first on PATH for this test and returns
// its data dir.
func fakeDocker(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	bin := t.TempDir()
	src := filepath.Join("..", "internal", "containers", "testdata", "docker27")
	for _, n := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(src, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := strings.ReplaceAll(fakeDockerScript, "@DATA@", data)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return data
}

func fileText(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func pidAlive(path string) bool {
	n, err := strconv.Atoi(strings.TrimSpace(fileText(path)))
	if err != nil || n <= 0 {
		return false
	}
	return syscall.Kill(n, 0) == nil
}

func waitUntil(t *testing.T, what string, d time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !f() {
		if time.Now().After(end) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Spec: with a fake engine the view lists the host containers, Tab jumps
// between section headers, "?" shows the keys, x stops the selected
// container with a plain `docker stop <id>`, the events stream runs only
// while the view is shown, and nothing is left behind on quit.
func TestContainersFakeEngineBrowseStopAndIdle(t *testing.T) {
	data := fakeDocker(t)
	tm := Start(t, StartOpts{Cols: 120, Rows: 36})
	tm.settle()
	tm.Palette("Containers: Focus")
	tm.WaitText("CONTAINERS", uiTimeout)
	tm.WaitText("nginx", uiTimeout)
	tm.CheckScreen("containers", mainFrame)
	waitUntil(t, "the events stream", 10*time.Second, func() bool { return pidAlive(filepath.Join(data, "events.pid")) })

	// Tab walks the headers and wraps; Shift+Tab goes back.
	tm.Send("\t")
	tm.WaitText("docker 27.5.1", uiTimeout)
	tm.Send("\t")
	tm.WaitText("images on this machine", uiTimeout)
	tm.Send("\x1b[Z")
	tm.WaitGone("images on this machine", uiTimeout)
	tm.WaitText("? keys", uiTimeout)
	tm.Send("?")
	tm.WaitText("Section keys", uiTimeout)
	tm.CheckScreen("keys help", mainFrame)

	// Down to the nginx row and stop it: a plain `stop <id>`, no -f.
	for i := 0; i < 30 && !strings.Contains(fileText(filepath.Join(data, "argv.log")), "stop aabbccdd"); i++ {
		tm.Send(KeyDown)
		tm.settle()
		if cursorFooterHas(tm, "nginx:1.27") {
			tm.Send("x")
			time.Sleep(200 * time.Millisecond)
		}
	}
	argv := fileText(filepath.Join(data, "argv.log"))
	if !strings.Contains(argv, "stop aabbccdd") {
		t.Fatalf("no `stop aabbccdd` call:\n%s\nscreen:\n%s", argv, tm.Screen())
	}
	if strings.Contains(argv, " -f ") || strings.Contains(argv, "--force") {
		t.Errorf("stop used a force flag:\n%s", argv)
	}

	// CP-12: "/" filters every section from the snapshot (no engine
	// call while typing); Esc clears it.
	tm.settle()
	calls := fileText(filepath.Join(data, "argv.log"))
	tm.Send("/")
	tm.Send("ngin")
	tm.WaitText("/ngin", uiTimeout)
	tm.WaitGone("api-1", uiTimeout)
	tm.CheckScreen("filter", mainFrame)
	if got := fileText(filepath.Join(data, "argv.log")); got != calls {
		t.Errorf("typing the filter called the engine:\n%s", strings.TrimPrefix(got, calls))
	}
	tm.Send(KeyEsc)
	tm.WaitGone("/ngin", uiTimeout)
	tm.WaitText("api-1", uiTimeout)

	// Another view hides this one: the events stream goes away.
	tm.Palette("Git: Focus Source Control")
	tm.settle()
	waitUntil(t, "the events process to end while hidden", 10*time.Second, func() bool { return !pidAlive(filepath.Join(data, "events.pid")) })
	tm.Quit()
	if pidAlive(filepath.Join(data, "events.pid")) {
		t.Error("the events process is still running after quit")
	}
}

// cursorFooterHas reports whether the two footer lines of the sidebar
// (above the status bar) name text.
func cursorFooterHas(tm *Term, text string) bool {
	ls := tm.Lines()
	for i := len(ls) - 1; i >= 0 && i >= len(ls)-5; i-- {
		if strings.Contains(ls[i], text) {
			return true
		}
	}
	return false
}
