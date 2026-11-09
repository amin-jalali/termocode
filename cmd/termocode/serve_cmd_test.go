//go:build unix

package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeServe records what `termocode serve` asked for.
type fakeServe struct {
	started  []string
	startErr error
	already  bool
	hubWaits int
	hubErr   error
	web      [][]string
}

func (f *fakeServe) deps() serveDeps {
	return serveDeps{
		start: func(dir string) (bool, error) {
			f.started = append(f.started, dir)
			return !f.already, f.startErr
		},
		waitHub: func(dir string, since time.Time) error {
			f.hubWaits++
			return f.hubErr
		},
		web: func(args []string) int {
			f.web = append(f.web, args)
			return 0
		},
	}
}

// Spec: `termocode serve <dir>` starts the folder's session with its UI
// and no terminal (its own pty, as `termocode service` does) and comes
// back at once: for a script. It says how to see and stop it.
func TestServeStartsTheSessionWithoutATerminal(t *testing.T) {
	ws := t.TempDir()
	f := &fakeServe{}
	var out, errw bytes.Buffer
	if code := runServeIn([]string{ws}, &out, &errw, "/", f.deps()); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
	abs, _ := filepath.Abs(ws)
	if !slices.Equal(f.started, []string{abs}) {
		t.Fatalf("started %v, want %q", f.started, abs)
	}
	for _, want := range []string{"termocode attach " + abs, "termocode kill " + abs} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
	if f.hubWaits != 0 || len(f.web) != 0 {
		t.Errorf("without --web: %d hub waits, web %v", f.hubWaits, f.web)
	}
}

// Spec: the folder defaults to the current one; --web also opens the web
// panel once the UI's hub is up (`termocode web start --yes`, which needs
// "web_panel": "on").
func TestServeWeb(t *testing.T) {
	cwd := t.TempDir()
	f := &fakeServe{}
	var out, errw bytes.Buffer
	if code := runServeIn([]string{"--web"}, &out, &errw, cwd, f.deps()); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
	if len(f.started) != 1 || f.started[0] != cwd || f.hubWaits != 1 {
		t.Fatalf("started %v, hub waits %d", f.started, f.hubWaits)
	}
	if len(f.web) != 1 || !slices.Equal(f.web[0], []string{"start", "--yes", "--dir", cwd}) {
		t.Fatalf("web %v", f.web)
	}
}

// Spec: a session that runs already is left as it is (exit 0, it says
// so); a missing folder, an unknown flag or a failed start is an error.
func TestServeErrors(t *testing.T) {
	ws := t.TempDir()
	f := &fakeServe{already: true}
	var out, errw bytes.Buffer
	if code := runServeIn([]string{ws}, &out, &errw, "/", f.deps()); code != 0 || !strings.Contains(out.String(), "already") {
		t.Errorf("running session: exit %d, out %q", code, out.String())
	}
	out.Reset()
	if code := runServeIn([]string{filepath.Join(ws, "gone")}, &out, &errw, "/", f.deps()); code == 0 {
		t.Error("a missing folder exited 0")
	}
	if code := runServeIn([]string{"--nope", ws}, &out, &errw, "/", f.deps()); code != 2 {
		t.Errorf("an unknown flag exited %d, want 2", code)
	}
	f = &fakeServe{startErr: errors.New("no pty")}
	errw.Reset()
	if code := runServeIn([]string{ws}, &out, &errw, "/", f.deps()); code != 1 || !strings.Contains(errw.String(), "no pty") {
		t.Errorf("failed start: exit %d, err %q", code, errw.String())
	}
	f = &fakeServe{hubErr: errors.New("web panel off")}
	errw.Reset()
	if code := runServeIn([]string{"--web", ws}, &out, &errw, "/", f.deps()); code != 1 || len(f.web) != 0 {
		t.Errorf("--web without a hub: exit %d, web %v", code, f.web)
	}
	if code := runServeIn([]string{"-h"}, &out, &errw, "/", f.deps()); code != 0 {
		t.Errorf("-h exited %d", code)
	}
}

// Spec: serve is a subcommand (no UI starts here: the GC is released at
// once) and is in the usage.
func TestServeIsASubcommand(t *testing.T) {
	if tuiCommand([]string{"serve", "."}) {
		t.Error("serve counts as a UI start")
	}
}
