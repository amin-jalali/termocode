package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes: edit YAML, apply a file (CTX-21) ─────────────────────────
//
// "Kubernetes: Apply Current File…" (and a in the view) applies the saved
// file of the editor tab: first `kubectl diff -f` (a server-side dry-run,
// nothing changes) opens as a read-only diff page, then a card asks.
// Cancel is the first button and has the focus. Apply runs `kubectl
// apply -f` in the context and namespace the card showed. The AI can not
// reach it (policy never).
//
// o on a row writes the object's YAML to a private temp file and opens it
// for editing; save it, then apply it the same way. A Secret is refused:
// its values are masked, so applying the copy would break it.

// confirmKindKubeApply is the apply card (kube_apply.go).
const confirmKindKubeApply confirmKindEnum = 3490

// kubeApplyTarget is the file an apply card asks about, in its scope.
type kubeApplyTarget struct {
	context, namespace, path string
}

type kubeDiffMsg struct {
	target kubeApplyTarget
	diff   kube.FileDiff
	err    error
}

type kubeApplyMsg struct {
	path, out string
	err       error
}

// errNoKubectl is the toast text when kubectl is missing.
var errNoKubectl = errors.New("Install kubectl, then try again")

// kubeResolve fills what a call needs that the view may not know yet:
// kubectl's path, the current context and its namespace.
func kubeResolve(ctx context.Context, c kube.Client) (kube.Client, error) {
	if c.Binary == "" {
		c.Binary = kube.Find(nil)
	}
	if c.Binary == "" {
		return c, errNoKubectl
	}
	if c.Context == "" {
		cur, err := c.CurrentContext(ctx)
		if err != nil {
			return c, err
		}
		c.Context = cur
	}
	if c.Namespace == "" {
		ns, err := c.ContextNamespace(ctx)
		if err != nil {
			return c, err
		}
		c.Namespace = ns
	}
	return c, nil
}

// kubeApplyCurrentFile starts the apply of the editor tab's file: it must
// have a path and no unsaved changes (the diff reads the file on disk).
func (m *Model) kubeApplyCurrentFile() tea.Cmd {
	buf, ok := m.liveBuffer(m.currentBufferID())
	if !ok || buf.Path == "" {
		return m.servicesToast(toast.Info, "No file to apply", "Open a saved YAML file in the editor first")
	}
	if buf.Modified {
		return m.servicesToast(toast.Info, "Save the file first", filepath.Base(buf.Path)+" has unsaved changes")
	}
	return m.kubeDiffFile(buf.Path)
}

// kubeDiffFile runs the dry-run diff of path.
func (m *Model) kubeDiffFile(path string) tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		c, err := kubeResolve(context.Background(), c)
		t := kubeApplyTarget{context: c.Context, namespace: c.Namespace, path: path}
		if err != nil {
			return kubeDiffMsg{target: t, err: err}
		}
		d, err := c.DiffFile(context.Background(), path)
		return kubeDiffMsg{target: t, diff: d, err: err}
	}
}

// kubeDiffStat counts the changed lines of a unified diff.
func kubeDiffStat(text string) (add, del int) {
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return add, del
}

func (m *Model) onKubeDiff(msg kubeDiffMsg) tea.Cmd {
	name := filepath.Base(msg.target.path)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot diff "+name, msg.err.Error())
	}
	if !msg.diff.Changed {
		return m.servicesToast(toast.Info, "Nothing to apply", "The cluster already matches "+name)
	}
	if err := m.openReadOnlyPage("diff: "+name, msg.diff.Text, "", "diff"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the diff page", err.Error())
	}
	m.askKubeApply(msg.target, msg.diff.Text)
	return nil
}

// askKubeApply opens the apply card.
func (m *Model) askKubeApply(t kubeApplyTarget, diff string) {
	m.kube.pendingApply = t
	add, del := kubeDiffStat(diff)
	body := "This changes the cluster as the diff page shows (+" + itoa(add) + " −" + itoa(del) + " lines).\n\n" +
		"File: " + t.path + "\n" +
		"Context: " + t.context + " · Namespace: " + t.namespace + "\n" +
		"Command: kubectl apply -f " + filepath.Base(t.path)
	m.confirm = confirm.Risky("Apply file to the cluster?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "apply", Title: "Apply", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeApply
}

// answerKubeApply runs the apply card's answer.
func (m *Model) answerKubeApply(id string) tea.Cmd {
	t := m.kube.pendingApply
	m.kube.pendingApply = kubeApplyTarget{}
	if id != "apply" || t.path == "" {
		return nil
	}
	c := m.kube.client()
	c.Context, c.Namespace = t.context, t.namespace // the scope the card showed
	return func() tea.Msg {
		c, err := kubeResolve(context.Background(), c)
		if err != nil {
			return kubeApplyMsg{path: t.path, err: err}
		}
		out, err := c.ApplyFile(context.Background(), t.path)
		return kubeApplyMsg{path: t.path, out: out, err: err}
	}
}

func (m *Model) onKubeApply(msg kubeApplyMsg) tea.Cmd {
	name := filepath.Base(msg.path)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot apply "+name, msg.err.Error())
	}
	detail := strings.TrimSpace(msg.out)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = detail[:i] + " …"
	}
	return tea.Batch(m.servicesToast(toast.Info, "Applied "+name, detail), m.kubeReload())
}

type kubeEditMsg struct {
	kind, name, path string
	err              error
}

// kubeEditPath is where o writes an object's YAML: a private temp dir per
// context and namespace.
func kubeEditPath(context, namespace, kind, name string) string {
	clean := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == 0 {
				return '_'
			}
			return r
		}, s)
		if s == "" || s == "." || s == ".." {
			s = "_"
		}
		return s
	}
	return filepath.Join(os.TempDir(), "termocode-kube", clean(context), clean(namespace), clean(kind)+"-"+clean(name)+".yaml")
}

// kubeEditYAML reads the object's YAML into a file and opens it.
func (m *Model) kubeEditYAML(kind, name string) tea.Cmd {
	if kind == "secret" || kind == "secrets" {
		return m.servicesToast(toast.Info, "Secrets are not edited here", "Their values are masked; edit the manifest file instead")
	}
	c := m.kube.client()
	path := kubeEditPath(c.Context, c.Namespace, kind, name)
	return func() tea.Msg {
		text, err := c.YAML(context.Background(), kind, name)
		if err == nil && kube.MaskSecretYAML("", text) != text {
			err = errors.New("it is a Secret; its values are masked")
		}
		if err == nil {
			err = os.MkdirAll(filepath.Dir(path), 0o700)
		}
		if err == nil {
			err = os.WriteFile(path, []byte(text), 0o600)
		}
		return kubeEditMsg{kind: kind, name: name, path: path, err: err}
	}
}

func (m *Model) onKubeEdit(msg kubeEditMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot edit "+msg.kind+"/"+msg.name, msg.err.Error())
	}
	m.openFileAtLine(msg.path, 1)
	return m.servicesToast(toast.Info, "Editing "+msg.kind+"/"+msg.name, "Save, then Kubernetes: Apply Current File… (a in the view)")
}
