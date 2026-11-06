package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// gitxSaved writes body to p and tells the app the user saved it.
func gitxSaved(t *testing.T, m *Model, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if cmd := m.dispatchNotify(nvim.NotifyMsg{Method: "termocode_local_history", Args: []any{p}}); cmd != nil {
		cmd()
	}
}

func gitxPickTitles(m *Model) string {
	var out []string
	for _, it := range m.gitx.items {
		out = append(out, it.Title+" | "+it.Hint)
	}
	return strings.Join(out, "\n")
}

// GITX-35 (R1–R4, E26): every save keeps a copy. File: Show Local
// History lists the active file's versions, newest first; a version
// compares with the file or is restored after a Cancel-first card (the
// file as it was goes into the history first). File: Put Label in Local
// History… names a version. File: Recover Deleted File… brings back a
// file of this workspace that is gone. local_history off keeps nothing.
func TestGitxLocalHistory(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	for _, id := range []string{"local-history", "local-history-label", "local-history-deleted"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("no AI policy for %s", id)
		}
	}
	if _, ok := aiSettingPolicies["local_history"]; !ok {
		t.Fatal("no AI policy for local_history")
	}
	if v, _ := gitxSettingValue("setting-local-history"); v != "true" {
		t.Fatalf("local_history default %q", v)
	}
	p := filepath.Join(dir, "notes", "a.md")
	gitxSaved(t, m, p, "first\n")
	gitxSaved(t, m, p, "second\n")
	gitxSaved(t, m, p, "second\n") // the same text: no new version
	gitxSaved(t, m, p, "third\n")

	m.editor.SetMeta(p, false, "")
	m.dispatchPaletteAction("local-history")
	if !m.pickerOpen || m.pickerKind != pickerKindGitx || len(m.gitx.items) != 3 {
		t.Fatalf("picker %v items:\n%s", m.pickerOpen, gitxPickTitles(m))
	}
	oldest := m.gitx.items[2].ID

	// Compare: old version against the file now.
	m.pickerOpen = false
	m.handleGitxPickerSelect(oldest)
	if !m.pickerOpen || gitxPickIDs(m) != "compare,restore" {
		t.Fatalf("action picker %q", gitxPickIDs(m))
	}
	m.pickerOpen = false
	cmd, _ := m.handleGitxPickerSelect("compare")
	msg := covMsg[compareReadyMsg](t, cmd)
	if msg.file.Old != "first\n" || msg.file.New != "third\n" {
		t.Fatalf("compare %q → %q", msg.file.Old, msg.file.New)
	}

	// Restore: Cancel keeps the file; Restore writes the old text.
	m.dispatchPaletteAction("local-history")
	m.pickerOpen = false
	m.handleGitxPickerSelect(m.gitx.items[2].ID)
	m.pickerOpen = false
	m.handleGitxPickerSelect("restore")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,restore" {
		t.Fatalf("restore card %v %q", m.confirmOpen, gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleGitxConfirm("cancel")
	if b, _ := os.ReadFile(p); string(b) != "third\n" {
		t.Fatalf("Cancel changed the file: %q", b)
	}
	m.dispatchPaletteAction("local-history")
	m.pickerOpen = false
	m.handleGitxPickerSelect(m.gitx.items[2].ID)
	m.pickerOpen = false
	m.handleGitxPickerSelect("restore")
	m.confirmOpen = false
	m.handleGitxConfirm("restore")
	if b, _ := os.ReadFile(p); string(b) != "first\n" {
		t.Fatalf("restored %q", b)
	}
	// The text before the restore is a version: the restore can be undone.
	m.dispatchPaletteAction("local-history")
	if len(m.gitx.items) != 4 {
		t.Fatalf("after restore:\n%s", gitxPickTitles(m))
	}
	m.pickerOpen = false

	// A label.
	m.dispatchPaletteAction("local-history-label")
	if !m.promptOpen || m.promptKind != promptKindGitx {
		t.Fatal("no label prompt")
	}
	m.promptOpen = false
	m.handleGitxPromptSubmit("before the big change")
	m.dispatchPaletteAction("local-history")
	if !strings.Contains(m.gitx.items[0].Title, "before the big change") {
		t.Fatalf("label not shown:\n%s", gitxPickTitles(m))
	}
	m.pickerOpen = false

	// Recover a deleted file.
	gone := filepath.Join(dir, "gone.txt")
	gitxSaved(t, m, gone, "keep me\n")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("local-history-deleted")
	if !m.pickerOpen || len(m.gitx.items) != 1 || !strings.Contains(m.gitx.items[0].Title, "gone.txt") {
		t.Fatalf("deleted picker:\n%s", gitxPickTitles(m))
	}
	m.pickerOpen = false
	m.handleGitxPickerSelect(m.gitx.items[0].ID)
	if b, err := os.ReadFile(gone); err != nil || string(b) != "keep me\n" {
		t.Fatalf("recovered %q %v", b, err)
	}
	m.dispatchPaletteAction("local-history-deleted")
	if m.pickerOpen {
		t.Fatal("a recovered file is still listed as deleted")
	}

	// Off: nothing is kept.
	m.applySetting("setting-local-history", "false")
	q := filepath.Join(dir, "off.txt")
	gitxSaved(t, m, q, "x\n")
	m.editor.SetMeta(q, false, "")
	m.dispatchPaletteAction("local-history")
	if m.pickerOpen {
		t.Fatal("a save was kept with local_history off")
	}
}

func gitxPickIDs(m *Model) string {
	var ids []string
	for _, it := range m.gitx.items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}
