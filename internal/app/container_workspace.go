package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Plan RC, phase 3: open a folder inside a running container from the
// UI (Containers row `W`, the palette, recents) and watch it while it is
// open (the status pill, the offline card). termocode stays local; the
// open itself is a re-exec with `--container`, like Open Recent
// Workspace re-execs into a local folder.

const (
	promptKindContainerFolder   promptKindEnum  = 5417 // "Folder in <container>" (row w)
	confirmKindContainerOffline confirmKindEnum = 5418 // the container stopped
)

// containerWatchEvery is how often an open container workspace is asked
// whether it still runs (one inspect call).
var containerWatchEvery = 5 * time.Second

// containerRef names a folder in a container: what --container takes.
type containerRef struct {
	Context string // "" = the engine's default context
	Name    string
	Root    string
}

// id is the workspace ID of r, the one containerhost.Host.ID gives:
// "container:<context>/<name>:<root>".
func (r containerRef) id() string {
	c := r.Context
	if c == "" {
		c = "default"
	}
	return string(workspace.KindContainer) + ":" + c + "/" + r.Name + ":" + r.Root
}

// args are the termocode arguments that open r.
func (r containerRef) args() []string {
	a := []string{"--container", r.Name}
	if r.Context != "" {
		a = append(a, "--context", r.Context)
	}
	return append(a, r.Root)
}

// title is the one-line name of r for lists: "▣ api-1 · prod-ssh /app"
// ("default" context left out).
func (r containerRef) title() string {
	s := "▣ " + r.Name
	if r.Context != "" {
		s += " · " + r.Context
	}
	return s + " " + r.Root
}

// parseContainerRef reads a container workspace ID; ok is false for
// anything else (a local path).
func parseContainerRef(id string) (containerRef, bool) {
	kind, rest := workspace.ParseID(id)
	if kind != workspace.KindContainer {
		return containerRef{}, false
	}
	ctxName, root, ok := strings.Cut(rest, ":")
	if !ok || !path.IsAbs(root) {
		return containerRef{}, false
	}
	c, name, ok := strings.Cut(ctxName, "/")
	if !ok || name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(c, "-") {
		return containerRef{}, false
	}
	if c == "default" {
		c = ""
	}
	return containerRef{Context: c, Name: name, Root: path.Clean(root)}, true
}

// PushContainerWorkspace records a container folder as the most recent
// workspace (cmd/termocode calls it after --container opened).
func PushContainerWorkspace(engineContext, name, root string) {
	pushWorkspaceID(containerRef{Context: engineContext, Name: name, Root: root}.id())
}

// pushWorkspaceID puts id first in the recent list (no path cleanup:
// a container ID is not a local path).
func pushWorkspaceID(id string) {
	entries := loadWorkspaces()
	out := make([]WorkspaceEntry, 0, len(entries)+1)
	out = append(out, WorkspaceEntry{Path: id, OpenedAt: time.Now()})
	for _, e := range entries {
		if e.Path == id {
			continue
		}
		out = append(out, e)
		if len(out) >= workspacesCap {
			break
		}
	}
	saveWorkspaces(out)
}

// workspaceEntryTitle is how a recent entry reads: the container title
// for a container folder, the path itself for a local one.
func workspaceEntryTitle(p string) string {
	if r, ok := parseContainerRef(p); ok {
		return r.title()
	}
	return p
}

// reexecTermocode replaces this process with termocode started with
// args (tests swap it).
var reexecTermocode = func(args []string) error {
	bin, err := spawn.Executable()
	if err != nil {
		bin = os.Args[0]
	}
	return spawn.Exec(bin, append([]string{filepath.Base(bin)}, args...), os.Environ())
}

// openContainerWorkspace leaves this workspace and opens r.
func (m *Model) openContainerWorkspace(r containerRef) tea.Cmd {
	if !canReexec() {
		return m.pushToastDetail(toast.Errr, "Exec failed", errHeadless.Error())
	}
	m.leaveWorkspace()
	if err := reexecTermocode(r.args()); err != nil {
		return m.pushToastDetail(toast.Errr, "Exec failed", err.Error())
	}
	return nil
}

// canReexec is false without a terminal (tests swap it).
var canReexec = func() bool { return !headless() }

// containerName is the engine name of c (no leading slash), or its ID.
func containerName(c containers.Container) string {
	for _, n := range c.Names {
		if n = strings.TrimPrefix(n, "/"); n != "" {
			return n
		}
	}
	return c.ID
}

// askOpenFolderInContainer is Containers row `W`: which folder of the
// running container c to open, prefilled with its working folder.
func (m *Model) askOpenFolderInContainer(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") {
		return m.servicesToast(toast.Info, "Start the container first", containerShownName(c)+" is not running.")
	}
	dir := c.WorkingDir
	if !path.IsAbs(dir) {
		dir = "/"
	}
	m.pendingContainerOpen = containerRef{Context: m.services.containerEngine.Context, Name: containerName(c)}
	m.prompt = prompt.New("Open folder in container", "Folder in "+containerShownName(c)+":", dir)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerFolder
	return nil
}

// onContainerFolderPrompt opens the typed folder in the chosen container.
func (m *Model) onContainerFolderPrompt(value string) tea.Cmd {
	r := m.pendingContainerOpen
	m.pendingContainerOpen = containerRef{}
	value = strings.TrimSpace(value)
	if r.Name == "" || value == "" {
		return nil
	}
	if !path.IsAbs(value) {
		return m.servicesToast(toast.Info, "Give the full folder path", "Like /app: the folder inside the container.")
	}
	r.Root = path.Clean(value)
	return m.openContainerWorkspace(r)
}

// openFolderInContainerPalette is "Workspace: Open Folder in
// Container…": the selected container row when there is one, else the
// Containers view with a hint.
func (m *Model) openFolderInContainerPalette() tea.Cmd {
	if c, ok := m.selectedContainer(); ok {
		return m.askOpenFolderInContainer(c)
	}
	return tea.Batch(m.revealServicesView(true),
		m.servicesToast(toast.Info, "Pick a running container", "Select it in Containers, then press W."))
}

// ─── watching an open container workspace ─────────────────────────────

// containerOnlineHost is what the watch needs from a container host
// (containerhost.Host).
type containerOnlineHost interface {
	CheckOnline(ctx context.Context) bool
	Start(ctx context.Context) error
	Label() string
}

type containerWatchTickMsg struct{}

// containerOnlineMsg is one answer of the watch.
type containerOnlineMsg struct{ online bool }

// containerStartDoneMsg is the end of Start on the offline card.
type containerStartDoneMsg struct{ err error }

func (m Model) containerHost() (containerOnlineHost, bool) {
	ws := m.workspace()
	if ws.Kind != workspace.KindContainer || ws.Host == nil {
		return nil, false
	}
	h, ok := ws.Host.(containerOnlineHost)
	return h, ok
}

// containerWatchInitCmd starts the watch for a container workspace (nil
// for a local one).
func (m Model) containerWatchInitCmd() tea.Cmd {
	if _, ok := m.containerHost(); !ok {
		return nil
	}
	return tea.Tick(containerWatchEvery, func(time.Time) tea.Msg { return containerWatchTickMsg{} })
}

func (m Model) containerCheckCmd() tea.Cmd {
	h, ok := m.containerHost()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		return containerOnlineMsg{online: h.CheckOnline(context.Background())}
	}
}

// handleContainerWSMsg routes the watch messages. ok=false: not ours.
func (m *Model) handleContainerWSMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case containerWatchTickMsg:
		return m.containerCheckCmd(), true
	case containerOnlineMsg:
		return m.onContainerOnline(msg.online), true
	case containerStartDoneMsg:
		m.containerStarting = false
		if msg.err != nil {
			m.containerOffline = true
			return tea.Batch(m.pushToastDetail(toast.Errr, "Could not start the container", msg.err.Error()),
				m.containerWatchInitCmd()), true
		}
		m.containerOffline = false
		return tea.Batch(m.pushToastDetail(toast.Info, "Container running again", m.workspaceHostLabel()),
			m.containerWatchInitCmd()), true
	}
	return nil, false
}

// onContainerOnline takes one watch answer: going offline opens the
// card once; the watch goes on either way.
func (m *Model) onContainerOnline(online bool) tea.Cmd {
	next := m.containerWatchInitCmd()
	was := m.containerOffline
	m.containerOffline = !online
	if online || was || m.containerStarting {
		if online && was && m.confirmOpen && m.confirmKind == confirmKindContainerOffline {
			m.confirmOpen = false // it came back by itself
		}
		return next
	}
	if !m.confirmOpen {
		m.openContainerOfflineCard()
	}
	return next
}

// openContainerOfflineCard says the container stopped. Wait (first,
// focused) changes nothing: the open buffers stay and the watch goes on.
func (m *Model) openContainerOfflineCard() {
	label := m.workspaceHostLabel()
	m.confirm = confirm.New(label+" stopped",
		"The container of this workspace is not running. Your open files stay; saving waits until it runs again.",
		[]confirm.Button{
			{ID: "wait", Title: "Wait"},
			{ID: "start", Title: "Start"},
			{ID: "disconnect", Title: "Disconnect"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerOffline
}

// onContainerOfflineConfirm handles Wait / Start / Disconnect.
func (m *Model) onContainerOfflineConfirm(id string) tea.Cmd {
	switch id {
	case "start":
		h, ok := m.containerHost()
		if !ok {
			return nil
		}
		m.containerStarting = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return containerStartDoneMsg{err: h.Start(ctx)}
		}
	case "disconnect":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/"
		}
		if !canReexec() {
			return m.pushToastDetail(toast.Errr, "Exec failed", errHeadless.Error())
		}
		m.leaveWorkspace()
		if err := reexecTermocode([]string{home}); err != nil {
			return m.pushToastDetail(toast.Errr, "Exec failed", err.Error())
		}
	}
	return nil
}

// workspaceHostLabel is the host's short name ("api-1"); "" for a local
// workspace.
func (m Model) workspaceHostLabel() string {
	ws := m.workspace()
	if ws.Kind != workspace.KindContainer || ws.Host == nil {
		return ""
	}
	return ws.Host.Label()
}
