package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

type containerEventMsg struct {
	gen    uint64
	event  containers.Event
	closed bool
}

func (st *servicesState) stopHostWatch() {
	if st.hostWatchStop != nil {
		stop := st.hostWatchStop
		st.hostWatchStop, st.hostWatchCh = nil, nil
		if st.hostWatchCancel != nil {
			st.hostWatchCancel()
			st.hostWatchCancel = nil
		}
		st.hostWatchCtx = nil
		st.hostWatchGen++
		stop()
	}
}

func (m *Model) startHostWatch() tea.Cmd {
	st := m.services
	if st == nil || !st.visible || st.watchStop != nil || st.hostWatchStop != nil {
		return nil
	}
	if st.containerEngine.Reachable {
		ctx, cancel := context.WithCancel(context.Background())
		ch, stop, err := st.containerEngine.Watch(ctx)
		if err != nil {
			cancel()
			return nil
		}
		st.hostWatchStop, st.hostWatchCh, st.hostWatchCancel = stop, ch, cancel
		st.hostWatchCtx = ctx
		st.hostWatchGen++
		return waitContainerEvent(ctx, ch, st.hostWatchGen)
	}
	if st.det.Compose && st.problem.Kind == services.ProblemNone {
		if c := st.composeBackend(); c != nil {
			ctx, cancel := context.WithCancel(context.Background())
			ch, stop, err := c.Watch(ctx, "")
			if err == nil {
				st.watchStop, st.watchCh, st.watchCtx, st.watchCancel = stop, ch, ctx, cancel
				st.watchGen++
				return waitServicesEvent(ctx, ch, st.watchGen)
			}
			cancel()
		}
	}
	return nil
}

func waitContainerEvent(ctx context.Context, ch <-chan containers.Event, gen uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-ch:
			return containerEventMsg{gen: gen, event: ev, closed: !ok}
		case <-ctx.Done():
			return containerEventMsg{gen: gen, closed: true}
		}
	}
}

func (m *Model) onContainerEvent(msg containerEventMsg) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || msg.gen != st.hostWatchGen {
		return nil
	}
	if msg.closed {
		st.stopHostWatch()
		return tea.Batch(m.containersReloadAll(), m.startHostWatch())
	}
	var cmds []tea.Cmd
	if msg.event.Type == "container" || msg.event.Type == "" {
		cmds = append(cmds, m.containersRefresh("containers"))
		if st.sectionShown("project") {
			cmds = append(cmds, m.servicesRefresh())
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
	return tea.Batch(append(cmds, waitContainerEvent(st.hostWatchCtx, st.hostWatchCh, msg.gen))...)
}

// ── Containers view: the host part (state and loading) ───────────────────
//
// The Containers view is the Services view (its PROJECT section) plus the
// container host: CONTAINERS, IMAGES, REGISTRIES, NETWORKS, VOLUMES,
// CONTEXTS and HELP. Host data loads only while the view is on screen and
// only for open sections: no timers, and hiding the view cancels every
// engine call (perf guard).

// containerSectionOrder is the order of the sections; "project" is the
// Services part.
var containerSectionOrder = []string{"project", "containers", "images", "registries", "networks", "volumes", "contexts", "help"}

// hostSectionOrder is containerSectionOrder without "project".
var hostSectionOrder = containerSectionOrder[1:]

// engineSections need the container engine; REGISTRIES reads a file and
// HELP is static.
var engineSections = []string{"containers", "images", "networks", "volumes", "contexts"}

func isEngineSection(section string) bool {
	for _, s := range engineSections {
		if s == section {
			return true
		}
	}
	return false
}

func isContainerSection(section string) bool {
	for _, s := range containerSectionOrder {
		if s == section {
			return true
		}
	}
	return false
}

// defaultHostSectionCollapsed: CONTAINERS starts open, the rest folded
// (less noise, less engine work).
func defaultHostSectionCollapsed(name string) bool { return name != "containers" }

type containerHostMsg struct {
	gen        uint64
	engine     containers.Engine
	section    string   // "detect" or one section
	loaded     []string // detect: the sections it listed too
	containers []containers.Container
	images     []containers.Image
	networks   []containers.Network
	volumes    []containers.Volume
	contexts   []containers.Context
	registries []containers.Registry
	err        string
	errs       map[string]string
}

func registryConfigPath() string {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".docker", "config.json")
}

// sectionShown: the ⋯ menu did not hide it (missing keys are shown).
func (st *servicesState) sectionShown(section string) bool {
	if shown, ok := st.sections[section]; ok && !shown {
		return false
	}
	return true
}

// hostSectionVisible: shown and unfolded, so its rows are on the list.
func (st *servicesState) hostSectionVisible(section string) bool {
	return st.sectionShown(section) && !st.collapsed[hostSection(section)]
}

// hostPartShown: at least one host section with data is shown.
func (st *servicesState) hostPartShown() bool {
	for _, s := range hostSectionOrder {
		if s != "help" && st.sectionShown(s) {
			return true
		}
	}
	return false
}

// engineAbsent: detection ran and found no docker / podman.
func (st *servicesState) engineAbsent() bool {
	return st.containerDetected && st.containerEngine.Binary == ""
}

// cancelHostWork stops every engine call of the view (it was hidden).
func (st *servicesState) cancelHostWork() {
	if st.containerCancel != nil {
		st.containerCancel()
		st.containerCancel = nil
	}
	for key, cancel := range st.containerCancels {
		cancel()
		delete(st.containerCancels, key)
	}
	st.containerGen++
	st.containerDetecting = false
}

// activeContext is the engine's current context name ("" = unknown or
// podman).
func (st *servicesState) activeContext() string {
	for _, c := range st.contextRows {
		if c.Current {
			return c.Name
		}
	}
	if st.containerEngine.Kind == "docker" {
		return os.Getenv("DOCKER_CONTEXT")
	}
	return ""
}

// containersDetect finds the engine once (docker, then podman) and lists
// the open sections in the same goroutine. The contexts are listed too,
// for the active context's name.
func (m *Model) containersDetect() tea.Cmd {
	return m.containersDetectWithWatch(false)
}

func (m *Model) containersDetectWithWatch(force bool) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || st.containerDetected || st.containerDetecting {
		return nil
	}
	var load []string
	for _, s := range engineSections {
		if st.hostSectionVisible(s) {
			load = append(load, s)
		}
	}
	if len(load) == 0 && !force {
		return nil
	}
	st.containerDetecting = true
	st.containerGen++
	gen := st.containerGen
	ctx, cancel := context.WithCancel(context.Background())
	st.containerCancel = cancel
	dir := st.root
	return func() tea.Msg {
		e := containers.Detect(ctx, containers.DetectOptions{Exec: servicesExec, Dir: dir, Android: runtime.GOOS == "android"})
		msg := containerHostMsg{gen: gen, engine: e, section: "detect", errs: map[string]string{}}
		if !e.Reachable {
			return msg
		}
		if e.Kind == "docker" && !containsString(load, "contexts") {
			load = append(load, "contexts")
		}
		for _, s := range load {
			var err error
			switch s {
			case "containers":
				msg.containers, err = e.ListContainers(ctx)
			case "images":
				msg.images, err = e.ListImages(ctx)
			case "networks":
				msg.networks, err = e.ListNetworks(ctx)
			case "volumes":
				msg.volumes, err = e.ListVolumes(ctx)
			case "contexts":
				msg.contexts, err = e.ListContexts(ctx)
			}
			if err != nil {
				msg.errs[s] = err.Error()
			}
			msg.loaded = append(msg.loaded, s)
		}
		return msg
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// containersRefresh lists one open section again (nil when the view is
// hidden, the section folded, or the engine not found yet).
func (m *Model) containersRefresh(section string) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || !st.hostSectionVisible(section) || section == "help" {
		return nil
	}
	if section != "registries" && (!st.containerDetected || !st.containerEngine.Reachable) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if old := st.containerCancels[section]; old != nil {
		old()
	}
	st.containerCancels[section] = cancel
	gen := st.containerGen
	e := st.containerEngine
	return func() tea.Msg {
		defer cancel()
		msg := containerHostMsg{gen: gen, engine: e, section: section}
		var err error
		switch section {
		case "containers":
			msg.containers, err = e.ListContainers(ctx)
		case "images":
			msg.images, err = e.ListImages(ctx)
		case "networks":
			msg.networks, err = e.ListNetworks(ctx)
		case "volumes":
			msg.volumes, err = e.ListVolumes(ctx)
		case "contexts":
			msg.contexts, err = e.ListContexts(ctx)
		case "registries":
			cfg, readErr := containers.ReadRegistryConfig(registryConfigPath())
			msg.registries = cfg.Registries
			if readErr != nil && !os.IsNotExist(readErr) {
				err = readErr
			}
		}
		if err != nil {
			msg.err = err.Error()
		}
		return msg
	}
}

// containersLoadVisible finds the engine (first time) and lists every
// open section.
func (m *Model) containersLoadVisible() tea.Cmd {
	cmds := []tea.Cmd{m.containersDetectWithWatch(true)}
	for _, s := range hostSectionOrder {
		cmds = append(cmds, m.containersRefresh(s))
	}
	return tea.Batch(cmds...)
}

// containersReloadAll is R: a missing or stopped engine is looked for
// again; every open section is listed again.
func (m *Model) containersReloadAll() tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	if st.containerDetected && !st.containerEngine.Reachable {
		st.containerDetected = false
		st.touch()
	}
	return m.containersLoadVisible()
}

// containersOpened loads a section that was just unfolded or shown.
func (m *Model) containersOpened(section string) tea.Cmd {
	return tea.Batch(m.containersDetect(), m.containersRefresh(section))
}

// onContainerHost takes one answer; after detection it lists the open
// sections the detection did not cover (opened while it ran).
func (m *Model) onContainerHost(msg containerHostMsg) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || msg.gen != st.containerGen {
		return nil
	}
	if msg.section == "detect" {
		st.containerDetecting, st.containerDetected = false, true
		st.containerEngine = msg.engine
		if st.containerCancel != nil {
			st.containerCancel()
			st.containerCancel = nil
		}
		for _, s := range msg.loaded {
			st.hostLoaded[s] = true
			if err := msg.errs[s]; err != "" {
				st.containerErrors[s] = err
			} else {
				delete(st.containerErrors, s)
			}
			switch s {
			case "containers":
				st.containerRows = msg.containers
			case "images":
				st.imageRows = msg.images
			case "networks":
				st.networkRows = msg.networks
			case "volumes":
				st.volumeRows = msg.volumes
			case "contexts":
				st.contextRows = msg.contexts
			}
		}
		st.touch()
		var cmds []tea.Cmd
		if st.containerEngine.Reachable && st.watchStop != nil {
			st.stopWatch()
		}
		if st.containerEngine.Reachable {
			cmds = append(cmds, m.startHostWatch())
		}
		for _, s := range engineSections {
			if st.hostSectionVisible(s) && !containsString(msg.loaded, s) {
				cmds = append(cmds, m.containersRefresh(s))
			}
		}
		return tea.Batch(cmds...)
	}
	if cancel := st.containerCancels[msg.section]; cancel != nil {
		cancel()
		delete(st.containerCancels, msg.section)
	}
	st.hostLoaded[msg.section] = true
	if msg.err != "" {
		st.containerErrors[msg.section] = msg.err
	} else {
		delete(st.containerErrors, msg.section)
	}
	switch msg.section {
	case "containers":
		st.containerRows = msg.containers
	case "images":
		st.imageRows = msg.images
	case "networks":
		st.networkRows = msg.networks
	case "volumes":
		st.volumeRows = msg.volumes
	case "contexts":
		st.contextRows = msg.contexts
	case "registries":
		st.registryRows = msg.registries
	}
	st.touch()
	return nil
}

// ── Saved choices ────────────────────────────────────────────────────────

// applySectionChoice reads containers_sections (config.json): empty =
// every section; "none" = the user hid them all.
func (st *servicesState) applySectionChoice(list []string) {
	st.sections = map[string]bool{}
	if len(list) == 0 {
		return
	}
	for _, name := range containerSectionOrder {
		st.sections[name] = false
	}
	for _, name := range list {
		if isContainerSection(name) {
			st.sections[name] = true
		}
	}
}

// sectionChoice is the containers_sections value for the current choice.
func (st *servicesState) sectionChoice() []string {
	var out []string
	for _, name := range containerSectionOrder {
		if st.sectionShown(name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		out = []string{"none"}
	}
	return out
}

// Fold keys in session.json: "images" = folded, "!containers" = open,
// "compose" / "local" the PROJECT parts, "grp/<section>/<name>" a folded
// group. Only values that differ from the default are kept.
func (st *servicesState) applyFoldKeys(keys []string) {
	st.collapsed[svcSecCompose], st.collapsed[svcSecLocal] = false, false
	for _, name := range hostSectionOrder {
		st.collapsed[hostSection(name)] = defaultHostSectionCollapsed(name)
	}
	for _, key := range keys {
		collapsed := true
		if strings.HasPrefix(key, "!") {
			collapsed, key = false, key[1:]
		}
		switch {
		case key == svcSecCompose || key == svcSecLocal:
			st.collapsed[key] = collapsed
		case strings.HasPrefix(key, "grp/"):
			sec, name, ok := strings.Cut(strings.TrimPrefix(key, "grp/"), "/")
			if ok && name != "" && isEngineSection(sec) && collapsed {
				st.collapsed[key] = true
			}
		case isContainerSection(key) && key != "project":
			st.collapsed[hostSection(key)] = collapsed
		}
	}
}

// foldKeys is the session list: non-default folds of known sections, and
// folded groups that still exist (or whose section has not loaded yet).
func (st *servicesState) foldKeys() []string {
	var out []string
	for _, key := range []string{svcSecCompose, svcSecLocal} {
		if st.collapsed[key] {
			out = append(out, key)
		}
	}
	for _, name := range hostSectionOrder {
		if c := st.collapsed[hostSection(name)]; c != defaultHostSectionCollapsed(name) {
			if c {
				out = append(out, name)
			} else {
				out = append(out, "!"+name)
			}
		}
	}
	live := st.hostGroupKeys()
	for key, c := range st.collapsed {
		if !c || !strings.HasPrefix(key, "grp/") {
			continue
		}
		sec, _, _ := strings.Cut(strings.TrimPrefix(key, "grp/"), "/")
		if live[key] || (isEngineSection(sec) && !st.hostLoaded[sec]) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
