package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: back / forward and save screen (CTX-44) ─────────────
//
// Every context or namespace change is a place. [ goes back to the place
// before, ] forward again; a new change drops the forward list. w saves
// the rows on screen as plain text (0600, never over an existing file).

const promptKindKubeSaveScreen promptKindEnum = 3790

// kubeNavMax caps the back list.
const kubeNavMax = 50

type kubePlace struct{ context, namespace string }

func (st *kubeState) place() kubePlace { return kubePlace{st.context, st.namespace} }

// pushPlace remembers where the view is before it moves to a new place.
func (st *kubeState) pushPlace() {
	if st.context == "" && st.namespace == "" {
		return
	}
	st.navBack = append(st.navBack, st.place())
	if len(st.navBack) > kubeNavMax {
		st.navBack = st.navBack[len(st.navBack)-kubeNavMax:]
	}
	st.navFwd = nil
}

// kubeNav goes back (dir < 0) or forward (dir > 0) one place.
func (m *Model) kubeNav(dir int) tea.Cmd {
	st := m.kube
	from, to := &st.navBack, &st.navFwd
	what := "No earlier place"
	if dir > 0 {
		from, to = &st.navFwd, &st.navBack
		what = "No later place"
	}
	if len(*from) == 0 {
		return m.servicesToast(toast.Info, what, "c and n pick the context and the namespace")
	}
	p := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, st.place())
	return m.kubeGo(p.context, p.namespace)
}

// kubeScreenText is the view as plain text: the scope, then each section
// with its count and the rows it shows.
func kubeScreenText(st *kubeState) string {
	var b strings.Builder
	b.WriteString("Kubernetes  " + st.context + " / " + st.namespace + "\n")
	if st.filter != "" {
		b.WriteString("Filter: " + st.filter + "\n")
	}
	for _, r := range st.rows() {
		switch r.kind {
		case kubeRowSection:
			b.WriteString("\n" + kubeSectionTitle(r.section) + " (" + st.count(r.section) + ")\n")
		case kubeRowItem:
			b.WriteString("  " + r.name + "\n")
		default:
			b.WriteString("  " + r.text + "\n")
		}
	}
	return b.String()
}

// kubeAskSaveScreen asks where to save the view.
func (m *Model) kubeAskSaveScreen() tea.Cmd {
	st := m.kube
	ns := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(st.namespace)
	if ns == "" {
		ns = "default"
	}
	name := "kube-" + ns + "-" + time.Now().Format("20060102-150405") + ".txt"
	return m.openHTTPPrompt(promptKindKubeSaveScreen, "Save screen", "File:", filepath.Join(aiWorkspaceRoot(), name))
}

// kubeSaveScreen writes the view to path (relative to the workspace).
func (m *Model) kubeSaveScreen(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(aiWorkspaceRoot(), path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return m.servicesToast(toast.Warn, "That file already exists", "Pick another name.")
		}
		return m.servicesToast(toast.Errr, "Cannot save the screen", err.Error())
	}
	_, err = f.WriteString(kubeScreenText(m.kube))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return m.servicesToast(toast.Errr, "Cannot save the screen", err.Error())
	}
	return m.servicesToast(toast.Info, "Saved the screen", path)
}
