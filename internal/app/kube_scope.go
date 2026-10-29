package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: context and namespace pickers ───────────────────────
//
// c picks a context, n a namespace. The choice is this session's only: it
// goes to kubectl as --context / --namespace; kubeconfig is not written.

type kubePickMsg struct {
	kind    string // "ctx" or "ns"
	names   []string
	current string
	err     error
}

// kubePick lists the contexts (kind "ctx") or the namespaces ("ns"), then
// opens the picker.
func (m *Model) kubePick(kind string) tea.Cmd {
	st := m.kube
	if st == nil || st.binary == "" {
		return m.servicesToast(toast.Info, "kubectl is not installed", "")
	}
	c := st.client()
	current := st.context
	if kind == "ns" {
		current = st.namespace
	}
	return func() tea.Msg {
		ctx := context.Background() // each call has its own timeout
		msg := kubePickMsg{kind: kind, current: current}
		if kind == "ctx" {
			msg.names, msg.err = c.Contexts(ctx)
		} else {
			msg.names, msg.err = c.Namespaces(ctx)
		}
		return msg
	}
}

func (m *Model) onKubePick(msg kubePickMsg) tea.Cmd {
	what := "contexts"
	if msg.kind == "ns" {
		what = "namespaces"
	}
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list "+what, msg.err.Error())
	}
	if len(msg.names) == 0 {
		return m.servicesToast(toast.Info, "No "+what+" in kubeconfig", "")
	}
	title := " Kubernetes Context "
	if msg.kind == "ns" {
		title = " Namespace "
	}
	m.openTasksPickerItems(title, kubePickItems(msg.kind, msg.names, msg.current))
	return nil
}

// kubePickItems are the picker rows; the current one says so.
func kubePickItems(kind string, names []string, current string) []picker.Item {
	items := make([]picker.Item, 0, len(names))
	for _, n := range names {
		it := picker.Item{ID: "k8s:" + kind + ":" + n, Title: n}
		if n == current {
			it.Hint = "current"
		}
		items = append(items, it)
	}
	return items
}

// handleKubePickerSelect runs a picked "k8s:<kind>:<value>".
func (m *Model) handleKubePickerSelect(id string) tea.Cmd {
	kind, value, _ := strings.Cut(strings.TrimPrefix(id, "k8s:"), ":")
	st := m.kube
	switch kind {
	case "ctx":
		if value == st.context {
			return nil
		}
		// The new context's own namespace is looked up on load.
		return m.kubeRescope(value, "")
	case "ns":
		if value == st.namespace {
			return nil
		}
		return m.kubeRescope(st.context, value)
	}
	return m.handleKubePodPick(kind, value)
}
