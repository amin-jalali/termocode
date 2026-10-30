//go:build unix

package app

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Spec (WEB-6): nothing listens by default: a new Model has no hub and no
// hub socket. startWebHub serves <key>.web.sock (0600); a browser
// connection works; stopWebHub (and quit) closes it and removes the
// socket; a second start for the same folder is busy and a toast.
func TestWebHubStartStop(t *testing.T) {
	m := aiTestModel(t)
	t.Setenv("XDG_RUNTIME_DIR", webTestDir(t))
	path, err := webHubSocketPath(aiWorkspaceRoot())
	if err != nil {
		t.Fatal(err)
	}
	if m.web != nil {
		t.Fatal("the hub runs by default")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket exists before start: %v", err)
	}
	if cmd := m.startWebHub(); cmd == nil || m.web == nil {
		t.Fatalf("not started: %s", aiToastText(&m))
	}
	if again := m.startWebHub(); again != nil {
		t.Fatal("a second start did something")
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket %v %v", fi, err)
	}
	r := &webRig{t: t, m: &m, hub: m.web, path: path}
	c, _ := r.webHello(webTestOpen)

	other := aiTestModel(t)
	other.startWebHub()
	if other.web != nil {
		t.Fatal("a second UI for the folder serves the web hub too")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("the busy socket was removed:", err)
	}

	m.quitNow()
	if m.web != nil {
		t.Fatal("still serving after quit")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left after quit: %v", err)
	}
	if _, ok := <-c.lines; ok {
		for range c.lines {
		}
	}
	if _, err := net.Dial("unix", path); err == nil {
		t.Fatal("the hub still answers")
	}
	StopWebHub() // nothing left: a no-op
}

// Spec (WEB-6): the real Update routes hub messages (and re-arms the wait).
func TestWebMsgRoutedByUpdate(t *testing.T) {
	r := newWebRig(t, nil)
	c := r.dial(webTestOpen)
	c.send(`{"t":"hello","v":2,"token":"x"}`)
	msg := <-r.hub.ch
	next, cmd := r.m.Update(msg)
	if cmd == nil {
		t.Fatal("no wait re-armed")
	}
	*r.m = next.(Model)
	select {
	case line, ok := <-c.lines:
		if !ok || !strings.Contains(line, `"t":"meta"`) {
			t.Fatalf("got %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Update did not answer the hello")
	}
}
