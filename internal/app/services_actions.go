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
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/theme"
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

// displayCommand is argv as a card shows it: long absolute paths become
// "." / "./sub" under root and "~/…" under home. Display only: the argv
// that runs is unchanged.
func displayCommand(argv []string, root string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		short := a
		if filepath.IsAbs(a) {
			if root != "" && a == root {
				short = "."
			} else if root != "" && strings.HasPrefix(a, root+string(filepath.Separator)) {
				short = "." + a[len(root):]
			} else {
				short = tildePath(a)
			}
		}
		if short != a && !strings.ContainsAny(short, " '\"$`\\") {
			q[i] = short
			continue
		}
		q[i] = svcShellLine([]string{a})
	}
	return strings.Join(q, " ")
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
	job := terminalJob{Label: c.Label, Cmdline: svcShellLine(c.Argv), Dir: c.Dir, Service: key}
	if strings.HasPrefix(key, containerTabPrefix) && m.services != nil {
		job.Env = m.services.contextEnv() // the session's DOCKER_CONTEXT
	}
	_, err := m.spawnTerminalJob(job)
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
			var err error
			if !st.busy[svcKey(svcSecCompose, name)].Busy() { // the up tab did not start
				err = fmt.Errorf("docker compose up could not start %s", name)
			}
			done(m, err)
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
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run", Style: confirm.StylePrimary}})
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
	confirmKindContainerRemove
	confirmKindContainerProjectDown
	confirmKindContainerImagePush
	confirmKindContainerHostAction
	confirmKindContainerSearchResult // CP-12: pull or browse a search result
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
	case confirmKindContainerRemove:
		if id == "remove" {
			return m.runContainerRemove(false), true
		}
		if id == "stop-remove" {
			return m.runContainerRemove(true), true
		}
		return nil, true
	case confirmKindContainerProjectDown:
		if id != "down" {
			return nil, true
		}
		project, dir := m.pendingContainerProject, m.pendingContainerDir
		files := append([]string(nil), m.pendingContainerFiles...)
		m.pendingContainerProject, m.pendingContainerDir, m.pendingContainerFiles = "", "", nil
		for _, c := range st.containerRows {
			if c.Project == project {
				st.busy[containerBusyPrefix+c.ID] = services.Stopping
			}
		}
		st.touch()
		cmd := st.containerEngine.ComposeWithFiles(project, dir, files, "down")
		execFor := st.hostExec()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, stderr, code, err := execFor.Capture(ctx, cmd.Dir, cmd.Argv...)
			if err == nil && code != 0 {
				err = fmt.Errorf("%s", errLine(stderr))
			}
			return containerGroupDoneMsg{project: project, err: err}
		}, true
	case confirmKindContainerImagePush:
		ref := m.pendingImageRef
		m.pendingImageRef = ""
		if id != "push" {
			return nil, true
		}
		return m.imagePush(ref), true
	case confirmKindContainerHostAction:
		if id != "run" && id != "remove" && id != "prune" && id != "create" && id != "use" && id != "signout" && id != "tag" {
			return m.runHostAction("cancel"), true
		}
		return m.runHostAction(id), true
	case confirmKindServicesQuit:
		return m.answerServicesQuit(id), true
	case confirmKindContainerSearchResult:
		return m.answerSearchResult(id), true
	}
	return nil, false
}

// servicesAskDown asks before `compose down` (removes containers).
func (m *Model) servicesAskDown() {
	m.confirm = confirm.New("Remove the containers?",
		"This stops and removes this project's containers and networks. Volumes stay.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"},
			{ID: "down", Title: "Remove", Style: confirm.StyleDestructive}})
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
		return m.servicesToast(toast.Info, "Docker Compose is not available", st.composeProblem().Message())
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
	rows, rowsSeq := st.rows(), st.seq
	var r svcRow
	have := st.cursor >= 0 && st.cursor < len(rows)
	if have {
		r = rows[st.cursor]
	}
	// The cursor keys move over the rows just built, unless a handler
	// below changed the view (every change touches it).
	moveRows := func() []svcRow {
		if st.seq == rowsSeq {
			return rows
		}
		return st.rows()
	}
	isSvc := have && r.kind == svcRowService
	if cmd, ok := m.containersFilterKey(k); ok {
		return m, cmd
	}
	if have {
		if cmd, ok := m.remoteRowKey(k.String(), r); ok {
			return m, cmd
		}
		if cmd, ok := m.registryKey(k.String(), r); ok {
			return m, cmd
		}
	}
	switch k.String() {
	case "up", "k":
		m.servicesMoveIn(moveRows(), -1)
	case "down", "j":
		m.servicesMoveIn(moveRows(), 1)
	case "pgup":
		m.servicesMoveIn(moveRows(), -10)
	case "pgdown":
		m.servicesMoveIn(moveRows(), 10)
	case "home", "g":
		st.cursor = 0
		m.servicesClampCursor()
		st.touch()
	case "end", "G":
		st.cursor = len(st.rows()) - 1
		m.servicesClampCursor()
		st.touch()
	case "enter", "l":
		if have && r.kind == svcRowRetry {
			return m, tea.Batch(m.servicesAction("refresh"), m.containersReloadAll())
		}
		if name, ok := m.selectedContext(); ok {
			return m, m.switchContext(name)
		}
		if _, ref, ok := m.selectedImage(); ok && k.String() == "enter" {
			m.askImageRun(ref)
			return m, nil
		}
		if cmd, ok := m.runHelpRow(r); have && ok {
			return m, cmd
		}
		switch {
		case have && r.kind == svcRowHostItem && r.host == hostContainer:
			if c, ok := m.selectedContainer(); ok {
				return m, m.openContainerTerminal(c, "logs")
			}
		case isSvc:
			return m, m.servicesLogs(r.section, r.name)
		case have && r.kind == svcRowSection && r.section == svcSecCompose && st.det.Empty():
			return m, m.servicesConfigure()
		case have && r.kind == svcRowSection && r.section == svcSecCompose:
			return m, m.servicesLogs(svcSecCompose, "")
		case have && (r.kind == svcRowGroup || (r.kind == svcRowSection && strings.HasPrefix(r.section, "host/"))):
			return m, m.servicesToggleFold(r)
		}
	case " ":
		if have && r.foldable() {
			return m, m.servicesToggleFold(r)
		}
	case "s":
		if _, _, ok := m.selectedProjectGroup(); ok {
			return m, m.projectGroupAction("up")
		}
		if c, ok := m.selectedContainer(); ok {
			return m, m.containerAction("start", c)
		}
		if isSvc {
			note, cmd := m.servicesStart(r.section, r.name)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "x":
		if _, _, ok := m.selectedProjectGroup(); ok {
			return m, m.projectGroupAction("stop")
		}
		if c, ok := m.selectedContainer(); ok {
			return m, m.containerAction("stop", c)
		}
		if isSvc {
			note, cmd := m.servicesStop(r.section, r.name, nil)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "r":
		if _, _, ok := m.selectedProjectGroup(); ok {
			return m, m.projectGroupAction("restart")
		}
		if c, ok := m.selectedContainer(); ok {
			return m, m.containerAction("restart", c)
		}
		if sec := r.hostSectionID(); have && sec != "" {
			if st.containerDetected && st.containerEngine.Reachable {
				return m, m.containersRefresh(sec)
			}
			return m, m.containersReloadAll()
		}
		if isSvc {
			note, cmd := m.servicesRestart(r.section, r.name, nil)
			return m, m.servicesNoteCmd(note, cmd)
		}
	case "t":
		if _, ref, ok := m.selectedImage(); ok {
			m.askImageTag(ref)
			return m, nil
		}
		if c, ok := m.selectedContainer(); ok {
			return m, m.openContainerTerminal(c, "attach")
		}
	case "a":
		if st.composeBlocked() {
			return m, m.servicesToast(toast.Info, "Docker is off", "Start it, then press R to retry")
		}
		if !st.det.Empty() {
			return m, m.servicesAction("start-all")
		}
	case "A":
		if st.composeBlocked() {
			return m, m.servicesToast(toast.Info, "Docker is off", "Start it, then press R to retry")
		}
		if !st.det.Empty() {
			return m, m.servicesAction("stop-all")
		}
	case "R":
		return m, tea.Batch(m.servicesAction("refresh"), m.containersReloadAll())
	case "m":
		m.openContainersSectionsMenu("")
		return m, nil
	case "e":
		if c, ok := m.selectedContainer(); ok {
			return m, m.openContainerTerminal(c, "shell")
		}
		if isSvc {
			return m, m.servicesShell(r.section, r.name)
		}
	case "o":
		if c, ok := m.selectedContainer(); ok {
			return m, m.containerOpenPort(c)
		}
		if have && r.hostSectionID() != "" {
			return m, nil
		}
		if st.det.Empty() {
			return m, m.servicesConfigure()
		}
		if isSvc || (have && r.kind == svcRowSection) {
			return m, m.servicesOpenSource(r.section, r.name)
		}
		if st.det.ConfigErr != nil {
			return m, m.servicesConfigure()
		}
		return m, m.servicesOpenSource(svcSecCompose, "")
	case "y":
		if name, ok := m.selectedContext(); ok {
			return m, m.copyContextEndpoint(name)
		}
		if im, _, ok := m.selectedImage(); ok {
			m.copyToClipboard(strings.TrimPrefix(im.ID, "sha256:"))
			return m, m.servicesToast(toast.Info, "Copied image id", shortImageID(im.ID))
		}
		if c, ok := m.selectedContainer(); ok {
			m.copyToClipboard(c.ID)
			return m, m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID))
		}
		if have && r.hostSectionID() != "" && st.containerEngine.Problem.Kind == services.ProblemNoPermission {
			m.copyToClipboard(svcPermissionFix)
			return m, m.servicesToast(toast.Info, "Copied", svcPermissionFix)
		}
		if st.problem.Kind == services.ProblemNoPermission {
			m.copyToClipboard(svcPermissionFix)
			return m, m.servicesToast(toast.Info, "Copied", svcPermissionFix)
		}
		if isSvc {
			return m, m.servicesCopyAddress(r.section, r.name)
		}
	case "p":
		if c, ok := m.selectedContainer(); ok {
			verb := "pause"
			if strings.EqualFold(c.State, "paused") {
				verb = "unpause"
			}
			return m, m.containerAction(verb, c)
		}
	case "z":
		if have && r.kind == svcRowSection && (r.section == hostSection("containers") || r.section == hostSection("images") || r.section == hostSection("networks") || r.section == hostSection("volumes")) {
			return m, m.askPrune(strings.TrimPrefix(r.section, "host/"))
		}
	case "S":
		if c, ok := m.selectedContainer(); ok {
			return m, m.openContainerTerminal(c, "stats")
		}
	case "i":
		if _, ref, ok := m.selectedImage(); ok {
			return m, m.imageInspect(ref)
		}
		if c, ok := m.selectedContainer(); ok {
			return m, m.inspectContainer(c)
		}
	case "D":
		if name, ok := m.selectedContext(); ok {
			return m, m.askContextDefault(name)
		}
		if im, ref, ok := m.selectedImage(); ok {
			return m, m.askImageRemoveRef(im, ref)
		}
		if have && r.kind == svcRowHostItem && (r.host == hostNetwork || r.host == hostVolume) {
			return m, m.askStorageRemove(r)
		}
		if c, ok := m.selectedContainer(); ok {
			m.askRemoveContainer(c)
			return m, nil
		}
		if _, _, ok := m.selectedProjectGroup(); ok {
			return m, m.askProjectDown()
		}
	case "u":
		if _, ref, ok := m.selectedImage(); ok {
			m.askImagePull(ref)
			return m, nil
		}
		if have && r.kind == svcRowGroup && r.hostSectionID() == "images" {
			m.askImagePull(r.name)
			return m, nil
		}
		if have && r.kind == svcRowSection && r.section == hostSection("images") {
			m.askImagePull("")
			return m, nil
		}
	case "P":
		if _, ref, ok := m.selectedImage(); ok {
			m.askImagePush(ref)
			return m, nil
		}
	case "n":
		if have && r.kind == svcRowSection && (r.section == hostSection("networks") || r.section == hostSection("volumes")) {
			m.storageCreatePrompt(strings.TrimPrefix(r.section, "host/"))
			return m, nil
		}
	case "d":
		if have && r.kind == svcRowSection && r.section == hostSection("volumes") {
			return m, m.containerDiskUsage()
		}
	case "h":
		if _, ref, ok := m.selectedImage(); ok {
			return m, m.imageHistory(ref)
		}

	case "tab":
		m.servicesJumpSection(1)
	case "shift+tab":
		m.servicesJumpSection(-1)
	case "?":
		if have {
			title, rows := hostKeysHelp(r)
			return m, m.openKeyCard(title, rows, help.Loc{Page: containersGuidePage, Anchor: containersKeysAnchor}, nil)
		}
	case "esc":
		m.focus = FocusEditor
	}
	return m, nil
}

// servicesJumpSection moves the cursor to the next (dir 1) or previous
// (dir -1) section header and wraps around at the ends.
func (m *Model) servicesJumpSection(dir int) {
	st := m.services
	rows := st.rows()
	n := len(rows)
	for i := 1; i <= n; i++ {
		j := ((st.cursor+dir*i)%n + n) % n
		if rows[j].kind == svcRowSection {
			st.cursor = j
			m.servicesClampCursor()
			st.touch()
			return
		}
	}
}

// hostKeysHelp is the "?" card: the keys of the row kind under the cursor.
func hostKeysHelp(r svcRow) (string, []confirm.KeyRow) {
	nav := [][]string{
		{"j / k", "move"},
		{"Tab / Shift+Tab", "next / previous section"},
		{"/", "filter the rows (Esc clears)"},
		{"Space", "fold or open"},
		{"m", "choose the sections"},
		{"R", "refresh"},
	}
	with := func(title string, own ...[]string) (string, []confirm.KeyRow) {
		return title, keyRows(append(own, nav...)...)
	}
	switch {
	case r.kind == svcRowHostItem && r.host == hostContainer:
		return with("Container keys",
			[]string{"s / x / r", "start / stop / restart"},
			[]string{"p", "pause or unpause"},
			[]string{"e", "shell in the container"},
			[]string{"t", "attach"},
			[]string{"Enter", "logs"},
			[]string{"S", "live stats"},
			[]string{"i", "inspect"},
			[]string{"o", "open a port in the browser"},
			[]string{"y", "copy the id"},
			[]string{"D", "remove (asks first)"})
	case r.kind == svcRowGroup && r.hostSectionID() == "containers":
		return with("Project keys",
			[]string{"s / x / r", "up / stop / restart the project"},
			[]string{"D", "down (asks first)"})
	case r.kind == svcRowHostItem && r.host == hostImage, r.kind == svcRowGroup && r.hostSectionID() == "images":
		return with("Image keys",
			[]string{"Enter", "run (asks first)"},
			[]string{"u", "pull"},
			[]string{"b", "remote tags"},
			[]string{"P", "push (asks first)"},
			[]string{"t", "tag"},
			[]string{"h", "history"},
			[]string{"i", "inspect"},
			[]string{"y", "copy the id"},
			[]string{"D", "remove (asks first)"})
	case r.kind == svcRowHostItem && r.host == hostRemoteRepo:
		return with("Remote tags keys",
			[]string{"Space", "fold the tags"},
			[]string{"r", "reload"},
			[]string{"u", "pull"},
			[]string{"y", "copy"},
			[]string{"x", "remove (REGISTRIES)"})
	case r.kind == svcRowHostItem && r.host == hostRemoteTag:
		return with("Remote tag keys",
			[]string{"Enter / u", "pull"},
			[]string{"y", "copy"})
	case r.kind == svcRowHostItem && (r.host == hostNetwork || r.host == hostVolume):
		return with("Network and volume keys",
			[]string{"Space", "show who uses it"},
			[]string{"n", "create"},
			[]string{"D", "remove (asks first)"},
			[]string{"z", "prune (asks first)"})
	case r.kind == svcRowHostItem && r.host == hostContext:
		return with("Context keys",
			[]string{"Enter", "use it here"},
			[]string{"D", "make it the default"},
			[]string{"y", "copy the endpoint"})
	case r.kind == svcRowHostItem && (r.host == hostRegistry || r.host == hostRegistryImage || r.host == hostRegistryConnect):
		return with("Registry keys",
			[]string{"Enter", "open"},
			[]string{"n", "connect a registry"},
			[]string{"s", "search Docker Hub"},
			[]string{"b", "browse the tags"},
			[]string{"D", "sign out (asks first)"},
			[]string{"u", "pull"})
	case r.kind == svcRowHostItem && isHelpHostKind(r.host):
		return with("Help keys", []string{"Enter", "open"})
	case r.kind == svcRowSection && strings.HasPrefix(r.section, "host/"):
		return with("Section keys",
			[]string{"z", "prune (asks first)"},
			[]string{"u", "pull (images)"},
			[]string{"n", "create (networks, volumes)"},
			[]string{"d", "disk usage (volumes)"})
	case r.kind == svcRowService:
		return with("Service keys",
			[]string{"s / x / r", "start / stop / restart"},
			[]string{"Enter", "logs"},
			[]string{"e", "shell"},
			[]string{"o", "open the source"},
			[]string{"y", "copy the address"},
			[]string{"a / A", "start all / stop all"})
	}
	return with("Containers keys")
}

// hostSectionID is the host section a row belongs to ("" = PROJECT).
func (r svcRow) hostSectionID() string {
	switch {
	case strings.HasPrefix(r.section, "host/"):
		return strings.TrimPrefix(r.section, "host/")
	case strings.HasPrefix(r.section, "grp/"):
		sec, _, _ := strings.Cut(strings.TrimPrefix(r.section, "grp/"), "/")
		return sec
	}
	return ""
}

// servicesToggleFold folds or unfolds a header or group (Space, Enter on
// a host header, a click), saves the choice and loads a host section that
// just opened.
func (m *Model) servicesToggleFold(r svcRow) tea.Cmd {
	st := m.services
	if r.kind == svcRowHostItem && r.host == hostRegistry {
		return m.toggleRegistry(r)
	}
	if r.kind == svcRowHostItem && r.host == hostRemoteRepo {
		return m.toggleRemote(r)
	}
	if r.kind == svcRowHostItem && (r.host == hostNetwork || r.host == hostVolume) {
		key := storageInspectKey(r)
		collapsed, ok := st.collapsed[key]
		if !ok {
			collapsed = true
		}
		st.collapsed[key] = !collapsed
		st.touch()
		if collapsed {
			return m.inspectStorageUsers(r)
		}
		return nil
	}
	st.collapsed[r.section] = !st.collapsed[r.section]
	m.servicesSaveSession()
	st.touch()
	if r.kind == svcRowSection && !st.collapsed[r.section] {
		if sec := r.hostSectionID(); sec != "" {
			return m.containersOpened(sec)
		}
	}
	return nil
}

// openContainersSectionsMenu is the ⋯ menu: a check row per section (the
// menu stays open while toggling) and Refresh all. selected keeps the
// highlight on a row after a toggle.
func (m *Model) openContainersSectionsMenu(selected string) {
	check := "✓"
	if theme.CurrentIconMode == theme.IconModeASCII {
		check = "x"
	}
	items := make([]menu.Item, 0, len(containerSectionOrder)+2)
	for _, section := range containerSectionOrder {
		icon := " "
		if m.services.sectionShown(section) {
			icon = check
		}
		items = append(items, menu.Item{ID: "section:" + section, Title: strings.ToUpper(section[:1]) + section[1:], Icon: icon})
	}
	items = append(items, menu.Item{Sep: true},
		menu.Item{ID: "refresh", Title: "Refresh all", Hint: "R", Icon: theme.GlyphRefresh.String()},
		menu.Item{ID: "prune-system", Title: "Prune system…", Icon: theme.GlyphPrune.String()})
	m.menu = menu.New(items, activity.Width+max(m.explorerWidth-26, 1), 2)
	m.menu.SetScreenSize(m.w, m.h)
	if selected != "" {
		m.menu.SelectID(selected)
	}
	m.menuOpen, m.menuKind = true, menuKindContainers
}

func (m *Model) handleContainersMenuSelect(id string) tea.Cmd {
	st := m.services
	if id == "refresh" {
		return tea.Batch(m.servicesAction("refresh"), m.containersReloadAll())
	}
	if id == "prune-system" {
		return m.askPrune(pruneSystem)
	}
	section, ok := strings.CutPrefix(id, "section:")
	if !ok || !isContainerSection(section) {
		return nil
	}
	st.sections[section] = !st.sectionShown(section)
	cfg := loadSettings()
	cfg.ContainersSections = st.sectionChoice()
	_ = saveSettings(cfg)
	st.touch()
	m.servicesClampCursor()
	m.openContainersSectionsMenu(id)
	if st.sections[section] && section != "project" {
		return m.containersOpened(section)
	}
	return nil
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
	if t == tea.MouseLeft && y == 0 && x == m.explorerWidth-2 {
		m.openContainersSectionsMenu("")
		return m, nil
	}
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
	if st.showEmptyCard() {
		if t == tea.MouseLeft && y > svcSummaryRow {
			return m, m.servicesConfigure()
		}
		return m, nil
	}
	if y == svcActionRow && t == tea.MouseLeft {
		for _, a := range st.actionLayout(w) {
			if x >= a.start && x < a.end {
				if a.disabled {
					return m, m.servicesToast(toast.Info, "Docker is off", "Start it, then press R to retry")
				}
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
		} else if r.kind == svcRowHostItem && r.host == hostContainer {
			m.openContainerMenu(r, activityWidthX(x), y)
		} else if r.kind == svcRowHostItem && r.host == hostContext {
			m.openContextMenu(r, activityWidthX(x), y)
		} else if r.kind == svcRowHostItem && isRegistryHostKind(r.host) {
			m.openRegistryMenu(r, activityWidthX(x), y)
		} else if r.kind == svcRowHostItem && isRemoteHostKind(r.host) {
			m.openRemoteMenu(r, activityWidthX(x), y)
		} else if r.kind == svcRowSection {
			m.openSectionMenu(r, activityWidthX(x), y)
		}
		return m, nil
	}
	if r.kind == svcRowRetry {
		return m, tea.Batch(m.servicesAction("refresh"), m.containersReloadAll())
	}
	if r.foldable() {
		return m, m.servicesToggleFold(r)
	}
	if r.kind == svcRowHostItem && r.host == hostRegistryConnect {
		m.startRegistryConnect("")
		return m, nil
	}
	if cmd, ok := m.runHelpRow(r); ok {
		return m, cmd
	}
	if double && r.kind == svcRowHostItem && r.host == hostContext {
		st.lastClick = time.Time{}
		return m, m.switchContext(r.hostID)
	}
	if double && r.kind == svcRowHostItem && r.host == hostRemoteTag {
		st.lastClick = time.Time{}
		m.askImagePull(r.imageRef)
		return m, nil
	}
	if double && r.kind == svcRowService {
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
	if _, ok := m.dbTargetFromRow(r); ok {
		items = append(items, menu.Item{ID: "databases", Title: dbOpenInDatabasesTitle, Icon: theme.IconDatabases.String()})
	}
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
	if r.kind == svcRowSection {
		return m.handleSectionMenuSelect(id)
	}
	if r.kind == svcRowHostItem && r.host == hostContext {
		return m.handleContextRowMenuSelect(id, r)
	}
	if r.kind == svcRowHostItem && isRegistryHostKind(r.host) {
		return m.handleRegistryMenuSelect(id, r)
	}
	if r.kind == svcRowHostItem && isRemoteHostKind(r.host) {
		return m.handleRemoteMenuSelect(id, r)
	}
	if r.kind == svcRowHostItem {
		return m.handleContainerRowMenuSelect(id, r)
	}
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
	case "databases":
		return m.databaseOpenFromRow(r)
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

// servicesPaletteItems are the "Containers: …" palette rows (the panel's title).
func servicesPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "services-focus", Title: "Containers: Focus Project Services"},
		{ID: "containers-focus", Title: "Containers: Focus"},
		{ID: "containers-refresh", Title: "Containers: Refresh"},
		{ID: "containers-help", Title: "Containers: Open Guide"},
		{ID: "containers-context-use", Title: "Containers: Switch Context…"},
		{ID: "containers-context-default", Title: "Containers: Make Context Default…"},
		{ID: "containers-registry-connect", Title: "Containers: Connect Registry…"},
		{ID: "containers-registry-signout", Title: "Containers: Sign Out of Registry…"},
		{ID: "containers-search", Title: "Containers: Search Docker Hub…"},
		{ID: "containers-browse-tags", Title: "Containers: Browse Remote Tags…"},
		{ID: "containers-filter", Title: "Containers: Filter Rows…"},
		{ID: "containers-start", Title: "Containers: Start Selected"},
		{ID: "containers-stop", Title: "Containers: Stop Selected"},
		{ID: "containers-restart", Title: "Containers: Restart Selected"},
		{ID: "containers-pause", Title: "Containers: Pause or Unpause Selected"},
		{ID: "containers-remove", Title: "Containers: Remove Selected…"},
		{ID: "containers-logs", Title: "Containers: Follow Logs"},
		{ID: "containers-shell", Title: "Containers: Open Shell"},
		{ID: "containers-attach", Title: "Containers: Attach"},
		{ID: "containers-stats", Title: "Containers: Follow Stats"},
		{ID: "containers-inspect", Title: "Containers: Inspect"},
		{ID: "containers-copy-id", Title: "Containers: Copy ID"},
		{ID: "containers-open-databases", Title: "Containers: Open in Databases"},
		{ID: "containers-open-port", Title: "Containers: Open Published Port"},
		{ID: "containers-image-pull", Title: "Containers: Pull Image…"},
		{ID: "containers-image-push", Title: "Containers: Push Selected Image…"},
		{ID: "containers-image-tag", Title: "Containers: Tag Selected Image…"},
		{ID: "containers-image-run", Title: "Containers: Run Selected Image…"},
		{ID: "containers-image-history", Title: "Containers: Show Image History"},
		{ID: "containers-image-inspect", Title: "Containers: Inspect Image"},
		{ID: "containers-image-remove", Title: "Containers: Remove Selected Image…"},
		{ID: "containers-prune", Title: "Containers: Prune Stopped Containers…"},
		{ID: "containers-image-prune", Title: "Containers: Prune Dangling Images…"},
		{ID: "containers-network-create", Title: "Containers: Create Network…"},
		{ID: "containers-network-remove", Title: "Containers: Remove Selected Network…"},
		{ID: "containers-network-prune", Title: "Containers: Prune Networks…"},
		{ID: "containers-volume-create", Title: "Containers: Create Volume…"},
		{ID: "containers-volume-remove", Title: "Containers: Remove Selected Volume…"},
		{ID: "containers-volume-prune", Title: "Containers: Prune Volumes…"},
		{ID: "containers-system-prune", Title: "Containers: Prune System…"},
		{ID: "containers-volume-disk-usage", Title: "Containers: Read Volume Disk Usage"},
		{ID: "containers-compose-up", Title: "Containers: Start Project"},
		{ID: "containers-compose-stop", Title: "Containers: Stop Project"},
		{ID: "containers-compose-restart", Title: "Containers: Restart Project"},
		{ID: "containers-compose-down", Title: "Containers: Remove Project Containers…"},
		{ID: "services-start-all", Title: "Containers: Start All Services"},
		{ID: "services-stop-all", Title: "Containers: Stop All Services"},
		{ID: "services-start", Title: "Containers: Start Service…"},
		{ID: "services-stop", Title: "Containers: Stop Service…"},
		{ID: "services-restart", Title: "Containers: Restart Service…"},
		{ID: "services-logs", Title: "Containers: Show Service Logs…"},
		{ID: "services-refresh", Title: "Containers: Refresh Project Services"},
		{ID: "services-configure", Title: "Containers: Configure Services (services.json)"},
		{ID: "services-open-compose", Title: "Containers: Open Compose File"},
		{ID: "services-trust", Title: "AI: Services Trust…"},
	}
}

func containerAITargetRequired(id string) bool {
	switch id {
	case "containers-start", "containers-stop", "containers-restart", "containers-pause",
		"containers-remove", "containers-logs", "containers-shell", "containers-attach",
		"containers-stats", "containers-inspect", "containers-copy-id", "containers-open-port",
		"containers-image-pull", "containers-image-push", "containers-image-tag", "containers-image-run",
		"containers-image-history", "containers-image-inspect", "containers-image-remove",
		"containers-network-remove", "containers-volume-remove",
		"containers-compose-up", "containers-compose-stop", "containers-compose-restart", "containers-compose-down":
		return true
	default:
		return false
	}
}

func (m *Model) dispatchContainerAIAction(id, target string) (tea.Cmd, error) {
	if strings.HasPrefix(id, "containers-registry-") {
		return nil, errRegistryAI(id)
	}
	if strings.HasSuffix(id, "-prune") {
		return nil, fmt.Errorf("%s is not available to the model: pruning is the user's", id)
	}
	if target == "" {
		return nil, fmt.Errorf("%s needs a named container, image, network, volume, or compose project", id)
	}
	if strings.HasPrefix(id, "containers-image-") {
		if id == "containers-image-pull" {
			return m.imagePull(target), nil
		}
		var image containers.Image
		ref := ""
		for _, candidate := range m.services.imageRows {
			if candidate.ID == target || strings.TrimPrefix(candidate.ID, "sha256:") == target {
				image, ref = candidate, target
				break
			}
			for _, tag := range candidate.RepoTags {
				if tag == target {
					image, ref = candidate, tag
					break
				}
			}
			if ref != "" {
				break
			}
		}
		if ref == "" {
			return nil, fmt.Errorf("image %q was not found", target)
		}
		switch id {
		case "containers-image-push":
			m.askImagePush(ref)
		case "containers-image-tag":
			m.askImageTag(ref)
		case "containers-image-run":
			m.askImageRun(ref)
		case "containers-image-history":
			return m.imageHistory(ref), nil
		case "containers-image-inspect":
			return m.imageInspect(ref), nil
		case "containers-image-remove":
			return m.askImageRemoveRef(image, ref), nil
		}
		return nil, nil
	}
	if id == "containers-network-remove" || id == "containers-volume-remove" {
		kind, host, rows := "network", hostNetwork, len(m.services.networkRows)
		if id == "containers-volume-remove" {
			kind, host, rows = "volume", hostVolume, len(m.services.volumeRows)
		}
		for i := 0; i < rows; i++ {
			var name string
			if host == hostVolume {
				name = m.services.volumeRows[i].Name
			} else {
				name = m.services.networkRows[i].Name
			}
			if name == target {
				return m.askStorageRemove(svcRow{kind: svcRowHostItem, host: host, idx: i, hostID: name, name: name}), nil
			}
		}
		return nil, fmt.Errorf("%s %q was not found", kind, target)
	}
	if strings.HasPrefix(id, "containers-compose-") {
		var row int = -1
		for i, r := range m.services.rows() {
			if r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/containers/") && r.name == target {
				row = i
				break
			}
		}
		if row < 0 {
			return nil, fmt.Errorf("compose project %q was not found", target)
		}
		old := m.services.cursor
		m.services.cursor = row
		var cmd tea.Cmd
		switch id {
		case "containers-compose-up":
			cmd = m.projectGroupAction("up")
		case "containers-compose-stop":
			cmd = m.projectGroupAction("stop")
		case "containers-compose-restart":
			cmd = m.projectGroupAction("restart")
		case "containers-compose-down":
			cmd = m.askProjectDown()
		}
		m.services.cursor = old
		return cmd, nil
	}
	var c containers.Container
	found := false
	for _, item := range m.services.containerRows {
		if item.ID == target {
			c, found = item, true
			break
		}
		for _, name := range item.Names {
			if strings.TrimPrefix(name, "/") == target {
				c, found = item, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("container %q was not found", target)
	}
	switch id {
	case "containers-start":
		return m.containerAction("start", c), nil
	case "containers-stop":
		return m.containerAction("stop", c), nil
	case "containers-restart":
		return m.containerAction("restart", c), nil
	case "containers-pause":
		verb := "pause"
		if strings.EqualFold(c.State, "paused") {
			verb = "unpause"
		}
		return m.containerAction(verb, c), nil
	case "containers-remove":
		m.askRemoveContainer(c)
		return nil, nil
	case "containers-logs":
		return m.openContainerTerminal(c, "logs"), nil
	case "containers-shell":
		return m.openContainerTerminal(c, "shell"), nil
	case "containers-attach":
		return m.openContainerTerminal(c, "attach"), nil
	case "containers-stats":
		return m.openContainerTerminal(c, "stats"), nil
	case "containers-inspect":
		return m.inspectContainer(c), nil
	case "containers-copy-id":
		m.copyToClipboard(c.ID)
		return m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID)), nil
	case "containers-open-port":
		return m.containerOpenPort(c), nil
	}
	return nil, fmt.Errorf("unsupported container command %q", id)
}

// dispatchServicesPalette runs the "Services: …" palette commands.
func (m *Model) dispatchServicesPalette(id string) (tea.Cmd, bool) {
	if id == "containers-focus" {
		return m.revealServicesView(true), true
	}
	if id == "containers-help" {
		return m.openContainersGuide(""), true
	}
	if id == "containers-open-databases" {
		open := m.revealServicesView(false)
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd {
			r, ok := m.servicesCurrentRow()
			if !ok {
				return m.servicesToast(toast.Info, "Select a database container or service first", "")
			}
			return m.databaseOpenFromRow(r)
		}), true
	}
	if strings.HasPrefix(id, "containers-") {
		open := m.revealServicesView(false)
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd {
			if id == "containers-refresh" {
				return tea.Batch(m.servicesAction("refresh"), m.containersReloadAll())
			}
			switch id {
			case "containers-context-use", "containers-context-default":
				name, note := m.contextForPalette()
				if name == "" {
					return note
				}
				if id == "containers-context-use" {
					return m.switchContext(name)
				}
				return m.askContextDefault(name)
			case "containers-registry-connect":
				m.startRegistryConnect("")
				return nil
			case "containers-registry-signout":
				return m.registrySignOutFromPalette()
			case "containers-search":
				m.askSearch()
				return nil
			case "containers-browse-tags":
				return m.browseFromPalette()
			case "containers-filter":
				m.focus = FocusExplorer
				m.services.filterTyping = true
				m.services.touch()
				return nil
			case "containers-image-pull":
				if _, ref, ok := m.selectedImage(); ok {
					m.askImagePull(ref)
					return nil
				}
				m.askImagePull("")
				return nil
			case "containers-image-push":
				if _, ref, ok := m.selectedImage(); ok {
					m.askImagePush(ref)
					return nil
				}
			case "containers-image-tag":
				if _, ref, ok := m.selectedImage(); ok {
					m.askImageTag(ref)
					return nil
				}
			case "containers-image-run":
				if _, ref, ok := m.selectedImage(); ok {
					m.askImageRun(ref)
					return nil
				}
			case "containers-image-history":
				if _, ref, ok := m.selectedImage(); ok {
					return m.imageHistory(ref)
				}
			case "containers-image-inspect":
				if _, ref, ok := m.selectedImage(); ok {
					return m.imageInspect(ref)
				}
			case "containers-image-remove":
				if im, ref, ok := m.selectedImage(); ok {
					return m.askImageRemoveRef(im, ref)
				}
			case "containers-prune":
				return m.askPrune(pruneContainers)
			case "containers-system-prune":
				return m.askPrune(pruneSystem)
			case "containers-image-prune":
				m.askImagePrune()
				return nil
			case "containers-network-create":
				m.storageCreatePrompt("networks")
				return nil
			case "containers-volume-create":
				m.storageCreatePrompt("volumes")
				return nil
			case "containers-network-remove", "containers-volume-remove":
				if r, ok := m.servicesCurrentRow(); ok && r.kind == svcRowHostItem && (r.host == hostNetwork || r.host == hostVolume) {
					return m.askStorageRemove(r)
				}
			case "containers-network-prune":
				return m.askStoragePrune("networks")
			case "containers-volume-prune":
				return m.askStoragePrune("volumes")
			case "containers-volume-disk-usage":
				return m.containerDiskUsage()
			}
			if strings.HasPrefix(id, "containers-compose-") {
				switch strings.TrimPrefix(id, "containers-compose-") {
				case "up":
					return m.projectGroupAction("up")
				case "stop":
					return m.projectGroupAction("stop")
				case "restart":
					return m.projectGroupAction("restart")
				case "down":
					return m.askProjectDown()
				}
			}
			c, ok := m.selectedContainer()
			if !ok {
				return m.servicesToast(toast.Info, "Select a container row first", "")
			}
			switch id {
			case "containers-start":
				return m.containerAction("start", c)
			case "containers-stop":
				return m.containerAction("stop", c)
			case "containers-restart":
				return m.containerAction("restart", c)
			case "containers-pause":
				verb := "pause"
				if strings.EqualFold(c.State, "paused") {
					verb = "unpause"
				}
				return m.containerAction(verb, c)
			case "containers-remove":
				m.askRemoveContainer(c)
			case "containers-logs":
				return m.openContainerTerminal(c, "logs")
			case "containers-shell":
				return m.openContainerTerminal(c, "shell")
			case "containers-attach":
				return m.openContainerTerminal(c, "attach")
			case "containers-stats":
				return m.openContainerTerminal(c, "stats")
			case "containers-inspect":
				return m.inspectContainer(c)
			case "containers-copy-id":
				m.copyToClipboard(c.ID)
				return m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID))
			case "containers-open-port":
				return m.containerOpenPort(c)
			}
			return nil
		}), true
	}
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
