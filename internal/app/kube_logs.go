package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: log options and saving logs (CTX-4) ─────────────────
//
// L on a pod picks how to show its logs: follow, the container before the
// last restart, the last 5 minutes or hour, with time stamps, or save
// every line to a file (asks the name; never overwrites a file). L on a
// deployment follows the logs of all its pods in one tab. Everything is
// read only.

// promptKindKubeSaveLogs asks the file for saved logs.
const promptKindKubeSaveLogs promptKindEnum = 4471

// kubeLogOpts are the pod options, in picker order.
var kubeLogOpts = []struct{ id, title, hint string }{
	{"follow", "Follow logs", "the last 200 lines, then new ones"},
	{"previous", "Previous container", "the logs before the last restart"},
	{"since5m", "Last 5 minutes", "then new ones"},
	{"since1h", "Last hour", "then new ones"},
	{"timestamps", "With time stamps", "a time before each line"},
	{"save", "Save to a file…", "every line, once"},
}

// kubeLogOptions are the kubectl options of an option id.
func kubeLogOptions(id string) kube.LogOptions {
	switch id {
	case "previous":
		return kube.LogOptions{Previous: true}
	case "since5m":
		return kube.LogOptions{Since: 5 * time.Minute}
	case "since1h":
		return kube.LogOptions{Since: time.Hour}
	case "timestamps":
		return kube.LogOptions{Timestamps: true}
	}
	return kube.LogOptions{}
}

// kubeLogsTarget is the pod (and container) a save prompt asks about, in
// the scope it was asked in.
type kubeLogsTarget struct {
	context, namespace, pod, container string
}

type kubeLogsSavedMsg struct {
	path string
	err  error
}

// kubeAskLogOptions is L: the options picker of a pod or a deployment.
func (m *Model) kubeAskLogOptions() tea.Cmd {
	st := m.kube
	if p, ok := st.selectedPod(); ok {
		items := make([]picker.Item, 0, len(kubeLogOpts))
		for _, o := range kubeLogOpts {
			items = append(items, picker.Item{ID: "k8s:logopt:" + p.Name + "|" + o.id, Title: o.title, Hint: o.hint})
		}
		m.openTasksPickerItems(" Logs of "+p.Name+" ", items)
		return nil
	}
	if d, ok := st.selectedDeployment(); ok {
		var items []picker.Item
		for _, o := range kubeLogOpts {
			if o.id == "save" {
				continue
			}
			title := "All pods: " + strings.ToLower(o.title[:1]) + o.title[1:]
			items = append(items, picker.Item{ID: "k8s:dlog:" + d.Name + "|" + o.id, Title: title, Hint: o.hint})
		}
		m.openTasksPickerItems(" Logs of deployment "+d.Name+" ", items)
		return nil
	}
	return nil
}

// handleKubeLogPick runs a picked log option: "logopt" (value
// "<pod>|<opt>"), "logc" ("<opt>|<pod>/<container>") or "dlog"
// ("<deployment>|<opt>"). ok=false: not a log pick.
func (m *Model) handleKubeLogPick(kind, value string) (tea.Cmd, bool) {
	switch kind {
	case "logopt":
		pod, opt, _ := strings.Cut(value, "|")
		p, ok := m.kube.podByName(pod)
		if !ok {
			return m.servicesToast(toast.Info, "The pod is gone", pod), true
		}
		if len(p.Containers) > 1 {
			items := make([]picker.Item, 0, len(p.Containers))
			for _, c := range p.Containers {
				items = append(items, picker.Item{ID: "k8s:logc:" + opt + "|" + p.Name + "/" + c, Title: c, Hint: p.Name})
			}
			m.openTasksPickerItems(" Logs of Container ", items)
			return nil, true
		}
		container := ""
		if len(p.Containers) == 1 {
			container = p.Containers[0]
		}
		return m.kubeLogsWith(opt, p.Name, container), true
	case "logc":
		opt, rest, _ := strings.Cut(value, "|")
		pod, container, _ := strings.Cut(rest, "/")
		if _, ok := m.kube.podByName(pod); !ok {
			return m.servicesToast(toast.Info, "The pod is gone", pod), true
		}
		return m.kubeLogsWith(opt, pod, container), true
	case "dlog":
		name, opt, _ := strings.Cut(value, "|")
		st := m.kube
		cmd := st.client().DeploymentLogsCommand(name, kubeLogOptions(opt))
		key := "k8s:dlogs:" + opt + ":" + st.context + "/" + st.namespace + "/" + name
		return m.kubeOpenLogTab(terminalJob{Label: cmd.Label, Cmdline: svcShellLine(cmd.Argv), Service: key}), true
	}
	return nil, false
}

// kubeLogsWith opens one pod's logs with an option, or asks the file for
// "save".
func (m *Model) kubeLogsWith(opt, pod, container string) tea.Cmd {
	st := m.kube
	if opt == "save" {
		st.pendingLogs = kubeLogsTarget{context: st.context, namespace: st.namespace, pod: pod, container: container}
		name := pod
		if container != "" {
			name += "-" + container
		}
		name += "-" + time.Now().Format("20060102-150405") + ".log"
		return m.openHTTPPrompt(promptKindKubeSaveLogs, "Save logs", "File:", filepath.Join(aiWorkspaceRoot(), name))
	}
	cmd := st.client().LogsCommandWith(pod, container, kubeLogOptions(opt))
	key := "k8s:logs:" + st.context + "/" + st.namespace + "/" + pod + "/" + container
	if opt != "follow" {
		key += ":" + opt
	}
	return m.kubeOpenLogTab(terminalJob{Label: cmd.Label, Cmdline: svcShellLine(cmd.Argv), Service: key})
}

func (m *Model) kubeOpenLogTab(job terminalJob) tea.Cmd {
	if m.servicesFocusTab(job.Service) {
		return nil
	}
	if err := kubeSpawnJob(m, job); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open logs", err.Error())
	}
	return nil
}

// kubeSaveLogs reads every line of the asked pod's logs once and writes
// them to path (relative to the workspace). An existing file is kept.
func (m *Model) kubeSaveLogs(path string) tea.Cmd {
	t := m.kube.pendingLogs
	m.kube.pendingLogs = kubeLogsTarget{}
	path = strings.TrimSpace(path)
	if path == "" || t.pod == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(aiWorkspaceRoot(), path)
	}
	c := m.kube.client()
	c.Context, c.Namespace = t.context, t.namespace // the scope it was asked in
	return func() tea.Msg {
		if _, err := os.Lstat(path); err == nil {
			return kubeLogsSavedMsg{path: path, err: os.ErrExist}
		}
		text, err := c.LogsWith(context.Background(), t.pod, t.container, -1, kube.LogOptions{})
		if err != nil {
			return kubeLogsSavedMsg{path: path, err: err}
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return kubeLogsSavedMsg{path: path, err: err}
		}
		_, err = f.WriteString(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return kubeLogsSavedMsg{path: path, err: err}
	}
}

func (m *Model) onKubeLogsSaved(msg kubeLogsSavedMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, os.ErrExist):
		return m.servicesToast(toast.Warn, "That file already exists", "Pick another name.")
	case msg.err != nil:
		return m.servicesToast(toast.Errr, "Cannot save the logs", msg.err.Error())
	}
	return m.servicesToast(toast.Info, "Saved the logs", msg.path)
}
