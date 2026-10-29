package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// openFetchModel is a bare Model on a real nvim whose last state fetch
// ran just now, so a plain redraw would only schedule a trailing fetch.
func openFetchModel(t *testing.T) (Model, string) {
	t.Helper()
	c := newRestoreNvim(t)
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := Model{nvim: c}
	m.stateLastFetch = time.Now()
	return m, p
}

// redrawFetchesNow reports whether a redraw right now starts a state
// fetch at once (instead of a trailing one after the throttle).
func redrawFetchesNow(t *testing.T, m Model) bool {
	t.Helper()
	before := m.stateLastFetch
	m2, _, _ := m.handleNvimMsg(nvim.RedrawMsg{})
	return !m2.stateFetchPending && m2.stateLastFetch.After(before)
}

// Spec: Given a state fetch ran a moment ago,
// when a file is opened (open request, explorer, picker),
// then the next redraw fetches the state at once, so the tab bar and
// status bar show the new file without waiting for the throttle.
func TestOpenFileFetchesStateOnNextRedraw(t *testing.T) {
	t.Run("throttled without an open", func(t *testing.T) {
		m, _ := openFetchModel(t)
		if redrawFetchesNow(t, m) {
			t.Fatal("a redraw inside the throttle window fetched at once")
		}
	})
	t.Run("open at line", func(t *testing.T) {
		m, p := openFetchModel(t)
		m.openFileAtLine(p, 1)
		if !redrawFetchesNow(t, m) {
			t.Fatal("the redraw after an open waited for the throttle")
		}
	})
	t.Run("explorer", func(t *testing.T) {
		m, p := openFetchModel(t)
		m2, _ := m.updateInner(explorer.OpenFileMsg{Path: p})
		if !redrawFetchesNow(t, m2) {
			t.Fatal("the redraw after an explorer open waited for the throttle")
		}
	})
}
