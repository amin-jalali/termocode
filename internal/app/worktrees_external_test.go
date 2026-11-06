package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/worktrees"
)

func wtCardText(m *Model, path string, w int) string {
	items, _ := m.wtList()
	for _, it := range items {
		if it.e.Path == path {
			return ansi.Strip(strings.Join(wtCardLines(it, wtStatus{known: true}, false, false, w, wtTestNow), "\n"))
		}
	}
	return ""
}

// WTX-10: a worktree made outside termocode says "no meta" on its card
// until A adopts it; A gives it metadata (it stays in EXTERNAL) and
// refuses on one termocode made.
func TestWorktreeExternalAdopt(t *testing.T) {
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, h: 42}
	const ext = "/elsewhere/api-v2"
	if c := wtCardText(m, ext, 40); !strings.Contains(c, "no meta") {
		t.Fatalf("external card:\n%s", c)
	}
	if c := wtCardText(m, "/src/app.worktrees/toast", 40); strings.Contains(c, "no meta") {
		t.Errorf("a user worktree says no meta:\n%s", c)
	}
	st.cursorPath = ext
	wtKey(t, m, "A")
	if md, _ := worktrees.Load(st.common); !md[ext].Adopted || md[ext].LastSeen.IsZero() {
		t.Fatalf("not adopted: %+v", md[ext])
	}
	if c := wtCardText(m, ext, 40); strings.Contains(c, "no meta") {
		t.Errorf("adopted card still says no meta:\n%s", c)
	}
	items, rows := m.wtList()
	if it := items[rows[st.cursorIndex(items, rows)].item]; it.group != "EXTERNAL" {
		t.Errorf("adopted one moved to %s", it.group)
	}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "A")
	if !strings.Contains(wtLastToast(m), "made outside termocode") {
		t.Errorf("toast %q", wtLastToast(m))
	}
	if _, ok := m.dispatchWorktreePalette(wtAdoptPaletteID); !ok {
		t.Error("adopt not in the palette")
	}
	if p, ok := aiActionPolicyFor(wtAdoptPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("adopt policy %+v", p)
	}
}

// W2 leftover: F forgets a gone worktree (its metadata and git's
// record); it refuses one whose folder is there.
func TestWorktreeForgetGone(t *testing.T) {
	var forgot []string
	old := wtForgetRun
	wtForgetRun = func(root, path string) error { forgot = append(forgot, path); return nil }
	defer func() { wtForgetRun = old }()
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	_ = worktrees.Update(st.common, func(mm map[string]worktrees.Meta) { mm["/tmp/deleted"] = worktrees.Meta{Name: "old", Pinned: true} })
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, h: 42}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "F")
	if len(forgot) != 0 || !strings.Contains(wtLastToast(m), "folder is still there") {
		t.Fatalf("forgot a live one: %v %q", forgot, wtLastToast(m))
	}
	st.cursorPath = "/tmp/deleted"
	wtDrive(t, m, wtKey(t, m, "F"))
	if len(forgot) != 1 || forgot[0] != "/tmp/deleted" {
		t.Fatalf("forgot %v", forgot)
	}
	if md, _ := worktrees.Load(st.common); len(md["/tmp/deleted"].Name) != 0 {
		t.Errorf("metadata kept: %+v", md["/tmp/deleted"])
	}
	for _, e := range st.entries {
		if e.Path == "/tmp/deleted" {
			t.Error("still in the list")
		}
	}
	if p, ok := aiActionPolicyFor(wtForgetPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("forget policy %+v", p)
	}
}
