package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Opening and closing a project from the phone (MOB-51, mobile plan §3.7).
//
// The home side (the door's own workspace): project.open names a known
// project by its id (the workspace key) and, after a laptop card (Cancel
// first, naming the folder and the device), starts that folder's session
// headless — the UI with no window — and waits for its web hub, which the
// door gateway then joins (MOB-50). At most web_projects_max projects the
// door started run at once. A project that runs already needs no card.
//
// The project side: a UI the door started (EnvDoorIdle in its start env)
// stops by itself after web_project_idle_minutes with no use, and
// project.close stops it at once; never while an agent works or waits, a
// workflow run goes or a card waits, never once someone attached to it on
// the laptop, and never for a service-installed folder (the door does not
// pass the idle minutes then). A project opened on the laptop is closed on
// the laptop only.
//
// The AI can never open or close a project: there is no AI tool or
// palette command for it; only a browser's op reaches these handlers.

// ProjectSessions are cmd/termocode's hooks for other projects' sessions
// (a session server is started from there). Nil fields: not on this
// system; project.open then says so.
type ProjectSessions struct {
	// Running: dir's session runs with its UI and its web hub.
	Running func(dir string) bool
	// Start starts dir's session headless with the door's idle minutes
	// (0: no idle stop) and waits for its web hub.
	Start func(dir string, idleMinutes int) error
	// Service: termocode's own service file runs dir's session.
	Service func(dir string) bool
}

var projectSessions ProjectSessions

// SetProjectSessions sets the hooks (cmd/termocode, at start).
func SetProjectSessions(p ProjectSessions) { projectSessions = p }

// webDoorHubWait bounds the wait for a started project's web hub.
const webDoorHubWait = 45 * time.Second

// webDoorState is the door's own projects (Update only): the ones being
// opened and the ones it started, by id.
type webDoorState struct {
	opening map[string]string // id → folder
	opened  map[string]string // id → folder, started by this door
}

var webDoor = webDoorState{opening: map[string]string{}, opened: map[string]string{}}

// webDoorProject is one project the laptop knows.
type webDoorProject struct {
	ID  string // session.Key of the folder
	Dir string
}

// webDoorHome is the id of this workspace.
func webDoorHome() string { return session.Key(aiWorkspaceRoot()) }

// webDoorKnown are the projects the laptop knows other than this one,
// from the same files as the projects topic (MOB-8: recent workspaces,
// sessions, services, the other windows' agent roll-ups), each folder
// once. Folders are added on the laptop only.
func webDoorKnown() []webDoorProject {
	in := webProjectsRead()
	home := webDoorHome()
	seen := map[string]bool{home: true}
	var out []webDoorProject
	add := func(dir string) {
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		dir = filepath.Clean(dir)
		id := session.Key(dir)
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, webDoorProject{ID: id, Dir: dir})
	}
	for _, e := range in.recent {
		add(e.Path)
	}
	for _, si := range in.sessions {
		add(si.Workspace)
	}
	for _, ws := range in.services {
		add(ws)
	}
	for _, r := range in.others {
		add(r.Workspace)
	}
	return out
}

// webDoorFind is the known project with id.
func webDoorFind(id string) (webDoorProject, bool) {
	for _, p := range webDoorKnown() {
		if p.ID == id {
			return p, true
		}
	}
	return webDoorProject{}, false
}

// webDoorCount is how many projects the door started still run, plus the
// ones being opened; started ones that stopped are forgotten.
func webDoorCount() int {
	for id, dir := range webDoor.opened {
		if projectSessions.Running == nil || !projectSessions.Running(dir) {
			delete(webDoor.opened, id)
		}
	}
	return len(webDoor.opened) + len(webDoor.opening)
}

// webDoorMarkOpening shows the projects the door is opening as
// "opening" in the projects topic (MOB-8 builds the rest).
func webDoorMarkOpening(s *webproto.ProjectsSnap) {
	if len(webDoor.opening) == 0 {
		return
	}
	for i := range s.Projects {
		if it := &s.Projects[i]; it.State != "here" && webDoor.opening[it.WS] != "" {
			it.State = webproto.ProjectOpening
		}
	}
}

// webDoorChanged makes the projects topic build again (an open began or
// ended).
func (m *Model) webDoorChanged() {
	if m.web != nil {
		m.web.projects.dirty = true
	}
}

func init() {
	webAskOps[webproto.OpProjectOpen] = (*Model).webProjectOpen
	webOps[webproto.OpProjectClose] = (*Model).webProjectClose
	webDoorSelf.read(os.Getenv, os.Unsetenv, time.Now())
	webAfterUpdates = append(webAfterUpdates, (*Model).webDoorAfter)
}

// webProjectOpen is project.open's laptop card; nil: it answered itself.
func (m *Model) webProjectOpen(call *webCall) *webAsk {
	id := call.args.(*webproto.ProjectArgs).WS
	ps := projectSessions
	if ps.Start == nil || ps.Running == nil {
		call.fail(webproto.CodeForbidden, "Opening projects from the phone is not supported on this computer.")
		return nil
	}
	if id == webDoorHome() {
		call.fail(webproto.CodeConflict, "This is the project the phone is in.")
		return nil
	}
	cfg := webSettings()
	if !cfg.On {
		call.fail(webproto.CodeForbidden, "The web panel is off in config.json.")
		return nil
	}
	p, ok := webDoorFind(id)
	if !ok {
		call.fail(webproto.CodeNotFound, "This project is not known on the laptop. Open it there once.")
		return nil
	}
	if fi, err := os.Stat(p.Dir); err != nil || !fi.IsDir() {
		call.fail(webproto.CodeGone, "This project's folder is gone.")
		return nil
	}
	if webDoor.opening[id] != "" {
		call.fail(webproto.CodeConflict, "This project is opening already.")
		return nil
	}
	if ps.Running(p.Dir) {
		call.fail(webproto.CodeConflict, "This project runs already.")
		return nil
	}
	if n := webDoorCount(); n >= cfg.ProjectsMax {
		call.fail(webproto.CodeLimit, fmt.Sprintf("Projects open now %d of %d. Close one first.", n, cfg.ProjectsMax))
		return nil
	}
	name := webName(filepath.Base(p.Dir))
	body := "Start " + webName(tildePath(p.Dir)) + " in the background (no window) so the phone can use it?" +
		"\n\nIt stops by itself after " + strconv.Itoa(cfg.ProjectIdleMinutes) + " minutes with no use, unless you open it here." +
		" Run `termocode attach` in that folder to see it on this laptop."
	return &webAsk{
		summary: name,
		title:   "Open " + name + " for the phone?",
		action:  "Open",
		body:    body,
		run:     func(m *Model) tea.Cmd { return m.webDoorStart(id, p.Dir) },
	}
}

// webDoorStart starts a project headless after the laptop's yes. What
// changed while the card waited is checked again.
func (m *Model) webDoorStart(id, dir string) tea.Cmd {
	ps := projectSessions
	name := webName(filepath.Base(dir))
	if ps.Start == nil || webDoor.opening[id] != "" || ps.Running(dir) {
		return nil
	}
	cfg := webSettings()
	if n := webDoorCount(); n >= cfg.ProjectsMax {
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Warn, name+" was not opened", fmt.Sprintf("Projects open now %d of %d.", n, cfg.ProjectsMax))
		return t
	}
	idle := cfg.ProjectIdleMinutes
	if ps.Service != nil && ps.Service(dir) {
		idle = 0 // its service runs it; the door never stops it
	}
	webDoor.opening[id] = dir
	m.webDoorChanged()
	return func() tea.Msg {
		err := ps.Start(dir, idle)
		return applyMsg(func(m *Model) tea.Cmd { return m.webDoorStarted(id, dir, err) })
	}
}

// webDoorStarted ends an open: the toast and the audit line say how.
func (m *Model) webDoorStarted(id, dir string, err error) tea.Cmd {
	delete(webDoor.opening, id)
	m.webDoorChanged()
	name := webName(filepath.Base(dir))
	var t tea.Cmd
	if err != nil {
		msg := ai.Redact(err.Error())
		recordError("[web] open " + name + " for the phone: " + msg)
		m.webAuditLine(webproto.OpProjectOpen, name, "door", "failed: "+msg)
		m.toast, t = m.toast.PushDetail(toast.Errr, "Could not open "+name, msg)
		return t
	}
	webDoor.opened[id] = dir
	m.webAuditLine(webproto.OpProjectOpen, name, "door", "opened headless")
	m.toast, t = m.toast.PushDetail(toast.Info, name+" is open for the phone", "It runs without a window. `termocode attach` in its folder shows it here.")
	return t
}

// ── The project side ──────────────────────────────────────────────────

// webDoorSelfState is this UI as a project the door started (Update only
// after init).
type webDoorSelfState struct {
	idle    time.Duration // 0: not started by the door
	last    time.Time     // the last use (a web call, busy work)
	laptop  bool          // someone attached on the laptop: never stop
	ticking bool
}

var webDoorSelf webDoorSelfState

// read takes EnvDoorIdle from the start env and removes it, so nothing
// this UI starts inherits it.
func (s *webDoorSelfState) read(getenv func(string) string, unset func(string) error, now time.Time) {
	v := getenv(session.EnvDoorIdle)
	if v == "" {
		return
	}
	_ = unset(session.EnvDoorIdle)
	n, err := strconv.Atoi(v)
	if err != nil || n < session.DoorIdleMin || n > session.DoorIdleMax {
		return
	}
	s.idle, s.last = time.Duration(n)*time.Minute, now
}

// webDoorTick is how often a door-started UI looks at its idle time.
const webDoorTick = time.Minute

func webDoorTickCmd() tea.Cmd {
	return tea.Tick(webDoorTick, func(time.Time) tea.Msg { return applyMsg((*Model).webDoorIdleTick) })
}

// webDoorAfter runs after every Update while the hub is busy: a web call
// is use, and the first run starts the idle tick.
func (m *Model) webDoorAfter(msg tea.Msg) tea.Cmd {
	s := &webDoorSelf
	if s.idle == 0 || s.laptop {
		return nil
	}
	if _, ok := msg.(webCallMsg); ok {
		s.last = webNow()
	}
	if s.ticking {
		return nil
	}
	s.ticking = true
	return webDoorTickCmd()
}

// webDoorIdleTick stops this UI when it had no use for the idle time.
func (m *Model) webDoorIdleTick() tea.Cmd {
	s := &webDoorSelf
	if s.idle == 0 || s.laptop || webDoorAttached() {
		s.laptop = s.laptop || s.idle != 0
		s.ticking = false
		return nil
	}
	now := webNow()
	if m.webDoorBusy() != "" {
		s.last = now
	}
	if now.Sub(s.last) < s.idle {
		return webDoorTickCmd()
	}
	s.ticking = false
	m.webAuditLine(webproto.OpProjectClose, webName(filepath.Base(aiWorkspaceRoot())), "door",
		"stopped after "+strconv.Itoa(int(s.idle/time.Minute))+" idle minutes")
	webDoorQuit()
	return nil
}

// webDoorBusy says what keeps this project open ("" for nothing).
func (m *Model) webDoorBusy() string {
	if len(m.agentRollupItems()) > 0 {
		return "An agent works or waits"
	}
	if m.wfx != nil && len(m.wfx.live) > 0 {
		return "A workflow run goes"
	}
	if m.confirmOpen || (m.web != nil && (len(m.web.asks.line) > 0 || m.web.asks.shown != nil)) {
		return "A card waits"
	}
	return ""
}

// webProjectClose is project.close on the project's own hub.
func (m *Model) webProjectClose(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ProjectArgs).WS
	if id != webDoorHome() {
		call.fail(webproto.CodeNotFound, "Close a project through its own hub.")
		return nil
	}
	s := &webDoorSelf
	if s.idle == 0 || s.laptop || webDoorAttached() {
		call.fail(webproto.CodeForbidden, "This project was opened on the laptop. Close it there.")
		return nil
	}
	if why := m.webDoorBusy(); why != "" {
		call.fail(webproto.CodeConflict, why+" here. The project stays open.")
		return nil
	}
	call.reply(webproto.ProjectClosed{WS: id})
	s.idle = 0 // once: no idle stop on top
	// A moment for the answer to leave before the hub goes.
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return applyMsg(func(*Model) tea.Cmd { webDoorQuit(); return nil })
	})
}
