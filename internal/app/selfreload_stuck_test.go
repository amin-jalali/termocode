package app

import (
	"strings"
	"testing"
	"time"
)

// reloadClock sets the reload's clock to a fake one the test moves.
func reloadClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Unix(50_000, 0)
	old, oldRecap := selfReloadNow, recapNow
	selfReloadNow = func() time.Time { return now }
	recapNow = func() time.Time { return now }
	t.Cleanup(func() { selfReloadNow, recapNow = old, oldRecap })
	return &now
}

func reloadLogged() string {
	errorRingMu.Lock()
	defer errorRingMu.Unlock()
	var out []string
	for _, l := range errorRing {
		if strings.Contains(l, "[reload]") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func clearErrorRing(t *testing.T) {
	t.Helper()
	errorRingMu.Lock()
	errorRing = nil
	errorRingMu.Unlock()
}

// Spec: a dialog or a page card that nobody answers (a serve session
// with no terminal attached, a card left open on the laptop) does not
// hold the new build back for ever: after selfReloadUnattended with no
// key or click, termocode reloads anyway; the log says why.
func TestReloadOnNewBuildNotHeldForeverByAnUnansweredDialog(t *testing.T) {
	now := reloadClock(t)
	clearErrorRing(t)
	r := newReloadRig(t)
	r.m.confirmOpen = true
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll()
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded at once under an open dialog")
	}
	*now = now.Add(selfReloadUnattended - time.Minute)
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded before the dialog was left alone long enough")
	}
	*now = now.Add(2 * time.Minute)
	r.poll()
	if len(r.execs) != 1 {
		t.Fatalf("execs %v: a dialog nobody answers held the new build back", r.execs)
	}
	if got := reloadLogged(); !strings.Contains(got, "a dialog is open") || !strings.Contains(got, "nobody") {
		t.Errorf("log %q: want why it reloaded under the dialog", got)
	}
}

// Spec: while the user works (a key or a click lately), an open dialog
// keeps holding the reload.
func TestReloadOnNewBuildDialogHoldsWhileTheUserIsThere(t *testing.T) {
	now := reloadClock(t)
	r := newReloadRig(t)
	r.m.confirmOpen = true
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll()
	r.poll()
	*now = now.Add(selfReloadUnattended + time.Hour)
	r.m.recapS().lastInput = now.Add(-time.Minute) // typed a minute ago
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded under a dialog the user just used")
	}
}

// Spec: real work is never cut: a command in a terminal tab (or an AI
// turn, an agent) holds the reload however long it runs.
func TestReloadOnNewBuildWorkIsNeverCut(t *testing.T) {
	now := reloadClock(t)
	r := newReloadRig(t)
	r.m.terminalTabs = []terminalTab{{Busy: true}}
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll()
	r.poll()
	*now = now.Add(selfReloadUnattended * 10)
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded while a command ran in a terminal tab")
	}
}

// Spec: the wait is not silent: errors.log names the new build and what
// it waits for, once per build and reason (a serve session has no
// screen for the toast and the chip).
func TestReloadOnNewBuildLogsWhyItWaits(t *testing.T) {
	reloadClock(t)
	clearErrorRing(t)
	r := newReloadRig(t)
	r.m.terminalTabs = []terminalTab{{Busy: true}}
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	for range 5 {
		r.poll()
	}
	got := reloadLogged()
	if !strings.Contains(got, "v0.0.0-next+dirty") || !strings.Contains(got, "a command runs in a terminal tab") {
		t.Fatalf("log %q: want the build and the reason", got)
	}
	if n := strings.Count(got, "\n") + 1; n != 1 {
		t.Errorf("%d log lines for one wait, want 1:\n%s", n, got)
	}
}
