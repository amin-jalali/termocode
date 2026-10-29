//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
)

// moreSSH is newFakeSSH with more switches: a "slow" file makes ssh
// wait before it connects (a slow network), a "last" file makes it fail
// with a message, a "sizes" file makes the far side write its terminal
// size to a file again and again.
func moreSSH(t *testing.T) *fakeSSH {
	t.Helper()
	f := newFakeSSH(t)
	script := `#!/bin/sh
d='` + f.dir + `'
echo "$*" >> "$d/argv"
if [ -e "$d/slow" ]; then echo "connecting"; while [ -e "$d/slow" ]; do sleep 0.02; done; fi
if [ -e "$d/last" ]; then echo "bye-from-ssh"; exit 4; fi
if [ -e "$d/sizes" ]; then echo "remote-args:"; while :; do stty size > "$d/size.tmp" && mv "$d/size.tmp" "$d/size"; sleep 0.02; done; fi
if [ -e "$d/fail" ]; then echo "ssh: connect to host box port 22: Network is unreachable"; exit 255; fi
while [ "$1" != "--" ]; do shift; done; shift
exec sh -c "$*"
`
	if err := os.WriteFile(f.ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func farSide(t *testing.T, out string) {
	t.Helper()
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })
}

// Keys typed while the link is down are dropped, and the far side is told
// so on the reconnect (it shows a note).
func TestRunSSHTellsAboutDroppedKeys(t *testing.T) {
	f := moreSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	farSide(t, out)
	f.set("fail", true)
	_, _ = keys.WriteString("D")
	waitFor(t, "a failed attempt", func() bool { return strings.Contains(readOut(out), "(attempt 2)") })
	_, _ = keys.WriteString("typed while offline")
	// The wait before attempt 3 takes the keys.
	waitFor(t, "the next attempt", func() bool { return strings.Contains(readOut(out), "(attempt 3)") })
	f.set("fail", false)
	waitFor(t, "the far side again", func() bool { return strings.Contains(readOut(out), ",dropped") })
	if strings.Contains(readOut(out), "got:typed") {
		t.Error("a key typed offline was sent")
	}
	_, _ = keys.WriteString("q")
	if c := waitCode(t, code); c != 3 {
		t.Fatalf("code %d", c)
	}
}

// Ctrl+C while ssh is still connecting again leaves at once.
func TestRunSSHQuitWhileConnecting(t *testing.T) {
	f := moreSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	farSide(t, out)
	f.set("slow", true)
	t.Cleanup(func() { f.set("slow", false) })
	_, _ = keys.WriteString("D")
	waitFor(t, "the slow attempt", func() bool { return strings.Count(readOut(out), "connecting") >= 1 })
	_, _ = keys.WriteString("x") // dropped
	_, _ = keys.WriteString("\x03")
	if c := waitCode(t, code); c != 0 {
		t.Fatalf("code %d", c)
	}
	if got := readOut(out); !strings.Contains(got, "session keeps running") {
		t.Fatalf("output %q", got)
	}
}

// The terminal goes away (its window closed) while ssh runs, or while
// termocode waits to reconnect: termocode ssh ends.
func TestRunSSHTerminalGone(t *testing.T) {
	f := moreSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	farSide(t, out)
	_ = keys.Close()
	waitCode(t, code)

	f = moreSSH(t)
	keys, out, code = f.run(SSHOpts{Reconnect: true})
	farSide(t, out)
	f.set("fail", true)
	_, _ = keys.WriteString("D")
	waitFor(t, "a failed attempt", func() bool { return strings.Contains(readOut(out), "(attempt 2)") })
	_ = keys.Close()
	if c := waitCode(t, code); c != 0 {
		t.Fatalf("code %d", c)
	}
	if got := readOut(out); !strings.Contains(got, "session keeps running") {
		t.Fatalf("output %q", got)
	}
}

// ssh fails before the far side answers: its last words still show and
// its exit code comes back.
func TestRunSSHShowsLastWords(t *testing.T) {
	f := moreSSH(t)
	f.set("last", true)
	_, out, code := f.run(SSHOpts{})
	if c := waitCode(t, code); c != 4 {
		t.Fatalf("code %d, want ssh's 4", c)
	}
	if got := readOut(out); !strings.Contains(got, "bye-from-ssh") {
		t.Fatalf("output %q", got)
	}
}

func TestRunSSHProgramMissing(t *testing.T) {
	f := moreSSH(t)
	f.ssh = filepath.Join(f.dir, "no-such-ssh")
	_, _, code := f.run(SSHOpts{})
	if c := waitCode(t, code); c != 1 {
		t.Fatalf("code %d, want 1", c)
	}
}

// Without -ssh and -remote set, "ssh" and "termocode" are found on PATH.
func TestRunSSHDefaultsUsePath(t *testing.T) {
	f := moreSSH(t)
	if err := os.Symlink(os.Args[0], filepath.Join(f.dir, "termocode")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", f.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	in, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer inW.Close()
	outPath := filepath.Join(f.dir, "out")
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	code := make(chan int, 1)
	go func() { code <- RunSSH(SSHOpts{Args: []string{"me@box"}, Dir: "~", In: in, Out: out}) }()
	farSide(t, outPath)
	if got := readOut(outPath); !strings.Contains(got, "remote-args:--link") {
		t.Fatalf("output %q", got)
	}
	_, _ = inW.WriteString("q")
	if c := waitCode(t, code); c != 3 {
		t.Fatalf("code %d", c)
	}
	if runs := f.runs(); len(runs) != 1 || !strings.HasSuffix(runs[0], "termocode --link ~") {
		t.Fatalf("ssh runs %q", runs)
	}
}

// Signals: a resize goes to ssh's terminal; SIGTERM ends with the
// terminal put back, SIGHUP (no terminal left) without. While waiting to
// reconnect a resize redraws the offline line and SIGTERM leaves.
func TestRunSSHSignals(t *testing.T) {
	guardSignals(t)
	t.Run("resize", func(t *testing.T) {
		f := moreSSH(t)
		f.set("sizes", true)
		tty := openTerm(t, 90, 20)
		code := make(chan int, 1)
		// In and Out are a terminal: raw mode is set and undone, and
		// the size comes from it.
		go func() {
			code <- RunSSH(SSHOpts{SSH: f.ssh, Remote: os.Args[0], Args: []string{"me@box"}, In: tty, Out: tty})
		}()
		size := func() string {
			b, _ := os.ReadFile(filepath.Join(f.dir, "size"))
			return strings.TrimSpace(string(b))
		}
		waitFor(t, "ssh's first size", func() bool { return size() == "20 90" })
		_ = pty.Setsize(tty, &pty.Winsize{Cols: 120, Rows: 40})
		waitFor(t, "the new size", func() bool {
			_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
			return size() == "40 120"
		})
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		if c := waitCode(t, code); c != 0 {
			t.Fatalf("code %d", c)
		}
	})
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			f := moreSSH(t)
			_, out, code := f.run(SSHOpts{})
			farSide(t, out)
			_ = syscall.Kill(os.Getpid(), sig)
			if c := waitCode(t, code); c != 0 {
				t.Fatalf("code %d", c)
			}
			reset := strings.Contains(readOut(out), "\x1b[?1049l")
			if reset != (sig == syscall.SIGTERM) {
				t.Fatalf("reset = %v for %v", reset, sig)
			}
		})
	}
	t.Run("while waiting", func(t *testing.T) {
		f := moreSSH(t)
		keys, out, code := f.run(SSHOpts{Reconnect: true})
		farSide(t, out)
		f.set("fail", true)
		_, _ = keys.WriteString("D")
		waitFor(t, "a failed attempt", func() bool { return strings.Contains(readOut(out), "(attempt 2)") })
		before := strings.Count(readOut(out), "Reconnecting")
		_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
		waitFor(t, "the redraw", func() bool { return strings.Count(readOut(out), "Reconnecting") > before })
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		if c := waitCode(t, code); c != 0 {
			t.Fatalf("code %d", c)
		}
		if !strings.Contains(readOut(out), "session keeps running") {
			t.Fatalf("output %q", readOut(out))
		}
	})
}

func TestRemotePathHome(t *testing.T) {
	if got := remotePath("~"); got != "~" {
		t.Fatalf("got %q", got)
	}
}
