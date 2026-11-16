//go:build unix

package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Spec (Now, TUI): "Now: What Needs You" opens the full Now page with the
// same items as the web's (whose turn, why, rule); e marks the selected
// item done in the per-user store (it leaves the list), u takes it back;
// esc closes. Nothing acts on the item's source.
func TestNowTUIPage(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	m.w, m.h = 120, 30
	m.ws = workspace.Workspace{Root: t.TempDir()}
	m.nowS = now.Open(filepath.Join(t.TempDir(), "now.json"))
	webTestTaskRun(&m, "test", time.Now().Add(-2*time.Minute), false, 1)
	m.ci = &ciWatch{branch: "main", loaded: true, snap: ghci.Snapshot{Branch: "main", HeadSHA: "abc1234def",
		Runs: []ghci.Run{{Workflow: "e2e-linux", Status: "completed", Conclusion: "failure"}}}}

	m.dispatchPaletteAction(nowPaletteID)
	if !m.nowPageOpen() {
		t.Fatal("the Now page did not open")
	}
	out := ansi.Strip(m.nowLayer(""))
	for _, w := range []string{"◉ Now", "2 need you", "YOUR TURN", "CI · main", "[Red CI]", "Your branch is red"} {
		if !strings.Contains(out, w) {
			t.Errorf("page lacks %q:\n%s", w, out)
		}
	}
	nm, _ := m.routeToNowPage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = nm
	if !m.nowS.Hidden(now.Item{ID: "ci:main", Head: "abc1234def"}, time.Now()) {
		t.Fatal("e did not mark the CI item done")
	}
	if out := ansi.Strip(m.nowLayer("")); !strings.Contains(out, "YOUR TURN · 1") || !strings.Contains(out, "✓ done: CI · main   u undo") {
		t.Errorf("after done:\n%s", out)
	}
	m, _ = m.routeToNowPage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if m.nowS.Hidden(now.Item{ID: "ci:main", Head: "abc1234def"}, time.Now()) {
		t.Fatal("u did not bring the CI item back")
	}
	if m.ci.snap.State() != ghci.StateFail {
		t.Fatal("the page changed the CI source")
	}
	m, _ = m.routeToNowPage(tea.KeyMsg{Type: tea.KeyEsc})
	if m.nowPageOpen() {
		t.Fatal("esc did not close the page")
	}
}
