package app

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/search"
)

func TestSearchMaxResultsSetting(t *testing.T) {
	if got := searchMaxResults(settingsConfig{}); got != search.DefaultMaxResults {
		t.Errorf("default = %d", got)
	}
	zero, neg, n := 0, -5, 50
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &zero}); got != 0 {
		t.Errorf("0 (unlimited) = %d", got)
	}
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &neg}); got != search.DefaultMaxResults {
		t.Errorf("negative = %d", got)
	}
	if got := searchMaxResults(settingsConfig{SearchMaxResults: &n}); got != 50 {
		t.Errorf("50 = %d", got)
	}

	// Round-trips through config.json next to the other settings.
	withConfigDir(t)
	if err := saveSettings(settingsConfig{SearchMaxResults: &n}); err != nil {
		t.Fatal(err)
	}
	if got := searchMaxResults(loadSettings()); got != 50 {
		t.Errorf("persisted = %d", got)
	}
}

// TestHandleFindBatch_StaleAndEmpty checks that batches from a superseded
// run are dropped and an empty finished run toasts instead of opening a
// buffer.
func TestHandleFindBatch_StaleAndEmpty(t *testing.T) {
	m := Model{w: 120, h: 40}
	m.findRun = findResultsRun{gen: 2, query: "zzz"}

	if cmd := m.handleFindBatch(findBatchMsg{gen: 1, batch: search.Batch{Done: true}}); cmd != nil {
		t.Error("stale batch should be ignored")
	}
	if cmd := m.handleFindBatch(findBatchMsg{gen: 2, batch: search.Batch{Done: true}}); cmd == nil {
		t.Error("empty finished run should push a toast")
	}
	if m.findRun.opened {
		t.Error("no buffer should open without results")
	}

	m.findRun = findResultsRun{gen: 3, query: "x"}
	if cmd := m.handleFindBatch(findBatchMsg{gen: 3, batch: search.Batch{Done: true, Err: errors.New("boom")}}); cmd == nil {
		t.Error("error should push a toast")
	}
}

func TestReplaceConfirmMessage(t *testing.T) {
	counts := []search.FileCount{{Path: "/a", Count: 3}, {Path: "/b", Count: 1}}
	got := replaceConfirmMessage("foo", "bar", counts, 0)
	if got != `Replace 4 matches of "foo" with "bar" in 2 files?` {
		t.Errorf("message = %q", got)
	}
	if got := replaceConfirmMessage("foo", "bar", counts, 1); !strings.Contains(got, "1 of them has unsaved changes") {
		t.Errorf("dirty warning missing: %q", got)
	}
}

func TestFindNavToast(t *testing.T) {
	if got := findNavToast("ok", 1); got != "" {
		t.Errorf("ok: got %q, want no toast", got)
	}
	if got := findNavToast("none", 1); !strings.Contains(got, "Find in Files") {
		t.Errorf("none: got %q", got)
	}
	if got := findNavToast("end", -1); !strings.Contains(got, "previous") {
		t.Errorf("end/-1: got %q", got)
	}
	if got := findNavToast("end", 1); !strings.Contains(got, "more") {
		t.Errorf("end/1: got %q", got)
	}
}

// The app calls _G._termocode_find_nav for F4 / Shift+F4; the old nvim
// <F4> maps never fired because the app keymap caught F4 first.
func TestFindResultsLuaExportsNav(t *testing.T) {
	if !strings.Contains(findResultsLua, "_G._termocode_find_nav = navigate_results") {
		t.Error("findResultsLua must export _G._termocode_find_nav")
	}
	if strings.Contains(findResultsLua, "'<F4>'") {
		t.Error("findResultsLua should not map <F4> in nvim any more")
	}
}

func TestFindDisplayPath(t *testing.T) {
	cases := []struct {
		abs   string
		roots []string
		want  string
	}{
		{"/ws/pkg/a.go", []string{"/ws"}, "pkg/a.go"},
		{"/elsewhere/a.go", []string{"/ws"}, "/elsewhere/a.go"},
		{"/ws/a.go", []string{"/ws", "/other"}, "ws/a.go"},
		{"/other/lib/x.go", []string{"/ws", "/other"}, "other/lib/x.go"},
		{"/ws/..x/a.go", []string{"/ws"}, "..x/a.go"},
	}
	for _, c := range cases {
		if got := findDisplayPath(c.abs, c.roots); got != c.want {
			t.Errorf("findDisplayPath(%q, %q) = %q, want %q", c.abs, c.roots, got, c.want)
		}
	}
}

// The page takes every colour from the theme: each TermocodeFind* group
// it draws with is defined in themeLuaBody, and the Lua holds no hex
// colours or global highlight overrides (they leaked to other windows).
func TestFindResultsLuaUsesThemeGroups(t *testing.T) {
	re := regexp.MustCompile(`TermocodeFind[A-Za-z0-9]+`)
	for _, g := range re.FindAllString(findResultsLua+findResultsGoGroups(), -1) {
		if g == "TermocodeFindResults" { // the augroup
			continue
		}
		if !strings.Contains(themeLuaBody, "hi('"+g+"'") {
			t.Errorf("%s is used but not defined in themeLuaBody", g)
		}
	}
	if m := regexp.MustCompile(`#[0-9a-fA-F]{6}`).FindString(findResultsLua); m != "" {
		t.Errorf("hex colour %s in findResultsLua", m)
	}
	if strings.Contains(findResultsLua, "highlight default") || strings.Contains(findResultsLua, "set_hl(0, 'CursorLine'") {
		t.Error("findResultsLua changes global highlights")
	}
}

// findResultsGoGroups is the Go code's group names, joined.
func findResultsGoGroups() string {
	var b strings.Builder
	for _, st := range []findState{{searching: true}, {errText: "x"}, {sum: search.Summary{Shown: 2, Total: 9, Files: 1}}} {
		_, hl := findSummaryLine(st)
		b.WriteString(hl + " ")
	}
	for _, r := range findEmptyRows() {
		if h, ok := r["hl"].(string); ok {
			b.WriteString(h + " ")
		}
	}
	return b.String()
}
