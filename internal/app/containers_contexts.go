package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── CONTEXTS: use for this session, make default ─────────────────────────
//
// Enter on a context uses it for this termocode session only: every
// engine call carries DOCKER_CONTEXT=<name>. Nothing is written to the
// user's shell or Docker config. "Make default…" is the only path that
// runs `docker context use`, and only after a card that shows it.

// hostExec is the docker runner for host actions: servicesExec plus the
// session's DOCKER_CONTEXT.
func (st *servicesState) hostExec() services.Exec {
	if st.sessionContext != "" && st.containerEngine.Kind != "podman" {
		return services.WithEnv(servicesExec, "DOCKER_CONTEXT="+st.sessionContext)
	}
	return servicesExec
}

// contextEnv is the extra environment of a visible container tab.
func (st *servicesState) contextEnv() map[string]any {
	if st.sessionContext == "" || st.containerEngine.Kind == "podman" {
		return nil
	}
	return map[string]any{"DOCKER_CONTEXT": st.sessionContext}
}

func (m *Model) selectedContext() (string, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostContext || r.hostID == "" {
		return "", false
	}
	return r.hostID, true
}

// switchContext uses a context for this session: the old host's rows and
// watcher go, the engine is detected again with DOCKER_CONTEXT set, and
// every open section lists again (the watcher restarts after detection).
func (m *Model) switchContext(name string) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || name == "" {
		return nil
	}
	known := false
	for _, c := range st.contextRows {
		if c.Name == name {
			known = true
		}
	}
	if !known {
		return m.servicesToast(toast.Info, "Unknown context", sanitizeOutputLine(name))
	}
	if name == st.activeContext() && st.containerEngine.Reachable {
		return m.servicesToast(toast.Info, "Already using "+sanitizeOutputLine(name), "")
	}
	st.cancelHostWork()
	st.stopHostWatch()
	st.sessionContext = name
	st.resetHostSnapshot(true)
	return tea.Batch(
		m.containersDetectWithWatch(true),
		m.servicesRefresh(),
		m.servicesToast(toast.Info, "Using context "+sanitizeOutputLine(name), "This session only · your default is unchanged"),
	)
}

// askContextDefault is "Make default…": a card with the exact command.
func (m *Model) askContextDefault(name string) tea.Cmd {
	st := m.services
	if st == nil || name == "" {
		return nil
	}
	args := []string{st.containerEngine.Binary, "context", "use", name}
	if st.containerEngine.Binary == "" {
		args[0] = "docker"
	}
	safe := sanitizeOutputLine(name)
	m.confirm = confirm.New("Make "+safe+" the default context?",
		"This changes the default for every terminal and app on this machine, not only termocode. "+
			"To try a context in this session only, press Enter on it instead.\n\nCommand: "+svcShellLine(args),
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "use", Title: "Make default", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "default-context", "contexts"
	m.pendingContextName = name
	return nil
}

// runContextDefault runs `docker context use <name>` (no session
// override on the call) after the card was confirmed.
func (m *Model) runContextDefault() tea.Cmd {
	name := m.pendingContextName
	m.pendingContextName = ""
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return hostActionDoneMsg{operation: "default-context", section: "contexts", target: name, err: e.UseContext(ctx, name)}
	}
}

func (m *Model) onContextDefaultDone(msg hostActionDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not change the default context", errLine(msg.err.Error()))
	}
	return tea.Batch(m.servicesToast(toast.Info, "Default context is now "+sanitizeOutputLine(msg.target), "For new terminals and other apps"), m.containersRefreshContexts())
}

// contextForPalette is the context the palette commands act on: the
// selected row, else the section opens with the cursor on it.
func (m *Model) contextForPalette() (string, tea.Cmd) {
	if name, ok := m.selectedContext(); ok {
		return name, nil
	}
	st := m.services
	if st.contextsHidden() {
		return "", m.servicesToast(toast.Info, "Only the default context exists", "Add one with docker context create")
	}
	st.collapsed[hostSection("contexts")] = false
	st.touch()
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContext && !r.current {
			st.cursor = i
			m.servicesClampCursor()
			break
		}
	}
	return "", m.servicesToast(toast.Info, "Pick a context", "Enter uses it here · D makes it the default")
}

func (m *Model) openContextMenu(r svcRow, x, y int) {
	m.services.menuRow = r
	items := []menu.Item{
		{ID: "use", Title: "Use in this session", Hint: "enter"},
		{ID: "default", Title: "Make default…", Hint: "D"},
		{Sep: true},
		{ID: "copy", Title: "Copy endpoint", Hint: "y"},
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindServices
}

func (m *Model) handleContextRowMenuSelect(id string, r svcRow) tea.Cmd {
	switch id {
	case "use":
		return m.switchContext(r.hostID)
	case "default":
		return m.askContextDefault(r.hostID)
	case "copy":
		return m.copyContextEndpoint(r.hostID)
	}
	return nil
}

func (m *Model) copyContextEndpoint(name string) tea.Cmd {
	for _, c := range m.services.contextRows {
		if c.Name == name && c.DockerEndpoint != "" {
			m.copyToClipboard(c.DockerEndpoint)
			return m.servicesToast(toast.Info, "Copied endpoint", sanitizeOutputLine(c.DockerEndpoint))
		}
	}
	return m.servicesToast(toast.Info, "No endpoint", sanitizeOutputLine(name))
}
