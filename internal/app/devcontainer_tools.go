package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// devContainerClickMsg is what a click on the dev container toast sends.
// It only opens the installer list; nothing is installed until the user
// picks a row and confirms.
type devContainerClickMsg struct{ tools []string }

// maybeSuggestDevContainerTools shows one quiet toast the first time a
// workspace with a dev container file is open and some tools it implies
// are missing. It reads one small file; no network, no watcher. The
// workspace is remembered when the toast shows, so "Not now" (ignoring it)
// is final.
func (m *Model) maybeSuggestDevContainerTools(root string) tea.Cmd {
	m.ensureLSPMgr()
	if m.lspMgr.devChecked || root == "" {
		return nil
	}
	m.lspMgr.devChecked = true
	if lspinstall.DevContainerFile(root) == "" || lspinstall.DevContainerAsked(root) {
		return nil
	}
	missing := lspinstall.MissingOnly(lspinstall.DevContainerTools(root), lspinstall.Available)
	if len(missing) == 0 {
		return nil
	}
	_ = lspinstall.MarkDevContainerAsked(root)
	names := make([]string, len(missing))
	for i, t := range missing {
		names[i] = t.Name
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushClick(toast.Info,
		"This project's dev container uses tools you don't have",
		"Click to review: "+strings.Join(names, ", "), devContainerClickMsg{tools: names})
	return cmd
}

// openDevContainerTools opens the installer list with just these tools.
func (m *Model) openDevContainerTools(names []string) tea.Cmd {
	m.ensureLSPMgr()
	installing := make(map[string]bool, len(m.lspMgr.installing))
	for k, v := range m.lspMgr.installing {
		installing[k] = v
	}
	m.picker = picker.NewWith(" Dev Container Tools ", func() []picker.Item {
		return devContainerPickerItems(names, installing, lspinstall.StatusOf)
	})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindToolManager
	return m.picker.Init()
}

// devContainerPickerItems lists the named tools (any category) like the
// manager does.
func devContainerPickerItems(names []string, installing map[string]bool, status func(lspinstall.Tool) lspinstall.Status) []picker.Item {
	var items []picker.Item
	for _, n := range names {
		t, ok := lspinstall.Lookup(n)
		if !ok {
			continue
		}
		glyph, hint := toolStateLabel(t, status(t), installing[t.Name])
		items = append(items, picker.Item{ID: t.Name, Title: glyph + " " + t.DisplayName(), Hint: hint})
	}
	return items
}
