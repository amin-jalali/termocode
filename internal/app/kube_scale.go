package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: scale a deployment (CTX-5) ──────────────────────────
//
// s on a deployment asks the number of replicas (it starts at the current
// number), then a card shows the change, the scope and the command
// (Cancel is the first button and has the focus; Esc cancels). Only then
// it runs `kubectl scale`. The AI can not reach it (no palette id).

const (
	confirmKindKubeScale confirmKindEnum = 3473
	promptKindKubeScale  promptKindEnum  = 3474
)

// kubeScaleTarget is the deployment a scale prompt and card ask about,
// in the scope they were asked in.
type kubeScaleTarget struct {
	context, namespace, name string
	from, to                 int
}

type kubeScaleMsg struct {
	name     string
	replicas int
	err      error
}

// askKubeScale opens the replicas prompt (value: the text to show, ""
// = the current number; errText: why the last answer was refused).
func (m *Model) askKubeScale(d kube.Deployment, value, errText string) {
	st := m.kube
	st.pendingScale = kubeScaleTarget{context: st.context, namespace: st.namespace, name: d.Name, from: d.Replicas}
	if value == "" && errText == "" {
		value = strconv.Itoa(d.Replicas)
	}
	m.prompt = prompt.New(" Scale deployment ", "Replicas for "+d.Name+" (now "+strconv.Itoa(d.Replicas)+"):", value)
	m.prompt.SetNote("0 stops every pod. A card asks before anything changes.")
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindKubeScale
}

// handleKubePromptSubmit takes the replicas answer. ok=false: not ours.
func (m *Model) handleKubePromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindKubeScale || m.kube == nil {
		return nil, false
	}
	t := m.kube.pendingScale
	if t.name == "" {
		return nil, true
	}
	text := strings.TrimSpace(value)
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 || n > kube.ScaleMax {
		m.askKubeScale(kube.Deployment{Name: t.name, Replicas: t.from}, text, "Type a whole number from 0 to "+strconv.Itoa(kube.ScaleMax)+".")
		m.kube.pendingScale.context, m.kube.pendingScale.namespace = t.context, t.namespace
		return nil, true
	}
	if n == t.from {
		m.kube.pendingScale = kubeScaleTarget{}
		return m.servicesToast(toast.Info, t.name+" already has "+strconv.Itoa(n)+" replicas", "Nothing changed"), true
	}
	t.to = n
	m.kube.pendingScale = t
	what := "This changes " + t.name + " from " + strconv.Itoa(t.from) + " → " + strconv.Itoa(n) + " replicas."
	if n == 0 {
		what += "\nIt stops every pod of " + t.name + "."
	}
	body := what + "\n\n" +
		"Context: " + t.context + " · Namespace: " + t.namespace + "\n" +
		"Command: kubectl scale deployment/" + t.name + " --replicas=" + strconv.Itoa(n)
	m.confirm = confirm.Risky("Scale deployment?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "scale", Title: "Scale", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeScale
	return nil, true
}

// answerKubeScale runs the scale card's answer.
func (m *Model) answerKubeScale(id string) tea.Cmd {
	t := m.kube.pendingScale
	m.kube.pendingScale = kubeScaleTarget{}
	if id != "scale" || t.name == "" {
		return nil
	}
	c := m.kube.client()
	c.Context, c.Namespace = t.context, t.namespace // the scope the card showed
	return func() tea.Msg {
		return kubeScaleMsg{name: t.name, replicas: t.to, err: c.Scale(context.Background(), "deployment", t.name, t.to)}
	}
}

func (m *Model) onKubeScale(msg kubeScaleMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot scale "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Scaling "+msg.name, "To "+strconv.Itoa(msg.replicas)+" replicas"), m.kubeReload())
}
