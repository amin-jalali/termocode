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
		{ID: "kubernetes-context", Title: "Kubernetes: Switch Context…"},
		{ID: "kubernetes-namespace", Title: "Kubernetes: Switch Namespace…"},
		{ID: "kubernetes-snippet", Title: "Kubernetes: New Manifest from Snippet…"},
		{ID: "kubernetes-kustomize", Title: "Kubernetes: Kustomize Preview"},
		{ID: "kubernetes-helm-repo-add", Title: "Kubernetes: Add Helm Repo…"},
		{ID: "kubernetes-helm-install", Title: "Kubernetes: Install Helm Chart…"},
	}
}

// dispatchKubePalette runs the "Kubernetes: …" palette commands.
func (m *Model) dispatchKubePalette(id string) (tea.Cmd, bool) {
	if cmd, ok := m.dispatchKubeInstallPalette(id); ok { // kube_install.go
		return cmd, true
	}
	switch id {
	case "kubernetes-snippet":
		m.openTasksPickerItems(" New Manifest ", kubeSnippetItems())
		return nil, true
	case "kubernetes-show":
		return m.revealKube(), true
	case "kubernetes-refresh":
		m.revealKube() // its load is replaced by the reload (a cmd never run starts nothing)
		return m.kubeReload(), true
	case "kubernetes-context", "kubernetes-namespace":
		kind := "ctx"
		if id == "kubernetes-namespace" {
			kind = "ns"
		}
		open := m.revealKube()
		if m.kube.binary == "" {
			// Not found yet: the view loads and says why; c and n
			// work in it once kubectl is there.
			return open, true
		}
		return tea.Batch(open, m.kubePick(kind)), true
	}
	return nil, false
}
