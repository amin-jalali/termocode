package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes: Add Cluster to Kubeconfig (CTX-39) ───────────────────
//
// The one place termocode writes the kubeconfig, and only after your OK:
// one line asks for "name server [ca-file]", a card shows the exact diff
// (Cancel first), and on OK the old file is copied to a timestamped
// backup next to it before the new one is written. The AI never runs it.

const (
	confirmKindKubeAddCluster confirmKindEnum = 3497
	promptKindKubeAddCluster  promptKindEnum  = 3497
)

const kubeAddClusterLabel = "Name, server URL and optional CA file (e.g. dev https://10.0.0.1:6443 /etc/ca.pem):"

// kubeAddClusterPlan is what the card shows and the OK writes.
type kubeAddClusterPlan struct {
	path string
	data []byte
}

type kubeAddClusterMsg struct {
	name, path, backup string
	err                error
}

func kubeconfigFile() string {
	home, _ := os.UserHomeDir()
	return kube.KubeconfigPath(os.Getenv, home)
}

func (m *Model) askKubeAddCluster(value, errText string) tea.Cmd {
	m.askKubeLine(promptKindKubeAddCluster, " Add Cluster ", kubeAddClusterLabel, value,
		"Adds a cluster and a context. A card shows the change first.", errText)
	return nil
}

func (m *Model) handleKubeAddClusterPrompt(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindKubeAddCluster {
		return nil, false
	}
	f := strings.Fields(value)
	if len(f) < 2 || len(f) > 3 {
		return m.askKubeAddCluster(value, "Type a name and a server URL."), true
	}
	a := kube.ClusterAdd{Name: f[0], Server: f[1]}
	if len(f) == 3 {
		a.CAFile = f[2]
	}
	path := kubeconfigFile()
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return m.askKubeAddCluster(value, err.Error()), true
	}
	out, err := kube.PlanAddCluster(old, a)
	if err != nil {
		return m.askKubeAddCluster(value, err.Error()), true
	}
	m.pendingKubeAdd = kubeAddClusterPlan{path: path, data: out}
	backup := "A backup of the old file is saved next to it first."
	if len(old) == 0 {
		backup = "The file is new, so there is nothing to back up."
	}
	body := "This adds the cluster " + a.Name + " and a context of the same name to\n" + path + "\n" +
		"Nothing else changes. " + backup + "\nAdd credentials with your cluster's own tools."
	m.confirm = confirm.Risky("Add cluster to kubeconfig?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "add-cluster", Title: "Add", Style: confirm.StyleDestructive}).
		WithDiff(strings.Split(kube.DiffContext(string(old), string(out), 1), "\n"))
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeAddCluster
	return nil, true
}

func (m *Model) handleKubeAddClusterConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindKubeAddCluster {
		return nil, false
	}
	plan := m.pendingKubeAdd
	m.pendingKubeAdd = kubeAddClusterPlan{}
	if id != "add-cluster" || plan.path == "" {
		return nil, true
	}
	return func() tea.Msg {
		backup, err := kube.WriteKubeconfig(plan.path, plan.data, time.Now())
		return kubeAddClusterMsg{path: plan.path, backup: backup, err: err}
	}, true
}

func (m *Model) onKubeAddCluster(msg kubeAddClusterMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot add the cluster", msg.err.Error())
	}
	detail := "Backup: " + filepath.Base(msg.backup)
	if msg.backup == "" {
		detail = "New file: " + msg.path
	}
	var reload tea.Cmd
	if m.kube != nil {
		reload = m.kubeReload()
	}
	return tea.Batch(m.servicesToast(toast.Info, "Cluster added to the kubeconfig", detail), reload)
}
