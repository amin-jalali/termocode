package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes: cluster report (CTX-27) ──────────────────────────────────
//
// "Kubernetes: Cluster Report" (x in the view) opens a read-only page of
// the namespace: Pulse (each kind: ok, warn, fail), Xray (the owner tree:
// deployment → pods, service → its pods, job → pods) and the sanitizer's
// findings. Read only; the AI may open it.

// kubePageMsg is a read-only page built from kubectl calls.
type kubePageMsg struct {
	what  string // "the cluster report", for the error toast
	title string
	text  string
	err   error
}

// kubePage runs build with a resolved client and opens its text as a
// read-only page titled "<title>: <ctx>/<ns>".
func (m *Model) kubePage(what, title string, build func(context.Context, kube.Client) (string, error)) tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		ctx := context.Background()
		c, err := kubeResolve(ctx, c)
		msg := kubePageMsg{what: what, title: title + ": " + c.Context + "/" + c.Namespace}
		if err != nil {
			msg.err = err
			return msg
		}
		msg.text, msg.err = build(ctx, c)
		return msg
	}
}

func (m *Model) onKubePage(msg kubePageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read "+msg.what, msg.err.Error())
	}
	if err := m.openReadOnlyPage(msg.title, msg.text, "", ""); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the page", err.Error())
	}
	return nil
}

// kubeReport opens the cluster report of the session's namespace.
func (m *Model) kubeReport() tea.Cmd {
	return m.kubePage("the cluster report", "report", func(ctx context.Context, c kube.Client) (string, error) {
		r, err := c.Report(ctx)
		if err != nil {
			return "", err
		}
		return r.Text(), nil
	})
}
