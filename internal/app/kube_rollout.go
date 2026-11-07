package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: rollout of a deployment (CTX-15) ────────────────────
//
// o on a deployment reads `kubectl rollout status` and `rollout history`
// (read only) and opens a picker: the first row opens a page with the
// status and every revision, the next rows are the revisions, newest
// first. Picking an older revision asks first (Cancel is the first
// button and has the focus), then runs `kubectl rollout undo
// --to-revision`. I asks for a new image (container=image), then a card
// shows the change before `kubectl set image` runs. The AI can not reach
// the writes (no palette id).

const (
	confirmKindKubeUndo  confirmKindEnum = 3475
	confirmKindKubeImage confirmKindEnum = 3476
	promptKindKubeImage  promptKindEnum  = 3477
)

// kubeUndoTarget is the deployment and revision an undo card asks about.
type kubeUndoTarget struct {
	context, namespace, name string
	revision                 int
}

// kubeImageTarget is the deployment and change an image card asks about.
type kubeImageTarget struct {
	context, namespace, name string
	container, from, to      string
}

type kubeRolloutMsg struct {
	name   string
	status string
	revs   []kube.RolloutRevision
	err    error
}

type kubeUndoMsg struct {
	name     string
	revision int
	err      error
}

type kubeImageMsg struct {
	name, container, image string
	err                    error
}

// kubeRollout reads the status and the history of one deployment.
func (m *Model) kubeRollout(name string) tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		ctx := context.Background()
		msg := kubeRolloutMsg{name: name}
		msg.revs, msg.err = c.RolloutHistory(ctx, name)
		if msg.err != nil {
			return msg
		}
		status, err := c.RolloutStatus(ctx, name)
		if err != nil {
			status = "status: " + err.Error()
		}
		msg.status = status
		return msg
	}
}

func (m *Model) onKubeRollout(msg kubeRolloutMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read the rollout of "+msg.name, msg.err.Error())
	}
	m.kube.lastRollout = msg
	m.openTasksPickerItems(" Rollout: "+msg.name+" ", kubeRolloutItems(msg))
	return nil
}

// kubeRolloutItems are the rollout picker's rows: the page, then the
// revisions newest first (the newest is the current one).
func kubeRolloutItems(msg kubeRolloutMsg) []picker.Item {
	status := msg.status
	if status == "" {
		status = "unknown"
	}
	items := []picker.Item{{ID: "k8s:rollout-page:" + msg.name, Title: "Status and history", Hint: kubeFirstLine(status)}}
	newest := 0
	for _, r := range msg.revs {
		newest = max(newest, r.Number)
	}
	for i := len(msg.revs) - 1; i >= 0; i-- {
		r := msg.revs[i]
		title := "Revision " + strconv.Itoa(r.Number)
		if r.Cause != "" {
			title += " · " + r.Cause
		}
		if r.Number == newest {
			items = append(items, picker.Item{ID: "k8s:rollout-current:" + msg.name, Title: title, Hint: "current"})
			continue
		}
		items = append(items, picker.Item{ID: "k8s:undo:" + msg.name + ":" + strconv.Itoa(r.Number), Title: title, Hint: "roll back (asks first)"})
	}
	return items
}

func kubeFirstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// kubeRolloutText is the read-only status and history page.
func kubeRolloutText(msg kubeRolloutMsg) string {
	var b strings.Builder
	b.WriteString("Deployment " + msg.name + "\n\nSTATUS\n  " + strings.ReplaceAll(strings.TrimSpace(msg.status), "\n", "\n  ") + "\n\nHISTORY\n")
	if len(msg.revs) == 0 {
		b.WriteString("  No revisions\n")
	}
	for i := len(msg.revs) - 1; i >= 0; i-- {
		r := msg.revs[i]
		cause := r.Cause
		if cause == "" {
			cause = "(no change cause)"
		}
		b.WriteString("  " + strconv.Itoa(r.Number) + "  " + cause + "\n")
	}
	b.WriteString("\no on the deployment rolls back to an older revision (asks first).\n")
	return b.String()
}

// handleKubeRolloutPick runs a rollout picker row. ok=false: not ours.
func (m *Model) handleKubeRolloutPick(kind, value string) (tea.Cmd, bool) {
	switch kind {
	case "rollout-page":
		msg := m.kube.lastRollout
		if msg.name != value {
			return nil, true
		}
		if err := m.openReadOnlyPage("rollout: deployment/"+value, kubeRolloutText(msg), "", ""); err != nil {
			return m.servicesToast(toast.Errr, "Cannot open the rollout page", err.Error()), true
		}
		return nil, true
	case "rollout-current":
		return m.servicesToast(toast.Info, value+" runs this revision now", "Pick an older one to roll back"), true
	case "undo":
		i := strings.LastIndex(value, ":")
		if i < 0 {
			return nil, true
		}
		rev, err := strconv.Atoi(value[i+1:])
		if err != nil || rev <= 0 {
			return nil, true
		}
		m.askKubeUndo(value[:i], rev)
		return nil, true
	}
	return nil, false
}

// askKubeUndo opens the undo card.
func (m *Model) askKubeUndo(name string, rev int) {
	st := m.kube
	st.pendingUndo = kubeUndoTarget{context: st.context, namespace: st.namespace, name: name, revision: rev}
	body := "This rolls " + name + " back to revision " + strconv.Itoa(rev) + ".\n" +
		"Its pods are replaced with the pods of that revision.\n\n" +
		"Context: " + st.context + " · Namespace: " + st.namespace + "\n" +
		"Command: kubectl rollout undo deployment/" + name + " --to-revision=" + strconv.Itoa(rev)
	m.confirm = confirm.Risky("Roll back deployment?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "undo", Title: "Roll Back", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeUndo
}

func (m *Model) answerKubeUndo(id string) tea.Cmd {
	t := m.kube.pendingUndo
	m.kube.pendingUndo = kubeUndoTarget{}
	if id != "undo" || t.name == "" {
		return nil
	}
	c := m.kube.client()
	c.Context, c.Namespace = t.context, t.namespace // the scope the card showed
	return func() tea.Msg {
		return kubeUndoMsg{name: t.name, revision: t.revision, err: c.RolloutUndo(context.Background(), t.name, t.revision)}
	}
}

func (m *Model) onKubeUndo(msg kubeUndoMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot roll back "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Rolling back "+msg.name, "To revision "+strconv.Itoa(msg.revision)), m.kubeReload())
}

// askKubeImage opens the image prompt: "container=image", starting at
// the first container's image.
func (m *Model) askKubeImage(d kube.Deployment, value, errText string) {
	st := m.kube
	st.pendingImage = kubeImageTarget{context: st.context, namespace: st.namespace, name: d.Name}
	st.imageOf = d
	if value == "" && errText == "" && len(d.Images) > 0 {
		value = d.Images[0].Name + "=" + d.Images[0].Image
	}
	var names []string
	for _, c := range d.Images {
		names = append(names, c.Name)
	}
	label := "New image for " + d.Name + " (container=image):"
	m.prompt = prompt.New(" Set image ", label, value)
	note := "A card asks before anything changes."
	if len(names) > 0 {
		note = "Containers: " + strings.Join(names, ", ") + ". " + note
	}
	m.prompt.SetNote(note)
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindKubeImage
}

// kubeParseImageAnswer reads "container=image"; with one container the
// name may be left out.
func kubeParseImageAnswer(d kube.Deployment, text string) (container, image string, err error) {
	text = strings.TrimSpace(text)
	name, img, ok := strings.Cut(text, "=")
	if !ok {
		if len(d.Images) != 1 {
			return "", "", errors.New("type container=image, for example " + kubeImageExample(d))
		}
		name, img = d.Images[0].Name, text
	}
	name, img = strings.TrimSpace(name), strings.TrimSpace(img)
	if img == "" || strings.ContainsAny(img, " \t") || strings.HasPrefix(img, "-") {
		return "", "", errors.New("not a valid image: " + img)
	}
	if len(d.Images) > 0 {
		found := false
		for _, c := range d.Images {
			found = found || c.Name == name
		}
		if !found {
			return "", "", errors.New("no container " + name + " in " + d.Name)
		}
	}
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", "", errors.New("type container=image, for example " + kubeImageExample(d))
	}
	return name, img, nil
}

func kubeImageExample(d kube.Deployment) string {
	if len(d.Images) > 0 {
		return d.Images[0].Name + "=" + d.Images[0].Image
	}
	return "app=repo/app:v2"
}

// handleKubeImageSubmit takes the image answer; ok=false: not ours.
func (m *Model) handleKubeImageSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindKubeImage || m.kube == nil {
		return nil, false
	}
	st := m.kube
	t := st.pendingImage
	if t.name == "" {
		return nil, true
	}
	d := st.imageOf
	container, image, err := kubeParseImageAnswer(d, value)
	if err != nil {
		e := err.Error()
		m.askKubeImage(d, strings.TrimSpace(value), strings.ToUpper(e[:1])+e[1:]+".")
		st.pendingImage.context, st.pendingImage.namespace = t.context, t.namespace
		return nil, true
	}
	for _, c := range d.Images {
		if c.Name == container {
			t.from = c.Image
		}
	}
	if t.from == image {
		st.pendingImage = kubeImageTarget{}
		return m.servicesToast(toast.Info, container+" already runs "+image, "Nothing changed"), true
	}
	t.container, t.to = container, image
	st.pendingImage = t
	from := t.from
	if from == "" {
		from = "?"
	}
	body := "This changes the image of " + container + " in " + t.name + ":\n" + from + " → " + image + "\n" +
		"Its pods are replaced one by one.\n\n" +
		"Context: " + t.context + " · Namespace: " + t.namespace + "\n" +
		"Command: kubectl set image deployment/" + t.name + " " + container + "=" + image
	m.confirm = confirm.Risky("Set image?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "set", Title: "Set Image", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeImage
	return nil, true
}

func (m *Model) answerKubeImage(id string) tea.Cmd {
	t := m.kube.pendingImage
	m.kube.pendingImage = kubeImageTarget{}
	if id != "set" || t.name == "" {
		return nil
	}
	c := m.kube.client()
	c.Context, c.Namespace = t.context, t.namespace
	return func() tea.Msg {
		return kubeImageMsg{name: t.name, container: t.container, image: t.to, err: c.SetImage(context.Background(), t.name, t.container, t.to)}
	}
}

func (m *Model) onKubeImage(msg kubeImageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot set the image of "+msg.name, msg.err.Error())
	}
	return tea.Batch(m.servicesToast(toast.Info, "Updating "+msg.name, msg.container+" → "+msg.image), m.kubeReload())
}
