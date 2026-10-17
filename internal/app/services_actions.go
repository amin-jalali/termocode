package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Services view: actions, keys, mouse, menu, palette (SV-5 / SV-6) ────
//
// Visible work (compose up, logs -f, exec, a local service) runs in a
// terminal tab; short work (stop, restart) is captured off the UI
// goroutine with a timeout and the rows show "stopping…" meanwhile.

// servicesActionDoneMsg ends a captured stop / restart / down.
type servicesActionDoneMsg struct {
	verb string
	keys []string
	err  error
	// done, when set, is called on the UI goroutine (AI tools wait here).
	done func(m *Model, err error)
}

func (m *Model) servicesToast(sev toast.Severity, title, detail string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(sev, title, detail)
	return c
}

// svcShellLine quotes argv for the terminal tab's `shell -c`.
func svcShellLine(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '=' || r == '@' || r == ',' ||
				(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
		}) < 0 {
			q[i] = a
			continue
		}
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// servicesRunCommand starts a visible services Command in a tab with key.
func (m *Model) servicesRunCommand(c services.Command, key string) error {
	_, err := m.spawnTerminalJob(terminalJob{Label: c.Label, Cmdline: svcShellLine(c.Argv), Dir: c.Dir, Service: key})
	return err
}

// servicesFocusTab shows the tab with key; false when there is none.
func (m *Model) servicesFocusTab(key string) bool {
	for i, t := range m.terminalTabs {
		if t.Service == key {
			m.terminalActiveTab = i
			if m.nvim != nil && m.terminalWinID > 0 {
				_ = m.nvim.ExecLua(fmt.Sprintf(`pcall(vim.api.nvim_win_set_buf, %d, %d)`, m.terminalWinID, t.BufID))
			}
			m.showPanelTab(panelKindTerminal)
			return true
		}
	}
	return false
}

// svcTabRunning reports a running tab with key.
func (m *Model) svcTabRunning(key string) (terminalTab, bool) {
	for _, t := range m.terminalTabs {
		if t.Service == key && t.Running {
			return t, true
		}
	}
	return terminalTab{}, false
}

// ── Start / stop / restart ───────────────────────────────────────────────

// servicesStart starts one service (or all with name ""). It returns a
// note for the AI ("" on success) and the command.
func (m *Model) servicesStart(section, name string) (string, tea.Cmd) {
	st := m.services
	if section == svcSecLocal {
		return m.servicesStartLocal(name, false)
	}
	c := st.composeBackend()
	if c == nil {
		return "Docker Compose is not available here", nil
	}
	var names []string
	if name != "" {
		s := st.statusOf(svcSecCompose, name)
		if s.State.Busy() {
			return fmt.Sprintf("service %q is busy (%s)", name, s.State), nil
		}
		if s.State.Up() {
			return fmt.Sprintf("service %q is already running", name), nil
		}
		names = []string{name}
	}
	targets := names
	if name == "" {
		for _, s := range st.compose {
			if ss := st.statusOf(svcSecCompose, s.Name); !ss.State.Up() && !ss.State.Busy() {
				targets = append(targets, s.Name)
			}
		}
	}
	if _, running := m.svcTabRunning(svcTabUp); running {
		return "docker compose up is still running in the services: up tab", nil
	}
	if err := m.servicesRunCommand(c.Up(names), svcTabUp); err != nil {
		return "", m.servicesToast(toast.Errr, "Cannot start services", err.Error())
	}
	for _, n := range targets {
		st.busy[svcKey(svcSecCompose, n)] = services.Starting
		st.startedByUs[n] = true
	}
	st.touch()
	m.servicesSaveSession()
	return "", nil
}

// servicesStop stops one service (all with name ""): compose through a
// captured `stop`, local through jobstop. done (optional) runs when it is
// over.
func (m *Model) servicesStop(section, name string, done func(*Model, error)) (string, tea.Cmd) {
	st := m.services
	if section == svcSecLocal {
		return m.servicesStopLocal(name, done)
	}
	c := st.composeBackend()
	if c == nil {
		return "Docker Compose is not available here", nil
	}
	var names, keys []string
	if name != "" {
		s := st.statusOf(svcSecCompose, name)
		if s.State == services.Stopping {
			return fmt.Sprintf("service %q is already stopping", name), nil
		}
		names = []string{name}
	}
	for _, s := range st.compose {
		if name == "" || s.Name == name {
			if name == "" && !st.statusOf(svcSecCompose, s.Name).State.Up() {
				continue
			}
			keys = append(keys, svcKey(svcSecCompose, s.Name))
		}
	}
	for _, k := range keys {
		st.busy[k] = services.Stopping
	}
	st.touch()
	return "", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return servicesActionDoneMsg{verb: "stop", keys: keys, err: c.Stop(ctx, names), done: done}
	}
}

// servicesRestart restarts one service: compose `restart` (or `up -d`
// when it was never created), local stop + start.
func (m *Model) servicesRestart(section, name string, done func(*Model, error)) (string, tea.Cmd) {
	st := m.services
	if section == svcSecLocal {
		return m.servicesStartLocal(name, true)
	}
	c := st.composeBackend()
	if c == nil {
		return "Docker Compose is not available here", nil
	}
	s := st.statusOf(svcSecCompose, name)
	if s.State.Busy() {
		return fmt.Sprintf("service %q is busy (%s)", name, s.State), nil
	}
	if s.State == services.NotCreated {
		note, cmd := m.servicesStart(svcSecCompose, name)
		if done != nil && note == "" {
			done(m, nil)
		}
		return note, cmd
	}
	key := svcKey(svcSecCompose, name)
	st.busy[key] = services.Restarting
	st.touch()
	return "", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return servicesActionDoneMsg{verb: "restart", keys: []string{key}, err: c.Restart(ctx, []string{name}), done: done}
	}
}

func (m *Model) onServicesActionDone(msg servicesActionDoneMsg) tea.Cmd {
	st := m.services
	for _, k := range msg.keys {
		delete(st.busy, k)
		if msg.verb == "stop" || msg.verb == "down" {
			_, name, _ := strings.Cut(k, "/")
			delete(st.startedByUs, name)
		}
	}
	st.touch()
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.servicesToast(toast.Errr, "docker compose "+msg.verb+" failed", msg.err.Error()))
	}
	if msg.verb == "stop" || msg.verb == "down" {
		m.servicesSaveSession()
	}
	if msg.done != nil {
		done, err := msg.done, msg.err
		st.waiters = append(st.waiters, func(m *Model) tea.Cmd { done(m, err); return nil })
	}
	cmds = append(cmds, m.servicesRefresh())
	return tea.Batch(cmds...)
}

// ── Local services ───────────────────────────────────────────────────────

func svcCommandHash(s services.Service) string {
	sum := sha256.Sum256([]byte(s.Name + "\x00" + s.Cwd + "\x00" + s.Command))
	return hex.EncodeToString(sum[:8])
}

// servicesStartLocal runs a local service in its tab. The first start of
// a command line in a project asks first (project files can run
// commands); restart replaces a running tab.
func (m *Model) servicesStartLocal(name string, restart bool) (string, tea.Cmd) {
	st := m.services
	svc, ok := st.findService(svcSecLocal, name)
	if !ok {
		return fmt.Sprintf("no local service %q", name), nil
	}
	if _, running := m.svcTabRunning(svcLocalTab(name)); running && !restart {
		return fmt.Sprintf("service %q is already running", name), nil
	}
	if !st.localOK[svcCommandHash(svc)] {
		m.confirm = confirm.New("Run "+name+"?",
			"This project runs a command from "+svc.Source+":\n\n"+svc.Command+"\n\nRun it in a terminal tab?",
			[]confirm.Button{{ID: "run", Title: "Run", Style: confirm.StylePrimary}, {ID: "cancel", Title: "Cancel"}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindServicesLocal
		st.pendingLocal = name
		return "asked the user to allow the command first", nil
	}
	return "", m.servicesSpawnLocal(svc)
}

func (m *Model) servicesSpawnLocal(svc services.Service) tea.Cmd {
	st := m.services
	v := m.taskVars()
	dir := st.root
	if svc.Cwd != "" {
		dir = filepath.Join(st.root, svc.Cwd)
	}
	env := map[string]any{}
	for k, val := range svc.Env {
		env[k] = tasks.Expand(val, v)
	}
	if _, err := m.spawnTerminalJob(terminalJob{Label: svc.Name, Cmdline: svc.Command, Dir: dir, Env: env, Service: svcLocalTab(svc.Name)}); err != nil {
		return m.servicesToast(toast.Errr, "Cannot start "+svc.Name, err.Error())
	}
	delete(st.localStop, svc.Name)
	st.localSt[svc.Name] = services.Status{Name: svc.Name, State: services.Running, StartedAt: time.Now()}
	st.touch()
	return m.servicesRefresh()
}

// servicesStopLocal sends SIGTERM to the service's job (nvim's jobstop;
// it kills after a timeout).
func (m *Model) servicesStopLocal(name string, done func(*Model, error)) (string, tea.Cmd) {
	st := m.services
	t, running := m.svcTabRunning(svcLocalTab(name))
	if !running {
		return fmt.Sprintf("service %q is not running", name), nil
	}
	st.localStop[name] = true
	st.busy[svcKey(svcSecLocal, name)] = services.Stopping
	st.touch()
	if m.nvim != nil {
		_ = m.nvim.ExecLua(fmt.Sprintf(`
			local b = %d
			if vim.api.nvim_buf_is_loaded(b) then
				local ok, chan = pcall(function() return vim.bo[b].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
			end
		`, t.BufID))
	}
	if done != nil {
		st.waiters = append(st.waiters, func(m *Model) tea.Cmd { done(m, nil); return nil })
	}
	return "", nil
}

// servicesLocalExitToast is the exit toast of a local service, with the
// Android hint for low ports.
func (m *Model) servicesLocalExitToast(name string, code int) tea.Cmd {
	detail := "l logs · r restart"
	if svc, ok := m.services.findService(svcSecLocal, name); ok && runtime.GOOS == "android" {
		for _, p := range svc.Ports {
			if p.Published > 0 && p.Published < 1024 {
				detail = "Android does not allow ports below 1024 · " + detail
			}
		}
	}
	return m.servicesToast(toast.Errr, fmt.Sprintf("Service %q exited with code %d", name, code), detail)
}

// ── Confirm cards ────────────────────────────────────────────────────────

const (
	confirmKindServicesLocal confirmKindEnum = 990 + iota
	confirmKindServicesDown
	confirmKindServicesQuit
)

// handleServicesConfirm routes the services confirm kinds.
func (m *Model) handleServicesConfirm(id string) (tea.Cmd, bool) {
	st := m.services
	switch m.confirmKind {
	case confirmKindServicesLocal:
		name := st.pendingLocal
		st.pendingLocal = ""
		if id != "run" {
			return nil, true
		}
		svc, ok := st.findService(svcSecLocal, name)
		if !ok {
			return nil, true
		}
		st.localOK[svcCommandHash(svc)] = true
		m.servicesSaveSession()
		return m.servicesSpawnLocal(svc), true
	case confirmKindServicesDown:
		if id != "down" {
			return nil, true
		}
		c := st.composeBackend()
		if c == nil {
			return nil, true
		}
		var keys []string
		for _, s := range st.compose {
			k := svcKey(svcSecCompose, s.Name)
			st.busy[k] = services.Stopping
			keys = append(keys, k)
		}
		st.touch()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			return servicesActionDoneMsg{verb: "down", keys: keys, err: c.Down(ctx)}
		}, true
	case confirmKindServicesQuit:
		return m.answerServicesQuit(id), true
	}
	return nil, false
}

// servicesAskDown asks before `compose down` (removes containers).
func (m *Model) servicesAskDown() {
	m.confirm = confirm.New("Remove the containers?",
		"docker compose down stops and removes this project's containers and networks. Volumes stay.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel", Style: confirm.StylePrimary},
			{ID: "down", Title: "Down", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindServicesDown
}

// ── Other row actions ────────────────────────────────────────────────────

// servicesLogs opens (or focuses) the logs of a service ("" = all
// compose services). Local services: their own tab.
func (m *Model) servicesLogs(section, name string) tea.Cmd {
	st := m.services
	if section == svcSecLocal {
		if !m.servicesFocusTab(svcLocalTab(name)) {
			return m.servicesToast(toast.Info, fmt.Sprintf("%q has not run yet", name), "s starts it in a terminal tab")
		}
		return nil
	}
	key := svcLogsTab(name)
	if name == "" {
		key = svcLogsTab("compose")
	}
	if t, ok := m.svcTabRunning(key); ok && t.Running && m.servicesFocusTab(key) {
		return nil
	}
	c := st.composeBackend()
	if c == nil {
		return m.servicesToast(toast.Info, "Docker Compose is not available", st.problem.Message())
	}
	if err := m.servicesRunCommand(c.LogsFollow(name), key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot show logs", err.Error())
	}
	return nil
}

// servicesShell opens `compose exec <name> sh` in a tab.
func (m *Model) servicesShell(section, name string) tea.Cmd {
	c := m.services.composeBackend()
	if section != svcSecCompose || c == nil {
		return m.servicesToast(toast.Info, "No shell for local services", "Their terminal tab is the shell")
	}
	if err := m.servicesRunCommand(c.Shell(name), "shell:"+name); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open a shell", err.Error())
	}
	return nil
}

// servicesAddress is "localhost:<published port>" of a service.
func (m *Model) servicesAddress(section, name string) (string, int) {
	st := m.services
	svc, _ := st.findService(section, name)
	s := st.statusOf(section, name)
	for _, list := range [][]services.Port{s.Ports, svc.Ports} {
		for _, p := range list {
			if p.Published > 0 {
				return "localhost:" + strconv.Itoa(p.Published), p.Published
			}
		}
	}
	if svc.Health.TCP > 0 {
		return "localhost:" + strconv.Itoa(svc.Health.TCP), svc.Health.TCP
	}
	return "", 0
}

// servicesCopyAddress copies the address; over SSH the toast adds the
// port-forward command (we never open tunnels).
func (m *Model) servicesCopyAddress(section, name string) tea.Cmd {
	addr, port := m.servicesAddress(section, name)
	if addr == "" {
		return m.servicesToast(toast.Info, "No published port", name+" has no port to copy")
	}
	m.copyToClipboard(addr)
	detail := addr
	if conn := os.Getenv("SSH_CONNECTION"); conn != "" || os.Getenv("SSH_TTY") != "" {
		host := "<host>"
		if f := strings.Fields(conn); len(f) >= 3 {
			host = f[2]
		}
		detail = fmt.Sprintf("over SSH: ssh -L %d:localhost:%d %s", port, port, host)
	}
	return m.servicesToast(toast.Info, "Copied "+addr, detail)
}

// servicesOpenSource opens the file a service comes from at its line.
func (m *Model) servicesOpenSource(section, name string) tea.Cmd {
	st := m.services
	if section == svcSecCompose || (name == "" && section == "") {
		if len(st.det.ComposeFiles) == 0 {
			return m.servicesConfigure()
		}
		path := st.det.ComposeFiles[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(st.root, path)
		}
		line := 1
		if b, err := os.ReadFile(path); err == nil && name != "" {
			if l := services.ComposeServiceLines(string(b))[name]; l > 0 {
				line = l
			}
		}
		m.openTestLocation(path, line)
		return nil
	}
	svc, ok := st.findService(svcSecLocal, name)
	if !ok {
		return m.servicesConfigure()
	}
	path := filepath.Join(st.root, svc.Source)
	line := 1
	if b, err := os.ReadFile(path); err == nil {
		for i, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, `"`+name+`"`) || strings.HasPrefix(strings.TrimSpace(l), name+":") {
				line = i + 1
				break
			}
		}
	}
	m.openTestLocation(path, line)
	return nil
}

// servicesConfigure writes .termocode/services.json (a commented
// template) when missing and opens it.
func (m *Model) servicesConfigure() tea.Cmd {
	root := m.taskVars().WorkspaceFolder
	if st := m.services; st != nil && st.root != "" {
		root = st.root
	}
	path := filepath.Join(root, services.ConfigPath)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		werr := os.MkdirAll(filepath.Dir(path), 0o755)
		if werr == nil {
			werr = os.WriteFile(path, []byte(services.Template), 0o644)
		}
		if werr != nil {
			return m.servicesToast(toast.Errr, "Cannot create services.json", werr.Error())
		}
	}
	m.openTestLocation(path, 1)
	return m.servicesRefresh()
}

// ── Keyboard ─────────────────────────────────────────────────────────────

func (m Model) handleServicesSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.services
	r, have := m.servicesCurrentRow()
	isSvc := have && r.kind == svcRowService
	switch k.String() {
	case "up", "k":
		m.servicesMove(-1)
	case "down", "j":
		m.servicesMove(1)
	case "pgup":
		m.servicesMove(-10)
	case "pgdown":
		m.servicesMove(10)
	case "home", "g":
		st.cursor = 0
		m.servicesClampCursor()
		st.touch()
	case "end", "G":
		st.cursor = len(st.rows()) - 1
		m.servicesClampCursor()
		st.touch()
	case "enter", "l":
		switch {
		case isSvc:
			return m, m.servicesLogs(r.section, r.name)
		case have && r.kind == svcRowSection && r.section == svcSecCompose:
			return m, m.servicesLogs(svcSecCompose, "")
		}
	case " ":
		if have && r.kind == svcRowSection {
			st.collapsed[r.section] = !st.collapsed[r.section]
			st.touch()
		}
	case "s":
		if isSvc {
			note, cmd := m.servicesStart(r.section, r.name)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "x":
		if isSvc {
			note, cmd := m.servicesStop(r.section, r.name, nil)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "r":
		if isSvc {
			note, cmd := m.servicesRestart(r.section, r.name, nil)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "a":
		return m, m.servicesAction("start-all")
	case "A":
		return m, m.servicesAction("stop-all")
	case "R":
		return m, m.servicesAction("refresh")
	case "e":
		if isSvc {
			return m, m.servicesShell(r.section, r.name)
		}
	case "o":
		if isSvc || (have && r.kind == svcRowSection) {
			return m, m.servicesOpenSource(r.section, r.name)
		}
		if st.det.ConfigErr != nil || st.det.Empty() {
			return m, m.servicesConfigure()
		}
		return m, m.servicesOpenSource(svcSecCompose, "")
	case "y":
		if st.problem.Kind == services.ProblemNoPermission {
			m.copyToClipboard(svcPermissionFix)
			return m, m.servicesToast(toast.Info, "Copied", svcPermissionFix)
		}
		if isSvc {
			return m, m.servicesCopyAddress(r.section, r.name)
		}
	case "esc":
		m.focus = FocusEditor
	}
	return m, nil
}

// servicesNoteCmd shows a "nothing to do" note as a toast.
func (m *Model) servicesNoteCmd(note string, cmd tea.Cmd) tea.Cmd {
	if note != "" && cmd == nil && !m.confirmOpen {
		return m.servicesToast(toast.Info, note, "")
	}
	return cmd
}

// servicesAction runs an action-bar / palette action by id.
func (m *Model) servicesAction(id string) tea.Cmd {
	switch id {
	case "start-all":
		var cmds []tea.Cmd
		if len(m.services.compose) > 0 {
			note, cmd := m.servicesStart(svcSecCompose, "")
			cmds = append(cmds, m.servicesNoteCmd(note, cmd))
		}
		for _, s := range m.services.local {
			if _, running := m.svcTabRunning(svcLocalTab(s.Name)); !running && !m.confirmOpen {
				_, cmd := m.servicesStartLocal(s.Name, false)
				cmds = append(cmds, cmd)
			}
		}
		return tea.Batch(cmds...)
	case "stop-all":
		var cmds []tea.Cmd
		if len(m.services.compose) > 0 {
			_, cmd := m.servicesStop(svcSecCompose, "", nil)
			cmds = append(cmds, cmd)
		}
		for _, s := range m.services.local {
			_, cmd := m.servicesStopLocal(s.Name, nil)
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	case "refresh":
		m.services.cfgCache.mu.Lock()
		m.services.cfgCache.key = ""
		m.services.cfgCache.mu.Unlock()
		return m.servicesRefresh()
	}
	return nil
}

// ── Mouse ────────────────────────────────────────────────────────────────

func (m Model) handleServicesSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	st := m.services
	switch t {
	case tea.MouseWheelUp:
		m.servicesMove(-3)
		return m, nil
	case tea.MouseWheelDown:
		m.servicesMove(3)
		return m, nil
	case tea.MouseLeft, tea.MouseRight:
	default:
		return m, nil
	}
	m.focus = FocusExplorer
	w := m.explorerWidth - 1
	if st.det.Empty() && st.loaded {
		if t == tea.MouseLeft && y > svcSummaryRow {
			return m, m.servicesConfigure()
		}
		return m, nil
	}
	if y == svcActionRow && t == tea.MouseLeft {
		for _, a := range svcActionLayout(w) {
			if x >= a.start && x < a.end {
				return m, m.servicesAction(a.id)
			}
		}
		return m, nil
	}
	idx := m.servicesRowAt(y)
	if idx < 0 {
		return m, nil
	}
	rows := st.rows()
	r := rows[idx]
	if !r.selectable() {
		return m, nil
	}
	double := idx == st.cursor && time.Since(st.lastClick) < 400*time.Millisecond
	st.cursor = idx
	st.lastClick = time.Now()
	st.touch()
	if t == tea.MouseRight {
		if r.kind == svcRowService {
			m.openServicesMenu(r, activityWidthX(x), y)
		}
		return m, nil
	}
	if r.kind == svcRowSection {
		st.collapsed[r.section] = !st.collapsed[r.section]
		return m, nil
	}
	if double {
		st.lastClick = time.Time{}
		return m, m.servicesLogs(r.section, r.name)
	}
	return m, nil
}

// activityWidthX turns a sidebar-local x into a screen x.
func activityWidthX(x int) int { return x + activity.Width }

// servicesRowAt maps a sidebar row y to a row index (-1 = none).
func (m Model) servicesRowAt(y int) int {
	st := m.services
	line := y - svcListTop
	if line < 0 || line >= svcBodyH(m.h) {
		return -1
	}
	lines := svcBodyLines(st.rows(), st.cursor, svcBodyH(m.h))
	if line >= len(lines) {
		return -1
	}
	return lines[line]
}

// ── Right-click menu ─────────────────────────────────────────────────────

func (m *Model) openServicesMenu(r svcRow, x, y int) {
	st := m.services
	st.menuRow = r
	items := []menu.Item{
		{ID: "start", Title: "Start", Hint: "s", Icon: "▸"},
		{ID: "stop", Title: "Stop", Hint: "x", Icon: "▪"},
		{ID: "restart", Title: "Restart", Hint: "r", Icon: "↻"},
		{ID: "logs", Title: "Logs", Hint: "enter", Icon: "≡"},
	}
	if r.section == svcSecCompose {
		items = append(items, menu.Item{ID: "shell", Title: "Shell in container", Hint: "e", Icon: "$"})
	}
	src := "Open " + services.ConfigPath
	if r.section == svcSecCompose && len(st.det.ComposeFiles) > 0 {
		src = "Open " + filepath.Base(st.det.ComposeFiles[0])
	} else if svc, ok := st.findService(r.section, r.name); ok {
		src = "Open " + filepath.Base(svc.Source)
	}
	items = append(items, menu.Item{Sep: true},
		menu.Item{ID: "copy", Title: "Copy address", Hint: "y", Icon: "⎘"},
		menu.Item{ID: "open", Title: src, Hint: "o", Icon: "✎"})
	if r.section == svcSecCompose {
		items = append(items, menu.Item{Sep: true},
			menu.Item{ID: "recreate", Title: "Recreate (up --force-recreate)", Icon: "⊘"},
			menu.Item{ID: "down", Title: "Down (remove containers)…", Icon: "✕"})
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindServices
}

// handleServicesMenuSelect runs a context-menu action on the row it was
// opened on.
func (m *Model) handleServicesMenuSelect(id string) tea.Cmd {
	r := m.services.menuRow
	switch id {
	case "start":
		note, cmd := m.servicesStart(r.section, r.name)
		return m.servicesNoteCmd(note, cmd)
	case "stop":
		note, cmd := m.servicesStop(r.section, r.name, nil)
		return m.servicesNoteCmd(note, cmd)
	case "restart":
		note, cmd := m.servicesRestart(r.section, r.name, nil)
		return m.servicesNoteCmd(note, cmd)
	case "logs":
		return m.servicesLogs(r.section, r.name)
	case "shell":
		return m.servicesShell(r.section, r.name)
	case "copy":
		return m.servicesCopyAddress(r.section, r.name)
	case "open":
		return m.servicesOpenSource(r.section, r.name)
	case "recreate":
		if c := m.services.composeBackend(); c != nil {
			if err := m.servicesRunCommand(c.Recreate(r.name), svcTabUp); err != nil {
				return m.servicesToast(toast.Errr, "Cannot recreate", err.Error())
			}
		}
	case "down":
		m.servicesAskDown()
	}
	return nil
}

// ── Palette ──────────────────────────────────────────────────────────────

// servicesPaletteItems are the "Services: …" palette rows.
func servicesPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "services-focus", Title: "Services: Focus Services"},
		{ID: "services-start-all", Title: "Services: Start All"},
		{ID: "services-stop-all", Title: "Services: Stop All"},
		{ID: "services-start", Title: "Services: Start Service..."},
		{ID: "services-stop", Title: "Services: Stop Service..."},
		{ID: "services-restart", Title: "Services: Restart Service..."},
		{ID: "services-logs", Title: "Services: Show Logs..."},
		{ID: "services-refresh", Title: "Services: Refresh"},
		{ID: "services-configure", Title: "Services: Configure (services.json)"},
		{ID: "services-open-compose", Title: "Services: Open Compose File"},
		{ID: "services-trust", Title: "AI: Services Trust..."},
	}
}

// dispatchServicesPalette runs the "Services: …" palette commands.
func (m *Model) dispatchServicesPalette(id string) (tea.Cmd, bool) {
	if !strings.HasPrefix(id, "services-") {
		return nil, false
	}
	open := m.revealServicesView(id == "services-focus")
	switch id {
	case "services-focus", "services-refresh":
		if id == "services-refresh" {
			return tea.Batch(open, m.servicesAction("refresh")), true
		}
		return open, true
	case "services-start-all", "services-stop-all":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.servicesAction(strings.TrimPrefix(id, "services-")) }), true
	case "services-start", "services-stop", "services-restart", "services-logs":
		verb := strings.TrimPrefix(id, "services-")
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.openServicesPicker(verb) }), true
	case "services-configure":
		return tea.Batch(open, m.servicesConfigure()), true
	case "services-open-compose":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.servicesOpenSource(svcSecCompose, "") }), true
	case "services-trust":
		return tea.Batch(open, m.openServicesTrust()), true
	}
	return open, true
}

// servicesAfterLoad runs f now when a snapshot is there, else after the
// first one arrives.
func (m *Model) servicesAfterLoad(open tea.Cmd, f func(m *Model) tea.Cmd) tea.Cmd {
	st := m.services
	if st.loaded {
		return tea.Batch(open, f(m))
	}
	st.waiters = append(st.waiters, f)
	return open
}

// openServicesPicker lists the services for one verb.
func (m *Model) openServicesPicker(verb string) tea.Cmd {
	st := m.services
	var items []picker.Item
	add := func(section string, list []services.Service) {
		for _, s := range list {
			ss := st.statusOf(section, s.Name)
			items = append(items, picker.Item{ID: "svc:" + verb + ":" + svcKey(section, s.Name),
				Title: s.Name, Hint: section + " · " + ss.State.String()})
		}
	}
	add(svcSecCompose, st.compose)
	add(svcSecLocal, st.local)
	if len(items) == 0 {
		return m.servicesToast(toast.Info, "No services", "Add compose.yaml, a Procfile or .termocode/services.json")
	}
	m.openTasksPickerItems(" "+strings.ToUpper(verb[:1])+verb[1:]+" Service ", items)
	return nil
}

// handleServicesPickerSelect runs a picked "svc:<verb>:<section>/<name>".
func (m *Model) handleServicesPickerSelect(id string) tea.Cmd {
	rest := strings.TrimPrefix(id, "svc:")
	verb, key, _ := strings.Cut(rest, ":")
	section, name, _ := strings.Cut(key, "/")
	switch verb {
	case "start":
		note, cmd := m.servicesStart(section, name)
		return m.servicesNoteCmd(note, cmd)
	case "stop":
		note, cmd := m.servicesStop(section, name, nil)
		return m.servicesNoteCmd(note, cmd)
	case "restart":
		note, cmd := m.servicesRestart(section, name, nil)
		return m.servicesNoteCmd(note, cmd)
	case "logs":
		return m.servicesLogs(section, name)
	}
	return nil
}
