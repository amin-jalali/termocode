package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/update"
)

// updInstalled puts a fake installed termocode at dir/bin/termocode and
// makes the update code see it as the running binary; owner says which
// package owns it ("" = none).
func updInstalled(t *testing.T, owner string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	_ = os.MkdirAll(dir, 0o755)
	exe := filepath.Join(dir, "termocode")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'termocode v0.1.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := updateDepsFor
	updateDepsFor = func() updateDeps {
		d := old()
		d.exe = func() (string, error) { return exe, nil }
		d.owner = func(p string) (string, bool) { return owner, owner != "" && p == exe }
		return d
	}
	t.Cleanup(func() { updateDepsFor = old })
	return exe
}

// updModel is a model that already saw release v0.2.0 (running v0.1.0).
func updModel(t *testing.T, owner string) (*Model, string, string) {
	t.Helper()
	f := newUpdFake(t, "v0.2.0")
	stateDir := useUpdFake(t, f, "v0.1.0", updNow)
	exe := updInstalled(t, owner)
	m := &Model{w: 160, h: 40}
	updRun(t, m, m.updateInitCmd())
	return m, exe, filepath.Join(stateDir, "termocode", "update.json")
}

// A click on the chip opens the card: versions, where it is, and Later
// first and focused.
func TestUpdateNoticeCardSelf(t *testing.T) {
	m, exe, _ := updModel(t, "")
	m.openUpdateCard()
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateNotice {
		t.Fatal("card not open")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"termocode v0.2.0 is out", "v0.1.0 → v0.2.0", filepath.Base(exe)} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	if ids := buttonIDs(m.confirm); ids != "cancel,notes,skip,update" {
		t.Errorf("buttons %v", ids)
	}
}

// A package manager's binary: the card shows its command and Copy, no
// in-app Update.
func TestUpdateNoticeCardPackage(t *testing.T) {
	m, _, _ := updModel(t, "termocode-bin")
	m.openUpdateCard()
	box := ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, "yay -S termocode-bin") {
		t.Errorf("card misses the command:\n%s", box)
	}
	if ids := buttonIDs(m.confirm); ids != "cancel,notes,skip,copy" {
		t.Errorf("buttons %v", ids)
	}
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	m.handleConfirmSelect("copy")
	if len(copied) != 1 || copied[0] != "yay -S termocode-bin" {
		t.Errorf("copied %v", copied)
	}
}

// Skip this version hides the chip and is saved; Release notes opens the
// release page.
func TestUpdateNoticeSkipAndNotes(t *testing.T) {
	m, _, statePath := updModel(t, "")
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	m.openUpdateCard()
	m.handleConfirmSelect("notes")
	if len(opened) != 1 || !strings.HasSuffix(opened[0], "/notes") {
		t.Errorf("opened %v", opened)
	}
	m.openUpdateCard()
	m.handleConfirmSelect("skip")
	if m.statusState().Update != "" {
		t.Error("chip still there after Skip")
	}
	if st := update.LoadState(statePath); st.Skipped != "v0.2.0" {
		t.Errorf("state %+v", st)
	}
}
