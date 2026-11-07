package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: Helm releases (CTX-17) ──────────────────────────────
//
// H in the view (or Kubernetes: Helm Releases…) lists the helm releases
// of the view's context and namespace in a picker. A release opens a
// second picker: Status, Values (secret-looking values masked) and
// History open read-only pages; Roll Back… lists the older revisions and
// Uninstall… asks first. Both writes show a card where Cancel is the
// first button and has the focus. No helm installed: a hint, nothing
// else. The AI can not reach any of it (the palette id opens a picker).

const confirmKindKubeHelm confirmKindEnum = 3480

// Test seams: the helm runner and the helm lookup.
var (
	kubeHelmExec services.Exec // nil = servicesExec
	kubeFindHelm = func() string { return kube.FindHelm(nil) }
)

// kubeHelmTarget is what a helm card asks about: the release (and
// revision) for roll back / uninstall (CTX-17), or the repo / chart for
// repo add / install (CTX-39, kube_install.go).
type kubeHelmTarget struct {
	action                   string // "rollback" or "uninstall"
	context, namespace, name string
	revision, current        int
	repo, url                string
	release, chart, version  string
}

type kubeHelmListMsg struct {
	releases []kube.Release
	err      error
}

type kubeHelmPageMsg struct {
	title, text, filetype string
	err                   error
}

type kubeHelmHistoryMsg struct {
	namespace, name string
	revs            []kube.Revision
	err             error
}

type kubeHelmWriteMsg struct {
	action, name string
	revision     int
	err          error
}

// helm is the helm CLI for the view's context and namespace ns ("" =
// the view's namespace).
func (st *kubeState) helm(bin, ns string) kube.Helm {
	x := kubeHelmExec
	if x == nil {
		x = servicesExec
	}
	if ns == "" {
		ns = st.namespace
	}
	return kube.Helm{Binary: bin, Context: st.context, Namespace: ns, X: x}
}

// kubeHelmBinary finds helm, or returns the hint toast.
func (m *Model) kubeHelmBinary() (string, tea.Cmd) {
	bin := kubeFindHelm()
	if bin == "" {
		return "", m.servicesToast(toast.Info, "helm is not installed", "Install helm to see the releases here")
	}
	return bin, nil
}

// kubeHelmList lists the releases of the view's namespace.
func (m *Model) kubeHelmList() tea.Cmd {
	bin, hint := m.kubeHelmBinary()
	if bin == "" {
		return hint
	}
	h := m.kube.helm(bin, "")
	return func() tea.Msg {
		rs, err := h.Releases(context.Background())
		return kubeHelmListMsg{releases: rs, err: err}
	}
}

func (m *Model) onKubeHelmList(msg kubeHelmListMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list the helm releases", msg.err.Error())
	}
	if len(msg.releases) == 0 {
		return m.servicesToast(toast.Info, "No helm releases in "+m.kube.namespace, "")
	}
	m.openTasksPickerItems(" Helm Releases ", kubeHelmReleaseItems(msg.releases))
	return nil
}

// kubeHelmReleaseItems are the release picker's rows.
func kubeHelmReleaseItems(rs []kube.Release) []picker.Item {
	items := make([]picker.Item, 0, len(rs))
	for _, r := range rs {
		items = append(items, picker.Item{ID: "k8s:helm:" + r.Namespace + "/" + r.Name,
			Title: r.Name + " · " + r.Chart, Hint: r.Status + " · rev " + strconv.Itoa(r.Revision)})
	}
	return items
}

// kubeHelmActionItems are one release's rows.
func kubeHelmActionItems(ns, name string) []picker.Item {
	ref := ns + "/" + name
	return []picker.Item{
		{ID: "k8s:helm-status:" + ref, Title: "Status", Hint: "read only"},
		{ID: "k8s:helm-values:" + ref, Title: "Values", Hint: "read only, secrets masked"},
		{ID: "k8s:helm-history:" + ref, Title: "History", Hint: "read only"},
		{ID: "k8s:helm-rollback:" + ref, Title: "Roll Back…", Hint: "pick a revision, asks first"},
		{ID: "k8s:helm-uninstall:" + ref, Title: "Uninstall…", Hint: "asks first"},
	}
}

// handleKubeHelmPick runs a helm picker row. ok=false: not ours.
func (m *Model) handleKubeHelmPick(kind, value string) (tea.Cmd, bool) {
	if !strings.HasPrefix(kind, "helm") {
		return nil, false
	}
	ns, name, _ := strings.Cut(value, "/")
	if kind == "helm" {
		m.openTasksPickerItems(" Helm: "+name+" ", kubeHelmActionItems(ns, name))
		return nil, true
	}
	if kind == "helm-uninstall" {
		m.askKubeHelm(kubeHelmTarget{action: "uninstall", name: name, namespace: ns})
		return nil, true
	}
	if kind == "helm-rb" {
		i := strings.LastIndex(name, ":")
		if i < 0 {
			return nil, true
		}
		rev, err := strconv.Atoi(name[i+1:])
		if err != nil || rev < 1 {
			return nil, true
		}
		m.askKubeHelm(kubeHelmTarget{action: "rollback", name: name[:i], namespace: ns, revision: rev, current: m.kube.helmCurrent})
		return nil, true
	}
	bin, hint := m.kubeHelmBinary()
	if bin == "" {
		return hint, true
	}
	h := m.kube.helm(bin, ns)
	switch kind {
	case "helm-status":
		return func() tea.Msg {
			s, err := h.Status(context.Background(), name)
			return kubeHelmPageMsg{title: "helm status: " + name, text: kubeHelmStatusText(s), err: err}
		}, true
	case "helm-values":
		return func() tea.Msg {
			v, err := h.Values(context.Background(), name, 0)
			return kubeHelmPageMsg{title: "helm values: " + name, text: v, filetype: "yaml", err: err}
		}, true
	case "helm-history":
		return func() tea.Msg {
			revs, err := h.History(context.Background(), name)
			return kubeHelmPageMsg{title: "helm history: " + name, text: kubeHelmHistoryText(name, revs), err: err}
		}, true
	case "helm-rollback":
		return func() tea.Msg {
			revs, err := h.History(context.Background(), name)
			return kubeHelmHistoryMsg{namespace: ns, name: name, revs: revs, err: err}
		}, true
	}
	return nil, true
}

func kubeHelmStatusText(s kube.ReleaseStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release      %s\nNamespace    %s\nRevision     %d\nStatus       %s\nChart        %s\nApp version  %s\n",
		s.Name, s.Namespace, s.Revision, s.Status, s.Chart, s.AppVersion)
	if s.Description != "" {
		fmt.Fprintf(&b, "Description  %s\n", s.Description)
	}
	if s.FirstDeployed != "" {
		fmt.Fprintf(&b, "First        %s\n", s.FirstDeployed)
	}
	if s.LastDeployed != "" {
		fmt.Fprintf(&b, "Last         %s\n", s.LastDeployed)
	}
	if strings.TrimSpace(s.Notes) != "" {
		b.WriteString("\nNOTES\n" + strings.TrimRight(s.Notes, "\n") + "\n")
	}
	return b.String()
}

func kubeHelmHistoryText(name string, revs []kube.Revision) string {
	var b strings.Builder
	b.WriteString("Release " + name + ", newest first\n\n")
	if len(revs) == 0 {
		b.WriteString("No revisions\n")
	}
	for _, r := range revs {
		line := fmt.Sprintf("%-4d %-12s %-24s %s", r.Revision, r.Status, r.Chart, r.Description)
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

func (m *Model) onKubeHelmPage(msg kubeHelmPageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read "+msg.title, msg.err.Error())
	}
	if err := m.openReadOnlyPage(msg.title, msg.text, "", msg.filetype); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the page", err.Error())
	}
	return nil
}

func (m *Model) onKubeHelmHistory(msg kubeHelmHistoryMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read the history of "+msg.name, msg.err.Error())
	}
	items := kubeHelmRollbackItems(msg)
	if len(items) == 0 {
		return m.servicesToast(toast.Info, msg.name+" has no older revision", "Nothing to roll back to")
	}
	m.kube.helmCurrent = 0
	if len(msg.revs) > 0 {
		m.kube.helmCurrent = msg.revs[0].Revision
	}
	m.openTasksPickerItems(" Roll back "+msg.name+" to ", items)
	return nil
}

// kubeHelmRollbackItems are the older revisions (the newest is the
// current one and is left out).
func kubeHelmRollbackItems(msg kubeHelmHistoryMsg) []picker.Item {
	var items []picker.Item
	for i, r := range msg.revs { // newest first
		if i == 0 {
			continue
		}
		items = append(items, picker.Item{ID: "k8s:helm-rb:" + msg.namespace + "/" + msg.name + ":" + strconv.Itoa(r.Revision),
			Title: "Revision " + strconv.Itoa(r.Revision) + " · " + r.Chart, Hint: r.Status})
	}
	return items
}

// askKubeHelm opens the rollback or uninstall card.
func (m *Model) askKubeHelm(t kubeHelmTarget) {
	st := m.kube
	t.context = st.context
	st.pendingHelm = t
	scope := "Context: " + t.context + " · Namespace: " + t.namespace + "\n"
	var title, body string
	var ok confirm.Button
	if t.action == "uninstall" {
		title = "Uninstall release?"
		body = "This deletes the release " + t.name + " and every object its chart made.\n\n" +
			scope + "Command: helm uninstall " + t.name
		ok = confirm.Button{ID: "uninstall", Title: "Uninstall", Style: confirm.StyleDestructive}
	} else {
		title = "Roll back release?"
		what := "This rolls " + t.name + " back to revision " + strconv.Itoa(t.revision)
		if t.current > 0 {
			what += " (now " + strconv.Itoa(t.current) + ")"
		}
		body = what + " and waits for its pods.\n\n" + scope +
			"Command: helm rollback " + t.name + " " + strconv.Itoa(t.revision) + " --wait"
		ok = confirm.Button{ID: "rollback", Title: "Roll Back", Style: confirm.StyleDestructive}
	}
	m.confirm = confirm.Risky(title, body, confirm.Button{ID: "cancel", Title: "Cancel"}, ok)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeHelm
}

func (m *Model) answerKubeHelm(id string) tea.Cmd {
	t := m.kube.pendingHelm
	m.kube.pendingHelm = kubeHelmTarget{}
	if t.name == "" || id != t.action {
		return nil
	}
	bin, hint := m.kubeHelmBinary()
	if bin == "" {
		return hint
	}
	h := m.kube.helm(bin, t.namespace)
	h.Context = t.context // the scope the card showed
	return func() tea.Msg {
		var err error
		if t.action == "uninstall" {
			err = h.Uninstall(context.Background(), t.name)
		} else {
			err = h.Rollback(context.Background(), t.name, t.revision)
		}
		return kubeHelmWriteMsg{action: t.action, name: t.name, revision: t.revision, err: err}
	}
}

func (m *Model) onKubeHelmWrite(msg kubeHelmWriteMsg) tea.Cmd {
	if msg.action == "uninstall" {
		if msg.err != nil {
			return m.servicesToast(toast.Errr, "Cannot uninstall "+msg.name, msg.err.Error())
		}
		return tea.Batch(m.servicesToast(toast.Info, "Uninstalled "+msg.name, ""), m.kubeReload())
	}
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot roll back "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Rolled back "+msg.name, "To revision "+strconv.Itoa(msg.revision)), m.kubeReload())
}
