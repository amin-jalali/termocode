package app

import (
	"os/exec"
	"strings"
	"testing"
)

// Glue D + F: the Run view hint installs the missing adapter.

func TestAdapterToolFor(t *testing.T) {
	cases := map[string]string{"go": "dlv", "python": "debugpy", "javascript": "js-debug", "typescript": "js-debug", "": "", "rust": ""}
	for lang, want := range cases {
		if got := adapterToolFor(lang); got != want {
			t.Errorf("adapterToolFor(%q) = %q, want %q", lang, got, want)
		}
	}
}

func TestDebugSetHint(t *testing.T) {
	d := newDebugState()
	d.setHint("DAP: dlv missing", "go")
	if d.hint == "" || d.hintTool != "dlv" {
		t.Fatalf("hint=%q tool=%q", d.hint, d.hintTool)
	}
	d.setHint("", "go")
	if d.hint != "" || d.hintTool != "" {
		t.Fatalf("clear: hint=%q tool=%q", d.hint, d.hintTool)
	}
}

// The hint row installs the named adapter in one step: Enter opens the
// install confirm for dlv, not the generic manager.
func TestRunHintRowOpensAdapterInstall(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := Model{w: 120, h: 40}
	d := m.ensureDebug()
	d.setHint("DAP: dlv missing — palette → DAP: Install Adapter...", "go")
	var hintRow runRow
	for _, r := range m.runRows() {
		if r.kind == runRowAction && r.action == "install" {
			hintRow = r
		}
	}
	if hintRow.action != "install" {
		t.Fatal("no install hint row")
	}
	m.runActivate(hintRow, false)
	if !m.confirmOpen || m.lspMgr == nil || m.lspMgr.pending != "dlv" {
		t.Fatalf("confirmOpen=%v pending=%v", m.confirmOpen, m.lspMgr)
	}
	if m.pickerOpen {
		t.Error("generic manager picker should not open")
	}
	if !strings.Contains(m.confirm.View(), "dlv") {
		t.Error("confirm does not name dlv")
	}
}

// Without a named adapter the hint falls back to the manager.
func TestRunHintRowFallsBackToManager(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := Model{w: 120, h: 40}
	m.openAdapterInstall("")
	if !m.pickerOpen || m.pickerKind != pickerKindToolManager {
		t.Fatalf("pickerOpen=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
}
