//go:build unix

package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webgw"
)

// fakeFwd records the forwards a webForward starts. A proc lives until
// it is killed, or ends at once while dieFast is set.
type fakeFwd struct {
	mu      sync.Mutex
	argv    [][]string
	opened  []string
	pages   []string // the redirect page as it was when opened
	killed  int
	dieFast bool
}

type fakeProc struct {
	f    *fakeFwd
	done chan struct{}
	once sync.Once
}

func (p *fakeProc) Wait() error { <-p.done; return errors.New("exit status 255") }

func (p *fakeProc) Kill() {
	p.once.Do(func() {
		p.f.mu.Lock()
		p.f.killed++
		p.f.mu.Unlock()
		close(p.done)
	})
}

func newTestForward(t *testing.T) (*webForward, *fakeFwd) {
	t.Helper()
	ff := &fakeFwd{}
	f := newWebForward("ssh", []string{"-p", "2222", "me@box"}, shortTemp(t))
	f.start = func(name string, argv []string) (webFwdProc, error) {
		ff.mu.Lock()
		defer ff.mu.Unlock()
		if name != "ssh" {
			t.Errorf("started %q", name)
		}
		ff.argv = append(ff.argv, argv)
		p := &fakeProc{f: ff, done: make(chan struct{})}
		if ff.dieFast {
			close(p.done)
		}
		return p, nil
	}
	f.open = func(path string) error {
		b, _ := os.ReadFile(path)
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("redirect page %s: %v %v", path, fi, err)
		}
		ff.mu.Lock()
		ff.opened = append(ff.opened, path)
		ff.pages = append(ff.pages, string(b))
		ff.mu.Unlock()
		return nil
	}
	f.ready = func(int, time.Duration) bool { return true }
	f.backoff = func(int) time.Duration { return 0 }
	return f, ff
}

func (ff *fakeFwd) snapshot() (argv [][]string, opened, pages []string, killed int) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return append([][]string(nil), ff.argv...), append([]string(nil), ff.opened...), append([]string(nil), ff.pages...), ff.killed
}

func waitCond(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Spec (WEB-29): the first hand-off starts one forward ssh with the
// user's arguments and opens the one-time link here through a 0600 page
// (the opener gets a path, never the secret). A later hand-off for the
// same port and socket (a reconnect) neither starts a second forward nor
// opens a second tab. Close kills the forward and removes the page.
func TestWebForwardOpensOnce(t *testing.T) {
	f, ff := newTestForward(t)
	h := session.WebHandoff{Port: 41237, Sock: "/run/user/1/termocode/k.webhttp.sock", Secret: "s3cret-one"}
	f.Handoff(h)
	waitCond(t, "the browser", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	argv, opened, pages, _ := ff.snapshot()
	if want := session.SSHForwardArgv([]string{"-p", "2222", "me@box"}, 41237, h.Sock); len(argv) != 1 || !reflect.DeepEqual(argv[0], want) {
		t.Fatalf("forward argv %q, want %q", argv, want)
	}
	if strings.Contains(opened[0], h.Secret) || !strings.Contains(pages[0], webgw.LaunchURL(41237, h.Secret)) {
		t.Fatalf("opened %q with page %q", opened[0], pages[0])
	}

	f.Handoff(session.WebHandoff{Port: 41237, Sock: h.Sock, Secret: "s3cret-two"})
	notes := f.Close()
	argv, opened, _, killed := ff.snapshot()
	if len(argv) != 1 || len(opened) != 1 {
		t.Errorf("after a reconnect: %d forwards, %d tabs; want 1 and 1", len(argv), len(opened))
	}
	if killed != 1 {
		t.Errorf("killed %d forwards at the end, want 1", killed)
	}
	if _, err := os.Stat(opened[0]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the redirect page is left: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes %q", notes)
	}
}

// Spec (E7): a forward that keeps dying is started again three times,
// then termocode ssh says why once it is done; no browser opens to a
// dead port.
func TestWebForwardRestartsThenE7(t *testing.T) {
	f, ff := newTestForward(t)
	ff.dieFast = true
	f.ready = func(int, time.Duration) bool { return false }
	f.Handoff(session.WebHandoff{Port: 41237, Sock: "/s.sock", Secret: "x"})
	waitCond(t, "four tries", func() bool { a, _, _, _ := ff.snapshot(); return len(a) >= 4 })
	notes := f.Close()
	argv, opened, _, _ := ff.snapshot()
	if len(argv) != 4 {
		t.Errorf("%d forwards, want 1 and 3 restarts", len(argv))
	}
	if len(opened) != 0 {
		t.Errorf("a browser opened to a dead forward")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "SSH could not forward the web panel") ||
		!strings.Contains(notes[0], "AllowStreamLocalForwarding") {
		t.Errorf("notes %q", notes)
	}
}

// Spec: the far side's reason (the panel is off there) is said once the
// session ends; nothing is started.
func TestWebForwardFarError(t *testing.T) {
	f, ff := newTestForward(t)
	f.Handoff(session.WebHandoff{Err: "The web panel is off on the far computer."})
	notes := f.Close()
	argv, _, _, _ := ff.snapshot()
	if len(argv) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "The web panel is off on the far computer.") {
		t.Errorf("forwards %d, notes %q", len(argv), notes)
	}
}
