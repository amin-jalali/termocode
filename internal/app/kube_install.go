package app

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes: Helm repo add, chart install, Kustomize preview (CTX-39) ─
//
// "Kubernetes: Add Helm Repo…" and "Kubernetes: Install Helm Chart…" ask
// one line, then a card shows the command and the scope (Cancel first).
// "Kubernetes: Kustomize Preview" builds the current file's folder with
// `kubectl kustomize` into a read-only page (Secret values masked);
// nothing goes to the cluster. The AI runs none of them.

const (
	confirmKindKubeRepoAdd confirmKindEnum = 3496
	confirmKindKubeInstall confirmKindEnum = 3492
	promptKindKubeRepoAdd  promptKindEnum  = 3493
	promptKindKubeInstall  promptKindEnum  = 3494
)

// kubeHelmFind finds helm (tests replace it); kubeHelmExec nil =
// servicesExec.
var (
	kubeHelmFind               = func() string { return kube.FindHelm(nil) }
	kubeHelmExec services.Exec = nil
)

// kubeHelmTarget is what the repo or install card asks about.
type kubeHelmTarget struct {
	context, namespace      string
	repo, url               string
	release, chart, version string
}

type kubeHelmMsg struct {
	what, out string
	err       error
}

type kubeKustomizeMsg struct {
	dir, text string
	err       error
}

func (m *Model) kubeHelm(t kubeHelmTarget) kube.Helm {
	x := kubeHelmExec
	if x == nil {
		x = servicesExec
	}
	return kube.Helm{Binary: kubeHelmFind(), Context: t.context, Namespace: t.namespace, X: x}
}

// kubeHelmScope is the view's scope ("" when the view has none yet).
func (m *Model) kubeHelmScope() kubeHelmTarget {
	if m.kube == nil {
		return kubeHelmTarget{}
	}
	return kubeHelmTarget{context: m.kube.context, namespace: m.kube.namespace}
}

func (m *Model) askKubeLine(kind promptKindEnum, title, label, value, note, errText string) {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetNote(note)
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

const (
	kubeRepoLabel    = "Repo name and URL (e.g. bitnami https://charts.bitnami.com/bitnami):"
	kubeInstallLabel = "Release, chart and optional version (e.g. web bitnami/nginx 15.1.0):"
)

// dispatchKubeInstallPalette runs the CTX-39 palette ids.
func (m *Model) dispatchKubeInstallPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "kubernetes-helm-repo-add":
		if kubeHelmFind() == "" {
			return m.servicesToast(toast.Errr, "helm is not installed", "Install helm, then try again"), true
		}
		m.askKubeLine(promptKindKubeRepoAdd, " Add Helm Repo ", kubeRepoLabel, "", "A card asks before helm runs.", "")
		return nil, true
	case "kubernetes-helm-install":
		if kubeHelmFind() == "" {
			return m.servicesToast(toast.Errr, "helm is not installed", "Install helm, then try again"), true
		}
		if m.kube == nil || m.kube.context == "" {
			return m.servicesToast(toast.Info, "Open the Kubernetes view first", "The chart goes to the view's context and namespace"), true
		}
		m.askKubeLine(promptKindKubeInstall, " Install Helm Chart ", kubeInstallLabel, "", "Into "+m.kube.context+" / "+m.kube.namespace+". A card asks first.", "")
		return nil, true
	case "kubernetes-kustomize":
		dir := ""
		if m.nvim != nil {
			if s, err := m.nvim.EvalLuaString(`return vim.fn.expand('%:p:h')`); err == nil {
				dir = strings.TrimSpace(s)
			}
		}
		return m.kubeKustomize(dir), true
	}
	return nil, false
}

// handleKubeInstallPrompt takes the repo and install answers.
func (m *Model) handleKubeInstallPrompt(value string) (tea.Cmd, bool) {
	f := strings.Fields(value)
	t := m.kubeHelmScope()
	switch m.promptKind {
	case promptKindKubeRepoAdd:
		if len(f) != 2 || kube.CheckRepo(f[0], f[1]) != nil {
			why := "Type a name and a URL."
			if len(f) == 2 {
				why = kube.CheckRepo(f[0], f[1]).Error()
			}
			m.askKubeLine(promptKindKubeRepoAdd, " Add Helm Repo ", kubeRepoLabel, value, "A card asks before helm runs.", why)
			return nil, true
		}
		t.repo, t.url = f[0], f[1]
		m.pendingKubeHelm = t
		body := "This adds the chart repo " + t.repo + " to your helm repo list.\nIt does not change the cluster.\n\n" +
			"Command: helm repo add " + t.repo + " " + t.url
		m.openKubeHelmCard("Add Helm repo?", body, "add", "Add", confirmKindKubeRepoAdd)
		return nil, true
	case promptKindKubeInstall:
		ok := len(f) == 2 || len(f) == 3
		var err error
		if ok {
			f = append(f, "")
			err = kube.CheckChart(f[0], f[1], f[2])
		}
		if !ok || err != nil {
			why := "Type a release name and a chart."
			if err != nil {
				why = err.Error()
			}
			m.askKubeLine(promptKindKubeInstall, " Install Helm Chart ", kubeInstallLabel, value, "A card asks first.", why)
			return nil, true
		}
		t.release, t.chart, t.version = f[0], f[1], f[2]
		m.pendingKubeHelm = t
		body := "This installs the chart " + t.chart + " as the release " + t.release + ".\nIt creates objects in the cluster.\n\n" +
			"Context: " + t.context + " · Namespace: " + t.namespace + "\n" +
			"Command: helm " + strings.Join(kube.InstallArgs(t.release, t.chart, t.version), " ")
		m.openKubeHelmCard("Install Helm chart?", body, "install", "Install", confirmKindKubeInstall)
		return nil, true
	}
	return nil, false
}

func (m *Model) openKubeHelmCard(title, body, id, label string, kind confirmKindEnum) {
	m.confirm = confirm.Risky(title, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: id, Title: label, Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
}

// handleKubeInstallConfirm runs the repo and install cards' answers.
func (m *Model) handleKubeInstallConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindKubeRepoAdd && m.confirmKind != confirmKindKubeInstall {
		return nil, false
	}
	t := m.pendingKubeHelm
	m.pendingKubeHelm = kubeHelmTarget{}
	h := m.kubeHelm(t)
	switch {
	case m.confirmKind == confirmKindKubeRepoAdd && id == "add" && t.repo != "":
		return func() tea.Msg {
			return kubeHelmMsg{what: "repo " + t.repo, err: h.RepoAdd(context.Background(), t.repo, t.url)}
		}, true
	case m.confirmKind == confirmKindKubeInstall && id == "install" && t.release != "":
		return func() tea.Msg {
			out, err := h.Install(context.Background(), t.release, t.chart, t.version)
			return kubeHelmMsg{what: "release " + t.release, out: out, err: err}
		}, true
	}
	return nil, true
}

func (m *Model) onKubeHelm(msg kubeHelmMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "helm failed for "+msg.what, msg.err.Error())
	}
	if msg.out != "" {
		if err := m.openReadOnlyPage("helm: "+msg.what, msg.out, "", ""); err != nil {
			return m.servicesToast(toast.Errr, "Cannot open the helm page", err.Error())
		}
	}
	var reload tea.Cmd
	if m.kube != nil && strings.HasPrefix(msg.what, "release ") {
		reload = m.kubeReload()
	}
	return tea.Batch(m.servicesToast(toast.Info, "helm: done", msg.what), reload)
}

// kubeKustomize builds dir (the current file's folder) into a page.
func (m *Model) kubeKustomize(dir string) tea.Cmd {
	if dir == "" || !filepath.IsAbs(dir) {
		return m.servicesToast(toast.Info, "Open a file of the kustomization first", "The preview builds the current file's folder")
	}
	bin := ""
	if m.kube != nil {
		bin = m.kube.binary
	}
	if bin == "" {
		bin = kube.Find(nil)
	}
	c := kube.Client{Binary: bin, X: servicesExec}
	return func() tea.Msg {
		text, err := c.Kustomize(context.Background(), dir)
		return kubeKustomizeMsg{dir: dir, text: text, err: err}
	}
}

func (m *Model) onKubeKustomize(msg kubeKustomizeMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot build the kustomization", msg.err.Error())
	}
	if err := m.openReadOnlyPage("kustomize: "+filepath.Base(msg.dir), msg.text, "", "yaml"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the preview", err.Error())
	}
	return nil
}
