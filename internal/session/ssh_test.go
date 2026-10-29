//go:build unix

package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

// envFakeRemote makes the test binary play the far side's
// `termocode --link` (fakeRemote); its value is the rig folder.
const envFakeRemote = "TERMOCODE_TEST_FAKE_REMOTE"

// fakeRemote is a far-side termocode for the ssh tests: it announces the
// link, prints its arguments, answers pings (unless the rig has a
// "freeze" file), echoes keys as "got:<keys>", exits 3 on "q" and 255
// (like ssh on a lost link) on "D".
func fakeRemote() int {
	dir := os.Getenv(envFakeRemote)
	if st, err := term.MakeRaw(0); err == nil {
		defer func() { _ = term.Restore(0, st) }()
	}
	fmt.Print(linkHello)
	fmt.Printf("remote-args:%s\r\n", strings.Join(os.Args[1:], " "))
	pings := newSeqFilter(linkPing)
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0
		}
		k, found := pings.Filter(buf[:n])
		if _, err := os.Stat(filepath.Join(dir, "freeze")); err == nil {
			select {} // a frozen link: nothing comes back
		}
		for range found {
			fmt.Print(linkPong)
			if f, err := os.OpenFile(filepath.Join(dir, "pings"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.WriteString("ping\n")
				_ = f.Close()
			}
		}
		switch {
		case bytes.Contains(k, []byte("q")):
			return 3
		case bytes.Contains(k, []byte("D")):
			return 255
		case len(k) > 0:
			fmt.Printf("got:%s\r\n", k)
		}
	}
}

// fakeSSH is a folder with an ssh script that logs its arguments
// (argv, one line per run), counts runs, fails like an unreachable host
// while a "fail" file exists, like a far side without termocode while a
// "missing" file exists, asks for a password once when a "prompt"
// file exists, and runs the remote command (after --) as the fake far
// side.
type fakeSSH struct {
	t   *testing.T
	dir string
	ssh string
}

func newFakeSSH(t *testing.T) *fakeSSH {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
d='` + dir + `'
echo "$*" >> "$d/argv"
if [ -e "$d/fail" ]; then echo "ssh: connect to host box port 22: Network is unreachable"; exit 255; fi
if [ -e "$d/missing" ]; then echo "sh: 1: termocode: not found"; exit 127; fi
if [ -e "$d/prompt" ]; then rm "$d/prompt"; printf "me@box's password: "; read pw; echo "$pw" > "$d/pw"; fi
while [ "$1" != "--" ]; do shift; done; shift
exec sh -c "$*"
`
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envFakeRemote, dir)
	return &fakeSSH{t: t, dir: dir, ssh: ssh}
}

func (f *fakeSSH) runs() []string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "argv"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (f *fakeSSH) set(name string, on bool) {
	p := filepath.Join(f.dir, name)
	if on {
		_ = os.WriteFile(p, nil, 0o644)
	} else {
		_ = os.Remove(p)
	}
}

// run starts RunSSH with o (In, Out, SSH and Remote are set here) and
// returns the key pipe, the output file and the exit code channel.
func (f *fakeSSH) run(o SSHOpts) (*os.File, string, <-chan int) {
	f.t.Helper()
	in, inW, err := os.Pipe()
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = in.Close(); _ = inW.Close() })
	outPath := filepath.Join(f.dir, "out")
	out, err := os.Create(outPath)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = out.Close() })
	o.In, o.Out, o.SSH, o.Remote = in, out, f.ssh, os.Args[0]
	if o.Args == nil {
		o.Args = []string{"me@box"}
	}
	code := make(chan int, 1)
	go func() { code <- RunSSH(o) }()
	return inW, outPath, code
}

// TestRunSSHRelays: termocode ssh starts the far side's termocode --link
// in the given folder, relays keys and output, hides the link strings,
// and ends with the far side's exit code.
func TestRunSSHRelays(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{Dir: "/srv/app"})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:--link /srv/app") })
	_, _ = keys.WriteString("hello")
	waitFor(t, "the echo", func() bool { return strings.Contains(readOut(out), "got:hello") })
	_, _ = keys.WriteString("q")
	if got := waitCode(t, code); got != 3 {
		t.Errorf("exit code %d, want the far side's 3", got)
	}
	if got := readOut(out); strings.Contains(got, "termocode:link") {
		t.Errorf("the link hello reached the terminal: %q", got)
	}
	if runs := f.runs(); len(runs) != 1 || !strings.Contains(runs[0], "-t ") || !strings.Contains(runs[0], "me@box -- ") {
		t.Errorf("ssh runs: %q", runs)
	}
}

// TestRunSSHReconnects: the link drops (ssh exits 255); termocode ssh
// keeps the screen, shows the offline line and runs ssh again with the
// same arguments, telling the far side it reconnected.
func TestRunSSHReconnects(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{Args: []string{"-p", "2222", "me@box"}, Reconnect: true})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:--link") })
	_, _ = keys.WriteString("D")
	waitFor(t, "the second connection", func() bool { return strings.Contains(readOut(out), "remote-args:--link --reconnected=") })
	runs := f.runs()
	if len(runs) != 2 || !strings.HasPrefix(runs[1], strings.Split(runs[0], " -- ")[0]) {
		t.Errorf("ssh runs: %q", runs)
	}
	if got := readOut(out); !strings.Contains(got, "Reconnecting") || strings.Contains(got, "\x1b[?1049l") {
		t.Errorf("no offline line, or the screen was left: %q", got)
	}
	_, _ = keys.WriteString("q")
	if got := waitCode(t, code); got != 3 {
		t.Errorf("exit code %d, want 3", got)
	}
}

// TestRunSSHOfflineCtrlC: while the far side cannot be reached, attempts
// go on with a growing wait; keys are not sent, and Ctrl+C leaves.
func TestRunSSHOfflineCtrlC(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })
	f.set("fail", true)
	_, _ = keys.WriteString("D")
	waitFor(t, "a failed attempt", func() bool { return strings.Contains(readOut(out), "(attempt 2)") })
	_, _ = keys.WriteString("typed")
	_, _ = keys.WriteString("\x03")
	if got := waitCode(t, code); got != 0 {
		t.Errorf("exit code %d, want 0", got)
	}
	got := readOut(out)
	if !strings.Contains(got, "session keeps running") {
		t.Errorf("no goodbye: %q", got)
	}
	if strings.Contains(got, "got:typed") {
		t.Errorf("a key typed offline was sent")
	}
	if n := len(f.runs()); n > 3 {
		t.Errorf("%d ssh runs in about a second, want a wait between them", n)
	}
}

// TestRunSSHFirstConnectFails: a destination that never answered (a
// typo, a wrong password) is not retried; ssh's code comes back.
func TestRunSSHFirstConnectFails(t *testing.T) {
	f := newFakeSSH(t)
	f.set("fail", true)
	_, out, code := f.run(SSHOpts{Reconnect: true})
	if got := waitCode(t, code); got != 255 {
		t.Errorf("exit code %d, want ssh's 255", got)
	}
	if n := len(f.runs()); n != 1 {
		t.Errorf("%d ssh runs, want 1", n)
	}
	if got := readOut(out); !strings.Contains(got, "Network is unreachable") || strings.Contains(got, "Reconnecting") {
		t.Errorf("output %q", got)
	}
}

// TestRunSSHNoReconnect: with auto_reconnect off a dropped link ends
// termocode ssh with a word on how to go back.
func TestRunSSHNoReconnect(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })
	_, _ = keys.WriteString("D")
	if got := waitCode(t, code); got != 255 {
		t.Errorf("exit code %d, want 255", got)
	}
	if got := readOut(out); !strings.Contains(got, "connection dropped") {
		t.Errorf("output %q", got)
	}
	if n := len(f.runs()); n != 1 {
		t.Errorf("%d ssh runs, want 1", n)
	}
}

// TestRunSSHHeartbeat: a far side that answers the pings keeps its link;
// one that stops answering (a frozen network) is given up after Dead,
// and termocode ssh connects again by itself.
func TestRunSSHHeartbeat(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true, Interval: 50 * time.Millisecond, Dead: 400 * time.Millisecond})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })
	waitFor(t, "ten pings", func() bool {
		b, _ := os.ReadFile(filepath.Join(f.dir, "pings"))
		return bytes.Count(b, []byte("ping")) >= 10
	})
	if n := len(f.runs()); n != 1 {
		t.Fatalf("an answering link was dropped: %d ssh runs", n)
	}
	f.set("freeze", true)
	_, _ = keys.WriteString("x")
	waitFor(t, "the second connection", func() bool { return len(f.runs()) == 2 })
	f.set("freeze", false)
	waitFor(t, "the far side again", func() bool { return strings.Contains(readOut(out), "--reconnected=") })
	_, _ = keys.WriteString("q")
	if got := waitCode(t, code); got != 3 {
		t.Errorf("exit code %d, want 3", got)
	}
}

// TestRunSSHPasswordPrompt: when ssh asks for a password, first or on a
// reconnect, the question shows and the keys go to ssh; termocode never
// keeps them.
func TestRunSSHPasswordPrompt(t *testing.T) {
	f := newFakeSSH(t)
	f.set("prompt", true)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	waitFor(t, "the first question", func() bool { return strings.Contains(readOut(out), "me@box's password: ") })
	_, _ = keys.WriteString("first-secret\r")
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })

	f.set("prompt", true)
	_, _ = keys.WriteString("D")
	waitFor(t, "the question on reconnect", func() bool { return strings.Count(readOut(out), "me@box's password: ") == 2 })
	_, _ = keys.WriteString("second-secret\r")
	waitFor(t, "the far side again", func() bool { return strings.Contains(readOut(out), "--reconnected=") })
	b, _ := os.ReadFile(filepath.Join(f.dir, "pw"))
	if strings.TrimSpace(string(b)) != "second-secret" {
		t.Errorf("ssh got %q, want the typed answer", b)
	}
	_, _ = keys.WriteString("q")
	waitCode(t, code)
}

// TestRunSSHGoneWhileReconnecting: the far side answers again but has no
// termocode any more; termocode ssh ends with that code, ssh's message
// shown and the terminal back to normal.
func TestRunSSHGoneWhileReconnecting(t *testing.T) {
	f := newFakeSSH(t)
	keys, out, code := f.run(SSHOpts{Reconnect: true})
	waitFor(t, "the far side", func() bool { return strings.Contains(readOut(out), "remote-args:") })
	f.set("missing", true)
	_, _ = keys.WriteString("D")
	if got := waitCode(t, code); got != 127 {
		t.Errorf("exit code %d, want 127", got)
	}
	got := readOut(out)
	i := strings.Index(got, "termocode: not found")
	if i < 0 || !strings.Contains(got[i:], "\x1b[?1049l") {
		t.Errorf("message or terminal reset missing: %q", got)
	}
}
