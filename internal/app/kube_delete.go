package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: delete one object ───────────────────────────────────
//
// d on a pod, deployment, service or job asks first (Cancel is the first
// button and has the focus; Enter and Esc cancel), then runs `kubectl
// delete <kind> <name> --wait=false` in the scope the card showed. The AI
// can not reach it (no palette id, no kube tool).

// confirmKindKubeDelete is the delete card (kube_delete.go).
const confirmKindKubeDelete confirmKindEnum = 3471

// kubeDeleteTarget is the object a delete card asks about, in the scope
// it was asked in.
type kubeDeleteTarget struct {
	context, namespace, kind, name string
}

type kubeDeleteMsg struct {
	kind, name string
	err        error
}

// kubeDeleteNote says what else goes with the object.
func kubeDeleteNote(kind string) string {
	switch kind {
	case "pod":
		return "A pod of a deployment, job or stateful set is made again."
	case "deployment":
		return "Its pods are deleted too."
	case "job":
		return "Its pods are deleted too."
	case "service":
		return "Its pods stay; traffic to the service stops."
	}
	return ""
}

// askKubeDelete opens the delete card.
func (m *Model) askKubeDelete(kind, name string) {
	st := m.kube
	if kind == "" || name == "" {
		return
	}
	st.pendingDelete = kubeDeleteTarget{context: st.context, namespace: st.namespace, kind: kind, name: name}
	body := "This deletes " + kind + " " + name + " from the cluster. It can not be undone."
	if note := kubeDeleteNote(kind); note != "" {
		body += "\n" + note
	}
	body += "\n\nContext: " + st.context + " · Namespace: " + st.namespace + "\n" +
		"Command: kubectl delete " + kind + " " + name
	m.confirm = confirm.Risky("Delete "+kind+"?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeDelete
}

// handleKubeDeleteConfirm runs the delete card's answer.
func (m *Model) handleKubeDeleteConfirm(id string) tea.Cmd {
	target := m.kube.pendingDelete
	m.kube.pendingDelete = kubeDeleteTarget{}
	if id != "delete" || target.name == "" {
		return nil
	}
	c := m.kube.client()
	c.Context, c.Namespace = target.context, target.namespace // the scope the card showed
	return func() tea.Msg {
		return kubeDeleteMsg{kind: target.kind, name: target.name, err: c.Delete(context.Background(), target.kind, target.name)}
	}
}

func (m *Model) onKubeDelete(msg kubeDeleteMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot delete "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Deleting "+msg.kind+" "+msg.name, "The list shows it until it is gone"), m.kubeReload())
}
