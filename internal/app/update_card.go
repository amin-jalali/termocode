package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/update"
)

// G-UPD cards: the notice (chip, toast or Check for Updates).

const (
	confirmKindUpdateNotice confirmKindEnum = 3200 + iota
	confirmKindUpdateInstall
	confirmKindUpdateRestart
	confirmKindUpdateRollback
)

// updateInstall finds out how the running binary was installed.
func updateInstall() update.Install {
	d := updateDepsFor()
	exe, err := d.exe()
	if err != nil {
		return update.Install{Method: update.MethodReadOnly, Command: update.InstallScript}
	}
	return update.Detect(exe, update.Env{Getenv: d.getenv, Owner: d.owner})
}

// updateMethodName says who installed termocode, for the card.
func updateMethodName(in update.Install) string {
	switch in.Method {
	case update.MethodBrew:
		return "Homebrew"
	case update.MethodPackage:
		return "the package " + in.Package
	case update.MethodGo:
		return "go install"
	}
	return ""
}

// openUpdateCard shows the newer release: what changes, where termocode
// is, and what Update does (or the command to run for a package
// manager's binary).
func (m *Model) openUpdateCard() tea.Cmd {
	w := m.updW()
	tag := w.state.Available(termocodeVersion)
	if tag == "" {
		return m.pushToastDetail(toast.Info, "termocode "+termocodeVersion+" is up to date", "")
	}
	in := updateInstall()
	w.install = in
	var body strings.Builder
	fmt.Fprintf(&body, "%s → %s\n\n", termocodeVersion, tag)
	buttons := []confirm.Button{
		{ID: "cancel", Title: "Later"},
		{ID: "notes", Title: "Release notes"},
		{ID: "skip", Title: "Skip this version"},
	}
	switch in.Method {
	case update.MethodSelf:
		fmt.Fprintf(&body, "Installed at %s.\n\nUpdate downloads it from GitHub and checks it with checksums.txt; you confirm before it replaces anything.", in.Path)
		buttons = append(buttons, confirm.Button{ID: "update", Title: "Update", Style: confirm.StylePrimary})
	case update.MethodReadOnly:
		fmt.Fprintf(&body, "Installed at %s, a folder you cannot write to. Run this in a shell (with the rights that folder needs):", in.Path)
		buttons = append(buttons, confirm.Button{ID: "copy", Title: "Copy command", Style: confirm.StylePrimary})
	default:
		fmt.Fprintf(&body, "termocode was installed with %s (%s). Update it there:", updateMethodName(in), in.Path)
		buttons = append(buttons, confirm.Button{ID: "copy", Title: "Copy command", Style: confirm.StylePrimary})
	}
	m.confirm = confirm.New("⬆ termocode "+tag+" is out", body.String(), buttons).WithWideText()
	if in.Command != "" && in.Method != update.MethodSelf {
		m.confirm = m.confirm.WithCode(in.Command, "")
	}
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindUpdateNotice
	return nil
}

// handleUpdateConfirm answers the update cards. ok=false: not ours.
func (m *Model) handleUpdateConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindUpdateInstall:
		return m.answerUpdateInstall(id), true
	case confirmKindUpdateRollback:
		return m.answerUpdateRollback(id), true
	case confirmKindUpdateRestart:
		if id == "restart" {
			return updateRestart(m), true
		}
		return nil, true
	case confirmKindUpdateNotice:
	default:
		return nil, false
	}
	w := m.updW()
	switch id {
	case "update":
		return m.updateStart(), true
	case "notes":
		return m.updateOpenNotes(), true
	case "skip":
		tag := w.state.Available(termocodeVersion)
		w.state.Skipped = tag
		_ = update.SaveState(update.StatePath(updateDepsFor().getenv), w.state)
		return m.pushToastDetail(toast.Info, "Skipped termocode "+tag, "termocode: Check for Updates shows it again"), true
	case "copy":
		return m.copyWithToast(w.install.Command, "Copied the update command"), true
	}
	return nil, true
}

// updateOpenNotes opens the release page (over SSH: copies the link).
func (m *Model) updateOpenNotes() tea.Cmd {
	url := m.updW().state.NotesURL
	if url == "" {
		url = "https://github.com/" + update.Repo + "/releases/latest"
	}
	if mcpOverSSH() {
		m.copyToClipboard(url)
		return m.pushToastDetail(toast.Info, "Copied the link", url)
	}
	if err := osOpen(url); err != nil {
		m.copyToClipboard(url)
		return m.pushToastDetail(toast.Info, "No browser: copied the link", url)
	}
	return nil
}
