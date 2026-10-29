//go:build unix

package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// while a "fail" file exists, asks for a password once when a "prompt"
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
