package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/traybar"
)

// Spec (CMP-50): `termocode bar` prints the session summary for a menu
// bar or status bar in the asked form; a bad flag is usage (exit 2); a
// failed look-up is exit 1 with the reason.
func TestRunBar(t *testing.T) {
	old := barGather
	t.Cleanup(func() { barGather = old })
	barGather = func() ([]traybar.Session, error) {
		return []traybar.Session{{Workspace: "/w/api", Attached: true,
			Tabs: []agentctl.Tab{{ID: "t1", Agent: "claude", State: "waiting"}}}}, nil
	}
	run := func(args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runBarIn(args, &out, &errw)
		return code, out.String(), errw.String()
	}
	if code, out, _ := run(); code != 0 || !strings.HasPrefix(out, "tc 1 waiting") {
		t.Errorf("default text %d %q", code, out)
	}
	if code, out, _ := run("--format", "json"); code != 0 || !strings.Contains(out, `"class":"waiting"`) {
		t.Errorf("json %d %q", code, out)
	}
	if code, out, _ := run("--format=menu"); code != 0 || !strings.Contains(out, "param1=attach") {
		t.Errorf("menu %d %q", code, out)
	}
	if code, _, errs := run("--format", "yaml"); code != 2 || errs == "" {
		t.Errorf("bad format %d %q", code, errs)
	}
	if code, _, _ := run("--nope"); code != 2 {
		t.Errorf("bad flag %d", code)
	}
	if code, out, _ := run("--help"); code != 0 || !strings.Contains(out, "termocode bar") {
		t.Errorf("help %d %q", code, out)
	}
	barGather = func() ([]traybar.Session, error) { return nil, errors.New("no session folder") }
	if code, _, errs := run(); code != 1 || !strings.Contains(errs, "no session folder") {
		t.Errorf("gather error %d %q", code, errs)
	}
}

// Spec (CMP-50): bar is a subcommand, so it never holds the GC for a
// first UI frame.
func TestBarIsNoTUICommand(t *testing.T) {
	if tuiCommand([]string{"bar"}) {
		t.Error("bar must not start the UI")
	}
}
