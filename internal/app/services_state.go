package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Services view state (services panel plan, SV-4 / SV-10) ─────────────
//
// Nothing runs until the view is first shown. While it is visible, one
// host-wide `docker events` process tells us when to refresh shown sections; when
// it hides (another activity item, sidebar closed, zen mode, quit) the
// watcher is killed, so a hidden view has no process and no goroutine.

const (
	svcSecCompose = "compose"
	svcSecLocal   = "local"

	svcTabUp = "up" // terminalTab.Service of the `compose up` tab

	svcRefreshKey      = "services-refresh"
	svcRefreshDebounce = 150 * time.Millisecond
	svcProbeTimeout    = 300 * time.Millisecond
)

// svcKey names one service: "compose/postgres", "local/web".
func svcKey(section, name string) string { return section + "/" + name }

// svcLocalTab is the terminalTab.Service key of a local service's tab.
func svcLocalTab(name string) string { return "local:" + name }

// svcLogsTab is the terminalTab.Service key of a `logs -f` tab.
func svcLogsTab(name string) string { return "logs:" + name }

type servicesState struct {
	opened bool // the view was shown once (detection ran)
	loaded bool // a snapshot arrived
	root   string

	det      services.Detection
	project  string
	compose  []services.Service
	status   map[string]services.Status // compose, by name
	problem  services.Problem
	local    []services.Service
	localSt  map[string]services.Status // local, by name (from the job tabs)
	probes   map[string]bool            // svcKey → port open
	at       time.Time
	prevUp   map[string]bool // compose names up in the previous snapshot
	exitSeen map[string]string

	busy        map[string]services.State // svcKey → Starting / Stopping / Restarting
	startedByUs map[string]bool           // compose names this session started
	aiAllowed   *atomic.Bool              // "Always in this project" (read by the AI gate goroutine)
	localOK     map[string]bool           // local command hashes the user OK'd
	localStop   map[string]bool           // local services we are stopping (no exit toast)
	sessLoaded  bool

	cursor    int
	collapsed map[string]bool
	sections  map[string]bool // visible host sections; missing keys use the all-visible default
	quitAsked bool            // the quit card was answered

	// refresh bookkeeping (UI goroutine only)
	reqSeq   uint64
	doneSeq  uint64 // the last snapshot applied
	inflight bool
	pending  bool
	seq      uint64                   // bumps on every visible change: the render cache key
	waiters  []func(m *Model) tea.Cmd // run after the next snapshot

	pendingLocal string    // local service waiting for the first-run OK
	lastClick    time.Time // double-click on the same row = logs
	menuRow      svcRow    // the row the context menu is for
	cfgCache     *svcConfigCache
	stale        bool // the view hid: events may have been missed
	everShown    bool

	// events watcher (alive only while the view is visible)
	watchStop   func()
	watchCh     <-chan services.Event
	watchCtx    context.Context
	watchCancel context.CancelFunc
	watchGen    uint64
	visible     bool

	render svcRenderCache

	containerEngine     containers.Engine
	containerDetected   bool
	containerDetecting  bool
	containerCancel     context.CancelFunc
	containerCancels    map[string]context.CancelFunc
	containerGen        uint64
	containerRows       []containers.Container
	imageRows           []containers.Image
	networkRows         []containers.Network
	volumeRows          []containers.Volume
	contextRows         []containers.Context
	registryRows        []containers.Registry
	storageUsers        map[string][]string
	storageInspectState map[string]string
	containerErrors     map[string]string
	hostLoaded          map[string]bool // host sections that got a first answer
	hostWatchStop       func()
	hostWatchCh         <-chan containers.Event
	hostWatchCtx        context.Context
	hostWatchCancel     context.CancelFunc
	hostWatchGen        uint64
}

func newServicesState() *servicesState {
	collapsed := map[string]bool{}
	for _, section := range hostSectionOrder {
		collapsed[hostSection(section)] = defaultHostSectionCollapsed(section)
	}
	return &servicesState{status: map[string]services.Status{}, localSt: map[string]services.Status{},
		busy: map[string]services.State{}, startedByUs: map[string]bool{}, collapsed: collapsed,
		sections:         map[string]bool{},
		containerErrors:  map[string]string{},
		containerCancels: map[string]context.CancelFunc{},
		storageUsers:     map[string][]string{}, storageInspectState: map[string]string{},
		hostLoaded: map[string]bool{},
		localOK:    map[string]bool{}, localStop: map[string]bool{}, exitSeen: map[string]string{}, prevUp: map[string]bool{},
		cfgCache: &svcConfigCache{}, aiAllowed: &atomic.Bool{}}
}

func (st *servicesState) touch() { st.seq++ }

// servicesExec is the docker runner; tests leave it to the PATH (the fake
// docker of internal/services/servicestest).
var servicesExec services.Exec = services.SpawnExec{}

// compose builds the compose backend for the current detection.
func (st *servicesState) composeBackend() *services.Compose {
	if !st.det.Compose {
		return nil
	}
	return &services.Compose{Root: st.root, Files: st.det.ComposeFiles, Profiles: st.det.Profiles,
		Project: st.det.Project, X: servicesExec}
}

// findService returns the declared service of a section by name.
func (st *servicesState) findService(section, name string) (services.Service, bool) {
	list := st.compose
	if section == svcSecLocal {
		list = st.local
	}
	for _, s := range list {
		if s.Name == name {
			return s, true
		}
	}
	return services.Service{}, false
}

// statusOf is the status a row shows: ps / the job tab, then the busy
// overlay (a start or stop in flight), then the probe.
func (st *servicesState) statusOf(section, name string) services.Status {
	var s services.Status
	var ok bool
	if section == svcSecLocal {
		s, ok = st.localSt[name]
		if !ok {
			s = services.Status{Name: name, State: services.Stopped}
		}
	} else {
		s, ok = st.status[name]
		if !ok {
			s = services.Status{Name: name, State: services.NotCreated}
		}
		if pk := st.problem.Kind; pk == services.ProblemDaemonDown || pk == services.ProblemNoPermission || pk == services.ProblemNoDocker || pk == services.ProblemNoCompose {
			s.State = services.Unknown
		}
	}
	if b, ok := st.busy[svcKey(section, name)]; ok {
		s.State = b
	}
	if open, ok := st.probes[svcKey(section, name)]; ok && s.State.Up() {
		o := open
		s.PortOpen = &o
	}
	return s
}

// counts are (up, total, bad) over every declared service.
func (st *servicesState) counts() (up, total, bad int) {
	each := func(section string, list []services.Service) {
		for _, s := range list {
			total++
			ss := st.statusOf(section, s.Name)
			if ss.State.Up() {
				up++
			}
			if ss.State == services.Unhealthy || (ss.State == services.Exited && ss.ExitCode != 0) {
				bad++
			}
		}
	}
	each(svcSecCompose, st.compose)
	each(svcSecLocal, st.local)
	return up, total, bad
}

// ── Opening the view ─────────────────────────────────────────────────────

// servicesVisible reports whether the Services view is on screen.
func (m Model) servicesVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewServices
}

// revealServicesView shows the Services view (focused when asked) and
// starts detection the first time.
func (m *Model) revealServicesView(focus bool) tea.Cmd {
	m.activity.SetActive(activity.ViewServices)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
	return m.servicesSyncWatch()
}

// servicesSyncWatch follows the view's visibility: shown → refresh and
// start the host watcher after engine detection; hidden → stop the watcher.
// Called after every update; cheap when nothing changed.
func (m *Model) servicesSyncWatch() tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	vis := m.servicesVisible()
	if vis == st.visible {
		return nil
	}
	st.visible = vis
	if !vis {
		if st.hostWatchStop != nil || st.watchStop != nil {
			st.stale = true
		}
		st.stopWatch()
		st.stopHostWatch()
		st.cancelHostWork()
		st.touch()
		return nil
	}
	if !st.opened {
		st.opened = true
		st.root = m.taskVars().WorkspaceFolder
		m.servicesLoadSession()
	}
	st.everShown = true
	return tea.Batch(m.servicesRefresh(), m.containersLoadVisible(), m.startHostWatch())
}

// stopWatch kills the docker events process (if any) and waits for it.
func (st *servicesState) stopWatch() {
	if st.watchStop != nil {
		stop := st.watchStop
		st.watchStop, st.watchCh = nil, nil
		if st.watchCancel != nil {
			st.watchCancel()
			st.watchCancel = nil
		}
		st.watchCtx = nil
		st.watchGen++
		stop()
	}
}

// shutdown is the quit path: no services process may outlive termocode.
func (st *servicesState) shutdown() {
	if st != nil {
		st.stopWatch()
		st.stopHostWatch()
	}
}

// ── Refresh ──────────────────────────────────────────────────────────────

type servicesSnapshotMsg struct {
	seq     uint64
	root    string
	det     services.Detection
	cfg     services.Config
	status  map[string]services.Status
	problem services.Problem
	probes  map[string]bool
	at      time.Time
}

type servicesRefreshMsg struct{}

// servicesRefresh reads the snapshot off the UI goroutine: detection,
// `compose config` (cached by file times), `compose ps`, and a TCP / HTTP
// probe of running services without a healthcheck. At most one runs; a
// request while one runs runs once more after it.
func (m *Model) servicesRefresh() tea.Cmd {
	st := m.services
	if st == nil || !st.opened {
		return nil
	}
	if st.inflight {
		st.pending = true
		return nil
	}
	st.inflight = true
	st.reqSeq++
	seq, root, cache := st.reqSeq, st.root, st.cfgCache
	var runningLocal []services.Service
	for _, s := range st.local {
		if ls, ok := st.localSt[s.Name]; ok && ls.State.Up() {
			runningLocal = append(runningLocal, s)
		}
	}
	return func() tea.Msg { return servicesLoad(seq, root, cache, runningLocal) }
}

// servicesLoad does the slow part of a refresh.
func servicesLoad(seq uint64, root string, cache *svcConfigCache, runningLocal []services.Service) servicesSnapshotMsg {
	det := services.Detect(root, os.Getenv, exec.LookPath, runtime.GOOS)
	msg := servicesSnapshotMsg{seq: seq, root: root, det: det, problem: det.Problem, probes: map[string]bool{}, at: clock.Now()}
	for _, v := range services.EnvFileSecrets(filepath.Join(root, ".env")) {
		ai.RegisterSecret(v)
	}
	if det.Compose {
		c := &services.Compose{Root: root, Files: det.ComposeFiles, Profiles: det.Profiles, Project: det.Project, X: servicesExec}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cfg, p := cache.load(ctx, c, root)
		msg.cfg, msg.problem = cfg, p
		if p.Kind == services.ProblemNone {
			for _, v := range cfg.SecretValues {
				ai.RegisterSecret(v)
			}
			msg.status, msg.problem = c.Status(ctx)
			for _, s := range cfg.Services {
				if ok := services.ProbeService(s, msg.status[s.Name], svcProbeTimeout); ok != nil {
					msg.probes[svcKey(svcSecCompose, s.Name)] = *ok
				}
			}
		}
	}
	for _, s := range runningLocal {
		if ok := services.ProbeService(s, services.Status{State: services.Running}, svcProbeTimeout); ok != nil {
			msg.probes[svcKey(svcSecLocal, s.Name)] = *ok
		}
	}
	return msg
}

// svcConfigCache keeps the parsed `compose config` until a compose file
// or .env changes (config is the slow call).
type svcConfigCache struct {
	mu  sync.Mutex
	key string
	cfg services.Config
}

func (c *svcConfigCache) load(ctx context.Context, comp *services.Compose, root string) (services.Config, services.Problem) {
	var b strings.Builder
	for _, f := range append(append([]string{}, comp.Files...), ".env") {
		if !filepath.IsAbs(f) {
			f = filepath.Join(root, f)
		}
		if fi, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", f, fi.ModTime().UnixNano(), fi.Size())
		}
	}
	fmt.Fprintf(&b, "%v", comp.Profiles)
	key := b.String()
	c.mu.Lock()
	if c.key == key && c.key != "" {
		cfg := c.cfg
		c.mu.Unlock()
		return cfg, services.Problem{}
	}
	c.mu.Unlock()
	cfg, p := comp.Load(ctx)
	if p.Kind == services.ProblemNone {
		c.mu.Lock()
		c.key, c.cfg = key, cfg
		c.mu.Unlock()
	}
	return cfg, p
}

// onServicesSnapshot applies a refresh result.
func (m *Model) onServicesSnapshot(msg servicesSnapshotMsg) tea.Cmd {
	st := m.services
	if st == nil || msg.seq != st.reqSeq || msg.root != st.root {
		return nil
	}
	st.inflight = false
	st.loaded = true
	st.doneSeq = msg.seq
	st.det = msg.det
	st.local = msg.det.Local
	st.problem = msg.problem
	if msg.det.Compose && msg.problem.Kind == services.ProblemNone {
		st.project = msg.cfg.Project
		st.compose = msg.cfg.Services
		st.status = msg.status
	} else if !msg.det.Compose {
		st.project, st.compose, st.status = "", nil, map[string]services.Status{}
	}
	// A problem keeps the last rows (dimmed by statusOf → Unknown).
	st.probes = msg.probes
	st.at = msg.at
	st.stale = false
	var cmds []tea.Cmd
	// Starting / restarting rows settle when ps shows a result.
	for k, b := range st.busy {
		section, name, _ := strings.Cut(k, "/")
		if section != svcSecCompose || b == services.Stopping {
			continue
		}
		if s, ok := st.status[name]; ok && (s.State.Up() || s.State == services.Exited || (b == services.Restarting && s.State != services.Restarting)) {
			delete(st.busy, k)
		}
	}
	// A container that exited non-zero gets one toast.
	for _, s := range st.compose {
		ss := st.status[s.Name]
		if ss.State == services.Exited && ss.ExitCode != 0 && st.prevUp[s.Name] && st.exitSeen[s.Name] != ss.Text {
			st.exitSeen[s.Name] = ss.Text
			cmds = append(cmds, m.servicesExitToast(s.Name, ss.ExitCode))
		}
	}
	st.prevUp = map[string]bool{}
	for name, s := range st.status {
		if s.State.Up() || s.State == services.Starting || s.State == services.Restarting {
			st.prevUp[name] = true
		}
	}
	m.servicesClampCursor()
	st.touch()
	if st.visible {
		cmds = append(cmds, m.startHostWatch())
	}
	waiters := st.waiters
	st.waiters = nil
	for _, w := range waiters {
		cmds = append(cmds, w(m))
	}
	if st.pending {
		st.pending = false
		cmds = append(cmds, m.servicesRefresh())
	}
	return tea.Batch(cmds...)
}

// servicesExitToast is the "Service "api" exited with code 1" toast.
func (m *Model) servicesExitToast(name string, code int) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, fmt.Sprintf("Service %q exited with code %d", name, code), "l logs · r restart")
	return c
}

// ── Events ───────────────────────────────────────────────────────────────

type servicesEventMsg struct {
	gen    uint64
	event  services.Event
	closed bool
}

// waitServicesEvent blocks on the watcher's channel (one goroutine, no
// CPU) and turns the next event into a message.
func waitServicesEvent(ctx context.Context, ch <-chan services.Event, gen uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-ch:
			return servicesEventMsg{gen: gen, event: event, closed: !ok}
		case <-ctx.Done():
			return servicesEventMsg{gen: gen, closed: true}
		}
	}
}

func (m *Model) onServicesEvent(msg servicesEventMsg) tea.Cmd {
	st := m.services
	if st == nil || msg.gen != st.watchGen {
		return nil
	}
	if msg.closed { // docker events ended (daemon stopped?): read once more
		st.stopWatch()
		return tea.Batch(m.servicesRefresh(), m.startHostWatch())
	}
	var cmds []tea.Cmd
	if msg.event.Type == "container" || msg.event.Type == "" {
		if st.sectionShown("project") {
			cmds = append(cmds, m.debounce.Do(svcRefreshKey, svcRefreshDebounce, func() tea.Msg { return servicesRefreshMsg{} }))
		}
		if st.sectionShown("project") && st.det.Compose {
			cmds = append(cmds, m.containersRefresh("containers"))
		}
	}
	switch msg.event.Type {
	case "image":
		cmds = append(cmds, m.containersRefresh("images"))
	case "network":
		cmds = append(cmds, m.containersRefresh("networks"))
	case "volume":
		cmds = append(cmds, m.containersRefresh("volumes"))
	}
	cmds = append(cmds, m.servicesWaitNext(msg.gen))
	return tea.Batch(cmds...)
}

// servicesWaitNext keeps reading the same watcher. The channel lives in
// the closure of the first wait; a second reader is a new closure over
// the same channel.
func (m *Model) servicesWaitNext(gen uint64) tea.Cmd {
	st := m.services
	if st.watchCh == nil {
		return nil
	}
	return waitServicesEvent(st.watchCtx, st.watchCh, gen)
}

// handleServicesMsg routes the services messages. ok=false: not ours.
func (m *Model) handleServicesMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case servicesSnapshotMsg:
		return m.onServicesSnapshot(msg), true
	case containerHostMsg:
		return m.onContainerHost(msg), true
	case containerActionDoneMsg:
		return m.onContainerActionDone(msg), true
	case containerInspectMsg:
		return m.onContainerInspect(msg), true
	case containerGroupDoneMsg:
		return m.onContainerGroupDone(msg), true
	case imagePageMsg:
		return m.onImagePage(msg), true
	case hostActionDoneMsg:
		return m.onHostActionDone(msg), true
	case diskUsageMsg:
		return m.onDiskUsage(msg), true
	case storageInspectMsg:
		return m.onStorageInspect(msg), true
	case storagePrunePreviewMsg:
		return m.onStoragePrunePreview(msg), true
	case containerEventMsg:
		return m.onContainerEvent(msg), true
	case servicesRefreshMsg:
		return m.servicesRefresh(), true
	case servicesEventMsg:
		return m.onServicesEvent(msg), true
	case servicesActionDoneMsg:
		return m.onServicesActionDone(msg), true
	case servicesQuitMsg:
		return m.quitNow(), true
	}
	return nil, false
}

// ── Session ──────────────────────────────────────────────────────────────

// servicesLoadSession reads the project's saved services state.
func (m *Model) servicesLoadSession() {
	st := m.services
	if st.sessLoaded {
		return
	}
	st.sessLoaded = true
	sess := loadSession()
	st.applyFoldKeys(sess.ContainersCollapsed)
	st.applySectionChoice(loadSettings().ContainersSections)
	st.aiAllowed.Store(sess.ServicesAIAllowed)
	for _, n := range sess.ServicesStarted {
		st.startedByUs[n] = true
	}
	for _, h := range sess.ServicesLocalOK {
		st.localOK[h] = true
	}
}

// servicesSaveSession writes the project's services state.
func (m *Model) servicesSaveSession() {
	st := m.services
	sess := loadSession()
	sess.ServicesAIAllowed = st.aiAllowed.Load()
	sess.ServicesStarted = sortedTrue(st.startedByUs)
	sess.ServicesLocalOK = sortedTrue(st.localOK)
	sess.ContainersCollapsed = st.foldKeys()
	saveSession(sess)
}

func sortedTrue(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// onServiceJobExit handles the end of a Services job tab: the `up` tab,
// a local service, a logs or shell tab.
func (m *Model) onServiceJobExit(key string, code int) tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	var cmds []tea.Cmd
	switch {
	case key == svcTabUp:
		for k, b := range st.busy {
			if strings.HasPrefix(k, svcSecCompose+"/") && b == services.Starting {
				delete(st.busy, k)
			}
		}
		if code != 0 {
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Errr, "docker compose up failed", fmt.Sprintf("exit code %d · see the services: up tab", code))
			cmds = append(cmds, c)
		}
	case strings.HasPrefix(key, "local:"):
		name := strings.TrimPrefix(key, "local:")
		delete(st.busy, svcKey(svcSecLocal, name))
		state := services.Stopped
		if code != 0 && !st.localStop[name] {
			state = services.Exited
			cmds = append(cmds, m.servicesLocalExitToast(name, code))
		}
		st.localSt[name] = services.Status{Name: name, State: state, ExitCode: code,
			Text: fmt.Sprintf("Exited (%d)", code)}
		delete(st.localStop, name)
	case strings.HasPrefix(key, "container:compose:"):
		project := strings.TrimPrefix(key, "container:compose:")
		for _, c := range st.containerRows {
			if c.Project == project {
				delete(st.busy, containerBusyPrefix+c.ID)
			}
		}
		if code != 0 {
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Errr, "Project command failed", fmt.Sprintf("exit code %d · see the compose terminal tab", code))
			cmds = append(cmds, c)
		}
		cmds = append(cmds, m.containersRefresh("containers"))
	case strings.HasPrefix(key, containerTabPrefix):
		// A logs, shell, attach, or stats tab ended. The event stream will
		// normally refresh state; this also covers an engine without events.
		cmds = append(cmds, m.containersRefresh("containers"))
	default:
		return nil
	}
	st.touch()
	cmds = append(cmds, m.servicesRefresh())
	return tea.Batch(cmds...)
}
