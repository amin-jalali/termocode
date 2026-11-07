//go:build unix

package main

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
)

// newPortTestForward is newTestForward with a free port of its own and
// an opener that records URLs (a port preview opens an address, not a
// redirect page).
func newPortTestForward(t *testing.T) (*webForward, *fakeFwd, func() []string) {
	t.Helper()
	f, ff := newTestForward(t)
	var mu sync.Mutex
	var urls []string
	next := 50100
	f.free = func() (int, error) { next++; return next, nil }
	f.open = func(u string) error {
		mu.Lock()
		urls = append(urls, u)
		mu.Unlock()
		return nil
	}
	return f, ff, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), urls...)
	}
}

var portTestHandoff = session.WebHandoff{Port: 41237, Sock: "/run/user/1/termocode/k.webhttp.sock", Secret: "s3cret"}

// Spec (WEB-54): without a web panel handed off (no termocode ssh --web
// session), a port forward request starts nothing.
func TestPortForwardNeedsWebHandoff(t *testing.T) {
	f, ff, urls := newPortTestForward(t)
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3000, ID: "tc1"}})
	f.Close()
	if argv, _, _, _ := ff.snapshot(); len(argv) != 0 || len(urls()) != 0 {
		t.Errorf("started %q, opened %q", argv, urls())
	}
}

// Spec (WEB-54): after the hand-off a request runs one more ssh, from a
// free 127.0.0.1 port here to 127.0.0.1:<port> there, and opens that
// port's own address in the browser here. Close of the id kills it; a
// second open of the same id starts nothing.
func TestPortForwardOpensOwnOrigin(t *testing.T) {
	f, ff, urls := newPortTestForward(t)
	f.Handoff(portTestHandoff)
	waitCond(t, "the panel", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	fwd := session.PortForward{Open: true, Port: 3000, ID: "tc1"}
	f.Handoff(session.WebHandoff{Fwd: &fwd})
	f.Handoff(session.WebHandoff{Fwd: &fwd})
	waitCond(t, "the preview", func() bool { return len(urls()) == 1 })
	argv, _, _, _ := ff.snapshot()
	want := session.SSHPortForwardArgv([]string{"-p", "2222", "me@box"}, 50101, 3000)
	if len(argv) != 2 || !reflect.DeepEqual(argv[1], want) {
		t.Fatalf("argv %q, want the panel's and %q", argv, want)
	}
	if u := urls()[0]; u != "http://127.0.0.1:50101/" || strings.Contains(u, "41237") {
		t.Errorf("opened %q", u)
	}
	f.Handoff(session.WebHandoff{Fwd: &session.PortForward{ID: "tc1"}})
	waitCond(t, "the kill", func() bool { _, _, _, k := ff.snapshot(); return k == 1 })
	f.Close()
	if _, _, _, k := ff.snapshot(); k != 2 {
		t.Errorf("killed %d, want the preview and the panel", k)
	}
}

// Spec (WEB-54): at most maxPortFwds previews run; the next is refused
// with a note, and Close ends every one.
func TestPortForwardCapAndCloseAll(t *testing.T) {
	f, ff, urls := newPortTestForward(t)
	f.Handoff(portTestHandoff)
	waitCond(t, "the panel", func() bool { _, o, _, _ := ff.snapshot(); return len(o) == 1 })
	for i := range maxPortFwds + 1 {
		f.Handoff(session.WebHandoff{Fwd: &session.PortForward{Open: true, Port: 3000 + i, ID: "tc" + string(rune('a'+i))}})
	}
	waitCond(t, "the previews", func() bool { return len(urls()) == maxPortFwds })
	notes := f.Close()
	argv, _, _, killed := ff.snapshot()
	if len(argv) != 1+maxPortFwds || killed != 1+maxPortFwds {
		t.Errorf("%d started, %d killed; want %d each", len(argv), killed, 1+maxPortFwds)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "too many port previews") {
		t.Errorf("notes %q", notes)
	}
}
