package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/update"
)

// The update card names Homebrew and go install, and a binary in a
// folder the user cannot write offers the command to copy.
func TestCovCoreUpdateCardMethods(t *testing.T) {
	cases := []struct {
		in   update.Install
		want string
	}{
		{update.Install{Method: update.MethodBrew}, "Homebrew"},
		{update.Install{Method: update.MethodGo}, "go install"},
		{update.Install{Method: update.MethodPackage, Package: "termocode-bin"}, "the package termocode-bin"},
		{update.Install{Method: update.MethodSelf}, ""},
	}
	for _, c := range cases {
		if got := updateMethodName(c.in); got != c.want {
			t.Errorf("updateMethodName(%v) = %q, want %q", c.in.Method, got, c.want)
		}
	}

	m, _, _ := updModel(t, "")
	old := updateDepsFor
	updateDepsFor = func() updateDeps {
		d := old()
		d.exe = func() (string, error) { return "", errors.New("no /proc") }
		return d
	}
	t.Cleanup(func() { updateDepsFor = old })
	_ = m.openUpdateCard()
	v := ansiPlain(m.confirm.View())
	if !strings.Contains(v, "a folder you cannot write to") || !strings.Contains(v, "Copy command") {
		t.Errorf("read-only card:\n%s", v)
	}
	if got := updateInstall(); got.Method != update.MethodReadOnly || got.Command == "" {
		t.Errorf("install without an exe: %+v", got)
	}
	// Copy command copies it.
	var copied []string
	restore := SetClipboardSink(func(s string) { copied = append(copied, s) })
	defer restore()
	m.handleConfirmSelect("copy")
	if len(copied) != 1 || copied[0] != update.InstallScript {
		t.Errorf("copied %q", copied)
	}
}

// Up to date: Check for Updates says so; release notes over SSH or
// without a browser copy the link; a busy download is not started twice.
func TestCovCoreUpdateNotesAndBusy(t *testing.T) {
	m, _, _ := updModel(t, "")
	var copied []string
	restore := SetClipboardSink(func(s string) { copied = append(copied, s) })
	defer restore()

	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	m.updW().state.NotesURL = ""
	_ = m.updateOpenNotes()
	if len(copied) != 1 || !strings.Contains(copied[0], "/releases/latest") || !strings.Contains(toastText(*m), "Copied the link") {
		t.Errorf("notes over ssh: %q\n%s", copied, toastText(*m))
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	old := osOpen
	osOpen = func(string) error { return errors.New("no browser") }
	defer func() { osOpen = old }()
	_ = m.updateOpenNotes()
	if !strings.Contains(toastText(*m), "No browser: copied the link") {
		t.Errorf("no browser:\n%s", toastText(*m))
	}

	m.updW().busy = true
	_ = m.updateStart()
	if !strings.Contains(toastText(*m), "An update is already downloading") {
		t.Errorf("busy:\n%s", toastText(*m))
	}
	m.updW().busy = false
	// Not our own binary: Update shows the card instead.
	m.updW().install = update.Install{Method: update.MethodBrew}
	_ = m.updateStart()
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateNotice {
		t.Error("update of a brew binary did not show the card")
	}

	// The restart card: Later does nothing.
	m.confirmKind = confirmKindUpdateRestart
	if cmd, ok := m.handleUpdateConfirm("cancel"); !ok || cmd != nil {
		t.Error("restart Later")
	}

	// Nothing newer: the check says termocode is up to date.
	m.updW().state.Latest = ""
	_ = m.openUpdateCard()
	if !strings.Contains(toastText(*m), "is up to date") {
		t.Errorf("up to date:\n%s", toastText(*m))
	}
}
