package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Kubernetes view: palette and actions ─────────────────────────────────

// kubePaletteItems are the "Kubernetes: …" palette rows.
func kubePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "kubernetes-show", Title: "Kubernetes: Show"},
		{ID: "kubernetes-refresh", Title: "Kubernetes: Refresh"},
	}
}

// dispatchKubePalette runs the "Kubernetes: …" palette commands.
func (m *Model) dispatchKubePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "kubernetes-show":
		return m.revealKube(), true
	case "kubernetes-refresh":
		return tea.Batch(m.revealKube(), m.kubeReload()), true
	}
	return nil, false
}
