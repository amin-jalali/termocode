package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: pod actions ─────────────────────────────────────────
//
// Logs and Shell run kubectl in a terminal tab (the live log UI of the
// Containers view); a pod with more than one container asks which one
// first. Describe opens a read-only page. Nothing here changes the
// cluster.

// selectedPod is the pod under the cursor.
func (st *kubeState) selectedPod() (kube.Pod, bool) {
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem || r.section != "pods" || r.idx >= len(st.pods) {
		return kube.Pod{}, false
	}
	return st.pods[r.idx], true
}

func (st *kubeState) podByName(name string) (kube.Pod, bool) {
	for _, p := range st.pods {
		if p.Name == name {
			return p, true
		}
	}
	return kube.Pod{}, false
}

// kubeKindOf is kubectl's resource name of a section.
func kubeKindOf(section string) string {
	switch section {
	case "pods":
		return "pod"
	case "deployments":
		return "deployment"
	case "services":
		return "service"
	case "jobs":
		return "job"
	}
	return ""
}

// kubeSpawnJob opens a terminal tab (tests replace it: they have no
// editor to run the tab in).
var kubeSpawnJob = func(m *Model, j terminalJob) error {
	_, err := m.spawnTerminalJob(j)
	return err
}

// kubeTerminalJob is the terminal tab of logs or a shell (kind "logs" or
// "shell"). Its key holds the scope, so another context's tab is another
// tab.
func (st *kubeState) kubeTerminalJob(kind, pod, container string) terminalJob {
	c := st.client()
	cmd := c.LogsCommand(pod, container)
	if kind == "shell" {
		cmd = c.ShellCommand(pod, container)
	}
	key := "k8s:" + kind + ":" + st.context + "/" + st.namespace + "/" + pod + "/" + container
	return terminalJob{Label: cmd.Label, Cmdline: svcShellLine(cmd.Argv), Service: key}
}

// kubePodTerminal opens logs or a shell; a pod with more containers asks
// which one first.
func (m *Model) kubePodTerminal(kind string, p kube.Pod, container string) tea.Cmd {
	if container == "" && len(p.Containers) > 1 {
		items := make([]picker.Item, 0, len(p.Containers))
		for _, c := range p.Containers {
			items = append(items, picker.Item{ID: "k8s:" + kind + ":" + p.Name + "/" + c, Title: c, Hint: p.Name})
		}
		title := " Logs of Container "
		if kind == "shell" {
			title = " Shell in Container "
		}
		m.openTasksPickerItems(title, items)
		return nil
	}
	if container == "" && len(p.Containers) == 1 {
		container = p.Containers[0]
	}
	job := m.kube.kubeTerminalJob(kind, p.Name, container)
	if m.servicesFocusTab(job.Service) {
		return nil
	}
	if err := kubeSpawnJob(m, job); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open "+kind, err.Error())
	}
	return nil
}

// handleKubePodPick runs a picked container: "logs" or "shell" with
// value "<pod>/<container>".
func (m *Model) handleKubePodPick(kind, value string) tea.Cmd {
	pod, container, _ := strings.Cut(value, "/")
	p, ok := m.kube.podByName(pod)
	if !ok {
		return m.servicesToast(toast.Info, "The pod is gone", pod)
	}
	switch kind {
	case "logs", "shell":
		return m.kubePodTerminal(kind, p, container)
	case "pf":
		port, err := strconv.Atoi(container)
		if err != nil || port <= 0 {
			return nil
		}
		return m.kubeToggleForward(p, port)
	}
	return nil
}

type kubeDescribeMsg struct {
	kind, name, text string
	err              error
}

// kubeDescribe reads `kubectl describe` of one object.
func (m *Model) kubeDescribe(kind, name string) tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		text, err := c.Describe(context.Background(), kind, name)
		return kubeDescribeMsg{kind: kind, name: name, text: text, err: err}
	}
}

func (m *Model) onKubeDescribe(msg kubeDescribeMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot describe "+msg.kind, msg.err.Error())
	}
	if err := m.openReadOnlyPage("describe: "+msg.kind+"/"+msg.name, msg.text, "", ""); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the describe page", err.Error())
	}
	return nil
}

// kubeCopyName copies the object's name.
func (m *Model) kubeCopyName(name string) tea.Cmd {
	m.copyToClipboard(name)
	return m.servicesToast(toast.Info, "Copied name", name)
}
