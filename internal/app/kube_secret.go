package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: show one Secret value (CTX-40) ──────────────────────
//
// The view lists key names only. V on a secret picks a key (when there are
// more), asks first on a Cancel-first card, then reads that one value and
// shows it on a card. The value is not cached, not logged and never given
// to the AI; it is wiped when the card closes.

const (
	// confirmKindKubeReveal is the "show the value?" card.
	confirmKindKubeReveal confirmKindEnum = 7741
	// confirmKindKubeSecretValue is the card that shows the value.
	confirmKindKubeSecretValue confirmKindEnum = 7742
)

type kubeRevealTarget struct {
	context, namespace, name, key string
}

type kubeSecretValueMsg struct {
	name, key, value string
	err              error
}

// kubeRevealKey is V on a secret row.
func (m *Model) kubeRevealKey(r kubeRow) tea.Cmd {
	st := m.kube
	o, ok := st.extraObj(r)
	if !ok {
		return nil
	}
	switch len(o.Keys) {
	case 0:
		return m.servicesToast(toast.Info, "Secret "+o.Name+" has no keys", "")
	case 1:
		m.askKubeReveal(o.Name, o.Keys[0])
		return nil
	}
	st.revealSecret = o.Name
	items := make([]picker.Item, 0, len(o.Keys))
	for _, k := range o.Keys {
		items = append(items, picker.Item{ID: "k8s:secretkey:" + k, Title: k, Hint: "show the value (asks first)"})
	}
	m.openTasksPickerItems(" Secret "+o.Name+": key ", items)
	return nil
}

// askKubeReveal opens the Cancel-first card for one key.
func (m *Model) askKubeReveal(name, key string) {
	st := m.kube
	if st == nil || name == "" || key == "" {
		return
	}
	st.revealSecret = ""
	st.pendingReveal = kubeRevealTarget{context: st.context, namespace: st.namespace, name: name, key: key}
	body := "This shows the value of key " + key + " in secret " + name + " on screen, in plain text.\n" +
		"It is not saved, not logged and never sent to the AI. Closing the card forgets it.\n\n" +
		"Context: " + st.context + " · Namespace: " + st.namespace + "\n" +
		"Command: " + kube.SecretValueCommand(name, key)
	m.confirm = confirm.Risky("Show secret value?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "reveal", Title: "Show value", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeReveal
}

// answerKubeReveal runs the answer of both cards.
func (m *Model) answerKubeReveal(id string) tea.Cmd {
	st := m.kube
	if m.confirmKind == confirmKindKubeSecretValue {
		v := st.revealedValue
		st.revealedValue = ""
		if id == "copy" && v != "" {
			m.copyToClipboard(v)
			return m.servicesToast(toast.Info, "Copied the secret value", "")
		}
		return nil
	}
	target := st.pendingReveal
	st.pendingReveal = kubeRevealTarget{}
	if id != "reveal" || target.name == "" {
		return nil
	}
	c := st.client()
	c.Context, c.Namespace = target.context, target.namespace // the scope the card showed
	return func() tea.Msg {
		v, err := c.SecretValue(context.Background(), target.name, target.key)
		return kubeSecretValueMsg{name: target.name, key: target.key, value: v, err: err}
	}
}

func (m *Model) onKubeSecretValue(msg kubeSecretValueMsg) tea.Cmd {
	if m.kube == nil {
		return nil
	}
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read "+msg.name+"/"+msg.key, msg.err.Error())
	}
	m.kube.revealedValue = msg.value
	shown := msg.value
	if shown == "" {
		shown = "(empty)"
	}
	lines := strings.Split(shown, "\n")
	for i, l := range lines {
		lines[i] = sanitizeOutputLine(l)
	}
	m.confirm = confirm.Risky("Secret "+msg.name+" · "+msg.key, strings.Join(lines, "\n"),
		confirm.Button{ID: "close", Title: "Close"}, confirm.Button{ID: "copy", Title: "Copy"}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeSecretValue
	return nil
}
