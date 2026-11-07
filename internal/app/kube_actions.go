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
		{ID: "kubernetes-top", Title: "Kubernetes: Show Metrics"},
		{ID: "kubernetes-helm", Title: "Kubernetes: Helm Releases…"},
		{ID: "kubernetes-kind", Title: "Kubernetes: Show Kind…"},
		{ID: "kubernetes-snippet", Title: "Kubernetes: New Manifest from Snippet…"},
		{ID: "kubernetes-apply-file", Title: "Kubernetes: Apply Current File…"},
		{ID: "kubernetes-report", Title: "Kubernetes: Cluster Report"},
		{ID: "kubernetes-rbac", Title: "Kubernetes: Permissions (RBAC)"},
		{ID: "kubernetes-nodes", Title: "Kubernetes: Nodes…"},
		{ID: "kubernetes-hpa", Title: "Kubernetes: Autoscalers (HPA)"},
	}
}

// dispatchKubePalette runs the "Kubernetes: …" palette commands.
func (m *Model) dispatchKubePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "kubernetes-nodes":
		return m.kubeNodes(), true
	case "kubernetes-hpa":
		return m.kubeHPA(), true
	case "kubernetes-rbac":
		return m.kubeRBAC(), true
	case "kubernetes-report":
		return m.kubeReport(), true
	case "kubernetes-apply-file":
		return m.kubeApplyCurrentFile(), true
	case "kubernetes-snippet":
		m.openTasksPickerItems(" New Manifest ", kubeSnippetItems())
		return nil, true
	case "kubernetes-show":
		return m.revealKube(), true
	case "kubernetes-kind":
		open := m.revealKube()
		if m.kube.binary == "" {
			return open, true // the view loads and says why
		}
		return tea.Batch(open, m.kubeAskKind()), true
	case "kubernetes-helm":
		open := m.revealKube()
		if m.kube.binary == "" {
			return open, true // the view loads and says why
		}
		return tea.Batch(open, m.kubeHelmList()), true
	case "kubernetes-top":
		open := m.revealKube()
		if m.kube.binary == "" {
			return open, true // the view loads and says why
		}
		return tea.Batch(open, m.kubeTop()), true
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
