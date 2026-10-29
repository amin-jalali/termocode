package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: restart a deployment ────────────────────────────────
//
// r on a deployment asks first (Cancel is the first button and has the
// focus; Esc cancels), then runs `kubectl rollout restart`. It is the
// view's only write; the AI can not reach it (no palette id).

// confirmKindKubeRestart is the restart card (kube_restart.go).
const confirmKindKubeRestart confirmKindEnum = 3470

// kubeRestartTarget is the deployment a restart card asks about, in the
// scope it was asked in.
type kubeRestartTarget struct {
	context, namespace, name string
}

type kubeRestartMsg struct {
	name string
	err  error
}

// selectedDeployment is the deployment under the cursor.
func (st *kubeState) selectedDeployment() (kube.Deployment, bool) {
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem || r.section != "deployments" || r.idx >= len(st.deploys) {
		return kube.Deployment{}, false
	}
	return st.deploys[r.idx], true
}

// askKubeRestart opens the restart card.
func (m *Model) askKubeRestart(d kube.Deployment) {
	st := m.kube
	st.pendingRestart = kubeRestartTarget{context: st.context, namespace: st.namespace, name: d.Name}
	body := "This restarts every pod of " + d.Name + ", one by one.\n\n" +
		"Context: " + st.context + " · Namespace: " + st.namespace + "\n" +
		"Command: kubectl rollout restart deployment/" + d.Name
	m.confirm = confirm.Risky("Restart deployment?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "restart", Title: "Restart", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeRestart
}

// handleKubeConfirm runs the restart card's answer.
func (m *Model) handleKubeConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindKubeRestart || m.kube == nil {
		return nil, false
	}
	target := m.kube.pendingRestart
	m.kube.pendingRestart = kubeRestartTarget{}
	if id != "restart" || target.name == "" {
		return nil, true
	}
	c := m.kube.client()
	c.Context, c.Namespace = target.context, target.namespace // the scope the card showed
	return func() tea.Msg {
		return kubeRestartMsg{name: target.name, err: c.RolloutRestart(context.Background(), target.name)}
	}, true
}

func (m *Model) onKubeRestart(msg kubeRestartMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot restart "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Restarting "+msg.name, "Its pods are replaced one by one"), m.kubeReload())
}
