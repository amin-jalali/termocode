package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/update"
)

// updFake is a GitHub stand-in (https, random port) with one release.
type updFake struct {
	srv  *httptest.Server
	tag  string
	fail atomic.Bool
	hits atomic.Int32
}

func newUpdFake(t *testing.T, tag string) *updFake {
	t.Helper()
	f := &updFake{tag: tag}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if f.fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "html_url": f.srv.URL + "/notes"})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// useUpdFake points the update code at f, a temp state folder and the
// running version cur, with a frozen clock at now.
func useUpdFake(t *testing.T, f *updFake, cur string, now time.Time) (stateDir string) {
	t.Helper()
	stateDir = t.TempDir()
	env := map[string]string{"XDG_STATE_HOME": stateDir, "HOME": t.TempDir()}
	old := updateDepsFor
	updateDepsFor = func() updateDeps {
		d := old()
		d.client = update.Client{HTTP: f.srv.Client(), LatestURL: f.srv.URL + "/latest", Version: cur}
		d.getenv = func(k string) string { return env[k] }
		d.headlessOK = true
		return d
	}
	oldV := termocodeVersion
	termocodeVersion = cur
	t.Cleanup(func() { updateDepsFor, termocodeVersion = old, oldV })
	t.Cleanup(clock.Set(func() time.Time { return now }))
	return stateDir
}

// updRun runs an update command and feeds its message back.
func updRun(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	next, ok := m.handleUpdateMsg(cmd())
	if !ok {
		t.Fatal("message not handled")
	}
	return next
}

var updNow = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

// A newer release: the chip shows it and the check time is saved.
func TestUpdateBackgroundCheckShowsChip(t *testing.T) {
	f := newUpdFake(t, "v0.2.0")
	dir := useUpdFake(t, f, "v0.1.0", updNow)
	m := &Model{w: 160, h: 40}
	updRun(t, m, m.updateInitCmd())
	if got := m.statusState().Update; got != "v0.2.0" {
		t.Fatalf("chip %q", got)
	}
	st := update.LoadState(filepath.Join(dir, "termocode", "update.json"))
	if !st.LastCheck.Equal(updNow) || st.Latest != "v0.2.0" {
		t.Fatalf("state %+v", st)
	}
	// Within 24 h the next start does not ask GitHub; the chip comes from
	// the saved state.
	clock.Set(func() time.Time { return updNow.Add(23 * time.Hour) })
	m2 := &Model{w: 160, h: 40}
	updRun(t, m2, m2.updateInitCmd())
	if f.hits.Load() != 1 {
		t.Fatalf("GitHub asked %d times, want 1", f.hits.Load())
	}
	if m2.statusState().Update != "v0.2.0" {
		t.Fatal("no chip from the saved state")
	}
	// After 24 h it asks again.
	clock.Set(func() time.Time { return updNow.Add(25 * time.Hour) })
	m3 := &Model{w: 160, h: 40}
	updRun(t, m3, m3.updateInitCmd())
	if f.hits.Load() != 2 {
		t.Fatalf("GitHub asked %d times, want 2", f.hits.Load())
	}
}

// The same version, an offline check and a GitHub error show nothing.
func TestUpdateBackgroundCheckQuiet(t *testing.T) {
	f := newUpdFake(t, "v0.1.0")
	useUpdFake(t, f, "v0.1.0", updNow)
	m := &Model{w: 160, h: 40}
	updRun(t, m, m.updateInitCmd())
	if m.statusState().Update != "" || !m.toast.Empty() {
		t.Fatal("same version: want nothing")
	}
	f2 := newUpdFake(t, "v0.2.0")
	f2.fail.Store(true)
	useUpdFake(t, f2, "v0.1.0", updNow)
	m2 := &Model{w: 160, h: 40}
	updRun(t, m2, m2.updateInitCmd())
	if m2.statusState().Update != "" || !m2.toast.Empty() {
		t.Fatal("GitHub error: want nothing")
	}
}

// Dev builds and update_check = false never look.
func TestUpdateBackgroundCheckOff(t *testing.T) {
	f := newUpdFake(t, "v0.2.0")
	useUpdFake(t, f, "dev", updNow)
	if (&Model{}).updateInitCmd() != nil {
		t.Fatal("dev build checks")
	}
	useUpdFake(t, f, "v0.1.0", updNow)
	path, _ := settingsConfigPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(`{"update_check": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if (&Model{}).updateInitCmd() != nil {
		t.Fatal("update_check off still checks")
	}
	if f.hits.Load() != 0 {
		t.Fatal("GitHub was asked")
	}
}
