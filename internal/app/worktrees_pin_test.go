package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/worktrees"
)

// p pins a worktree into PINNED and unpins it again; the pin is stored
// in worktrees.json (it survives a restart) and the cursor stays on it.
func TestWorktreePinToggles(t *testing.T) {
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42}
	st.cursorPath = "/elsewhere/api-v2"
	wtKey(t, m, "p")
	items, rows := m.wtList()
	cur := st.cursorIndex(items, rows)
	if it := items[rows[cur].item]; it.e.Path != "/elsewhere/api-v2" || it.group != "PINNED" {
		t.Fatalf("after p: %s in %s", it.e.Path, it.group)
	}
	if metas, _ := worktrees.Load(st.common); !metas["/elsewhere/api-v2"].Pinned {
		t.Errorf("pin not stored: %+v", metas)
	}
	wtKey(t, m, "p")
	if metas, _ := worktrees.Load(st.common); metas["/elsewhere/api-v2"].Pinned {
		t.Error("unpin not stored")
	}
	// A broken worktrees.json is never overwritten: the pin says why.
	p := worktrees.MetaPath(st.common)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtKey(t, m, "p")
	if b, _ := os.ReadFile(p); string(b) != "{broken" {
		t.Errorf("broken file overwritten: %q", b)
	}
	if !strings.Contains(wtLastToast(m), "could not be saved") {
		t.Errorf("toast %q", wtLastToast(m))
	}
	if _, ok := m.dispatchWorktreePalette("worktree-pin"); !ok {
		t.Error("worktree-pin not handled")
	}
	if p, ok := aiActionPolicyFor("worktree-pin"); !ok || p.Gate != aiGateAuto {
		t.Errorf("worktree-pin policy %+v", p)
	}
}
