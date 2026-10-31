package app

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/update"
)

// Spec (SCN-13): offline, the daily check stays quiet (no chip, no
// toast) and still saves the check time, so it asks once a day, not on
// every start; Help: Check for Updates says it could not check, once.
func TestUpdateCheckOffline(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "https://" + l.Addr().String() + "/latest"
	_ = l.Close()
	f := newUpdFake(t, "v0.2.0")
	dir := useUpdFake(t, f, "v0.1.0", updNow)
	old := updateDepsFor
	updateDepsFor = func() updateDeps {
		d := old()
		d.client.LatestURL = refused
		return d
	}
	t.Cleanup(func() { updateDepsFor = old })

	m := &Model{w: 160, h: 40}
	start := time.Now()
	updRun(t, m, m.updateInitCmd())
	if time.Since(start) > 10*time.Second {
		t.Errorf("the offline check took %v", time.Since(start))
	}
	if m.statusState().Update != "" || !m.toast.Empty() {
		t.Fatalf("offline check not quiet: chip %q, toast %q", m.statusState().Update, toastText(*m))
	}
	if st := update.LoadState(filepath.Join(dir, "termocode", "update.json")); !st.LastCheck.Equal(updNow) {
		t.Errorf("check time not saved: %+v", st)
	}

	m2 := &Model{w: 160, h: 40}
	updRun(t, m2, m2.updateCheckNow())
	if got := toastText(*m2); strings.Count(got, "Could not check for updates") != 1 {
		t.Errorf("manual check offline: toast %q", got)
	}
}

func TestUpdateManualErrorUsesPlainCause(t *testing.T) {
	m := &Model{w: 160, h: 40}
	err := &url.Error{Op: "Get", URL: "https://example.test/x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	m.onUpdateManual(updateManualMsg{err: err})
	got := toastText(*m)
	if !strings.Contains(got, "refused the connection") || strings.Contains(got, "dial tcp") {
		t.Errorf("toast shows the raw error: %q", got)
	}
}
