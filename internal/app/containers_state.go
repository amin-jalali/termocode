package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

type containerEventMsg struct {
	gen    uint64
	events []containers.Event // one burst: the first event and those already waiting
	closed bool
}

// maxEventBurst caps the events taken in one message.
const maxEventBurst = 256

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
	// While a detection runs (after a context switch) containerEngine is
	// the old engine: a watcher started now would watch the old context
	// and block the new one. The detect's answer starts the watcher.
	if st.containerDetecting && st.containerEngine.Reachable {
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
			if !ok {
				return containerEventMsg{gen: gen, closed: true}
			}
			// Take the events already waiting too: a burst (compose
			// up of many services) lists the sections once.
			msg := containerEventMsg{gen: gen, events: []containers.Event{ev}}
			for len(msg.events) < maxEventBurst {
				select {
				case ev, ok := <-ch:
					if !ok {
						return msg // the next wait reports the end
					}
					msg.events = append(msg.events, ev)
				default:
					return msg
				}
			}
			return msg
		case <-ctx.Done():
			return containerEventMsg{gen: gen, closed: true}
		}
	}
}

// containerEventKinds are the object kinds a burst touched (an event
// without a type counts as a container event).
func containerEventKinds(events []containers.Event) map[string]bool {
	kinds := map[string]bool{}
	for _, ev := range events {
		k := ev.Type
		if k == "" {
			k = "container"
		}
		kinds[k] = true
	}
	return kinds
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
	kinds := containerEventKinds(msg.events)
	var cmds []tea.Cmd
	if kinds["container"] {
		cmds = append(cmds, m.containersRefresh("containers"))
		if st.sectionShown("project") {
			cmds = append(cmds, m.servicesRefresh())
		}
	}
	if kinds["image"] {
		if !st.hostSectionVisible("images") && st.anyRegistryExpanded() {
			cmds = append(cmds, m.containersList("images"))
		} else {
			cmds = append(cmds, m.containersRefresh("images"))
		}
	}
	if kinds["network"] {
		st.clearStorageInspect("network")
		cmds = append(cmds, m.containersRefresh("networks"))
	}
	if kinds["volume"] {
		st.clearStorageInspect("volume")
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

// registryConfigPath is the engine's login file: $DOCKER_CONFIG or
// ~/.docker for docker; $REGISTRY_AUTH_FILE or
// $XDG_RUNTIME_DIR/containers/auth.json for podman. It is only read.
func registryConfigPath(kind string) string {
	if kind == "podman" {
		if f := os.Getenv("REGISTRY_AUTH_FILE"); f != "" {
			return f
		}
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			return filepath.Join(dir, "containers", "auth.json")
		}
	}
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
		if strings.HasPrefix(key, "item/") {
			delete(st.storageInspectState, key)
		}
		delete(st.containerCancels, key)
	}
	st.containerGen++
	st.containerDetecting = false
	st.stopRemoteLoading()
}

// activeContext is the engine's current context (podman: connection)
// name ("" = unknown, or podman on its local socket).
func (st *servicesState) activeContext() string {
	if st.sessionContext != "" {
		return st.sessionContext
	}
	if st.isPodman() {
		// On Linux plain podman runs locally unless CONTAINER_CONNECTION
		// (or --remote) says otherwise; on macOS and Windows it is always
		// a client of its default connection.
		if env := os.Getenv("CONTAINER_CONNECTION"); env != "" {
			return env
		}
		if runtime.GOOS == "linux" {
			return ""
		}
	}
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

// hasContexts: the engine has contexts (docker) or connections (podman).
func (st *servicesState) hasContexts() bool {
	k := st.containerEngine.Kind
	return k == "docker" || k == "podman"
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
	preferred := preferredContainerEngine()
	st.containerPreference = preferred
	sessionContext := st.sessionContext
	return func() tea.Msg {
		e := containers.Detect(ctx, containers.DetectOptions{Preferred: preferred, Context: sessionContext, Exec: servicesExec, Dir: dir, Android: runtime.GOOS == "android"})
		msg := containerHostMsg{gen: gen, engine: e, section: "detect", errs: map[string]string{}}
		// Contexts are files, not daemon answers: list them even when
		// the engine does not answer, so a broken remote context can be
		// switched away from. The engine answers: the contexts and the
		// open sections are listed at the same time (one engine call's
		// wait instead of one per section).
		jobs := []string{}
		if e.Kind == "docker" || e.Kind == "podman" {
			jobs = append(jobs, "contexts")
		}
		if e.Reachable {
			for _, s := range load {
				if s != "contexts" {
					jobs = append(jobs, s)
				}
			}
		}
		errs := make([]error, len(jobs))
		var wg sync.WaitGroup
		for i, s := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				switch s {
				case "containers":
					msg.containers, errs[i] = e.ListContainers(ctx)
				case "images":
					msg.images, errs[i] = e.ListImages(ctx)
				case "networks":
					msg.networks, errs[i] = e.ListNetworks(ctx)
				case "volumes":
					msg.volumes, errs[i] = e.ListVolumes(ctx)
				case "contexts":
					msg.contexts, errs[i] = e.ListContexts(ctx)
				}
			}()
		}
		wg.Wait()
		for i, s := range jobs {
			if errs[i] != nil {
				msg.errs[s] = errs[i].Error()
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
	if st == nil || !st.visible || !st.hostSectionVisible(section) {
		return nil
	}
	return m.containersList(section)
}

// containersList lists one section even when it is folded (REGISTRIES
// shows the images of an open host). nil when the view is hidden or the
// engine is not found yet.
func (m *Model) containersList(section string) tea.Cmd {
	st := m.services
	if st == nil || !st.visible || section == "help" {
		return nil
	}
	if section == "contexts" && !st.hasContexts() {
		return nil
	}
	if section != "registries" && (!st.containerDetected || (!st.containerEngine.Reachable && section != "contexts")) {
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
			path := registryConfigPath(e.Kind)
			cfg, readErr := containers.ReadRegistryConfig(path)
			msg.registries = cfg.Registries
			if readErr != nil && !os.IsNotExist(readErr) {
				// The decoder's words name no stored value.
				err = fmt.Errorf("cannot read %s: %v", filepath.Base(path), readErr)
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
	cmds = append(cmds, m.containersRefreshContexts())
	return tea.Batch(cmds...)
}

// containersReloadAll is R: a missing or stopped engine is looked for
// again; every open section is listed again.
func (m *Model) containersReloadAll() tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	if st.containerDetected && st.containerPreference != preferredContainerEngine() {
		// containers_engine changed: forget the engine and what it listed.
		st.cancelHostWork()
		st.stopHostWatch()
		st.resetHostSnapshot(false)
	}
	if st.containerDetected && !st.containerEngine.Reachable {
		st.containerDetected = false
		st.touch()
	}
	return m.containersLoadVisible()
}

// containersRefreshContexts lists the contexts again even when the
// section is folded: the list decides whether CONTAINERS shows the
// section at all, and `docker context ls` costs no daemon call.
func (m *Model) containersRefreshContexts() tea.Cmd {
	st := m.services
	if st == nil || !st.visible || !st.sectionShown("contexts") || !st.containerDetected || !st.hasContexts() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if old := st.containerCancels["contexts"]; old != nil {
		old()
	}
	st.containerCancels["contexts"] = cancel
	gen := st.containerGen
	e := st.containerEngine
	return func() tea.Msg {
		defer cancel()
		msg := containerHostMsg{gen: gen, engine: e, section: "contexts"}
		var err error
		msg.contexts, err = e.ListContexts(ctx)
		if err != nil {
			msg.err = err.Error()
		}
		return msg
	}
}

// preferredContainerEngine reads containers_engine: "docker", "podman"
// or "auto" (also for a missing or unknown value).
func preferredContainerEngine() string {
	switch v := strings.ToLower(strings.TrimSpace(loadSettings().ContainersEngine)); v {
	case "docker", "podman":
		return v
	}
	return "auto"
}

// resetHostSnapshot forgets the engine and every host listing, so a new
// engine or context starts from "Loading…" and never shows rows of the
// old one. The contexts list is kept: it comes from files.
func (st *servicesState) resetHostSnapshot(keepEngine bool) {
	st.containerDetected, st.containerDetecting = false, false
	st.containerRows, st.imageRows, st.networkRows, st.volumeRows = nil, nil, nil, nil
	for _, sec := range engineSections {
		if sec != "contexts" {
			delete(st.hostLoaded, sec)
		}
		delete(st.containerErrors, sec)
	}
	for k := range st.busy {
		if strings.HasPrefix(k, containerBusyPrefix) {
			delete(st.busy, k)
		}
	}
	st.storageInspectState = map[string]string{}
	st.storageUsers = map[string][]string{}
	if !keepEngine {
		st.containerEngine = containers.Engine{}
	}
	st.touch()
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
		if st.registryWantImages {
			// A registry host was opened before the engine was found.
			st.registryWantImages = false
			if st.containerEngine.Reachable && !containsString(msg.loaded, "images") && !st.hostSectionVisible("images") {
				cmds = append(cmds, m.containersList("images"))
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
