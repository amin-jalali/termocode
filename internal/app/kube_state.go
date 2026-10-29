package app

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/kube"
)

// ── Kubernetes view: state and loading (G-K8S) ───────────────────────────
//
// A read-only view of one cluster through kubectl: pods, deployments,
// services and jobs of one namespace. It lives under the Containers icon
// (activity.ViewKubernetes) and opens from the palette (Kubernetes: Show).
//
// Loading: one load (the four lists) when the view shows, then one every
// kubeInterval while the view is on screen and the window is in the
// foreground; a failed load waits longer (kubeBackoff, doubling up to
// kubeMaxBackoff). Hiding the view or a blurred window ends the chain and
// cancels a call in flight: no timer, no process.
//
// The session's context and namespace are passed as flags; kubeconfig is
// never written.

const (
	kubeInterval   = 5 * time.Second
	kubeBackoff    = 10 * time.Second
	kubeMaxBackoff = 60 * time.Second
)

// kubeSections are the list sections, in order.
var kubeSections = []string{"pods", "deployments", "services", "jobs"}

type kubeState struct {
	visible  bool
	detected bool   // kubectl was looked for
	binary   string // kubectl's path ("" = not installed)

	context   string // the session's context ("" until known)
	namespace string // the session's namespace ("" until known)

	pods     []kube.Pod
	deploys  []kube.Deployment
	services []kube.Service
	jobs     []kube.Job
	loaded   bool              // one load came back
	err      string            // why the whole load failed ("" = it worked)
	errs     map[string]string // per section (forbidden, …)
	loadedAt time.Time

	gen     uint64 // a load or tick of an older gen is dropped
	polling bool   // a load or a tick of the current gen is outstanding
	loading bool   // a load runs now
	cancel  context.CancelFunc
	delay   time.Duration // the wait before the next load
	off     bool          // a demo: never call kubectl

	cursor       int
	collapsed    map[string]bool
	filter       string
	filterTyping bool

	forwards map[string]*kube.Forward // by pod name

	seq    uint64 // bumped on every change (render cache)
	render struct {
		sync.Mutex
		key, out string
	}
}

func newKubeState() *kubeState {
	return &kubeState{collapsed: map[string]bool{}, errs: map[string]string{}, forwards: map[string]*kube.Forward{}}
}

func (st *kubeState) touch() { st.seq++ }

// client is kubectl for the session's context and namespace.
func (st *kubeState) client() kube.Client {
	return kube.Client{Binary: st.binary, Context: st.context, Namespace: st.namespace, X: servicesExec}
}

// kubeVisible: the Kubernetes view is on screen.
func (m *Model) kubeVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewKubernetes
}

// revealKube shows the view (Kubernetes: Show) and focuses it.
func (m *Model) revealKube() tea.Cmd {
	m.activity.SetActive(activity.ViewKubernetes)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	return m.kubeSync()
}

// kubeWanted: should loads run right now?
func (m *Model) kubeWanted() bool {
	st := m.kube
	return st != nil && st.visible && !m.termBlurred && !st.off
}

// kubeSync follows the view's visibility and the window focus: shown →
// load now and every kubeInterval; hidden → stop. Called after every
// update; cheap when nothing changed.
func (m *Model) kubeSync() tea.Cmd {
	st := m.kube
	if st == nil {
		return nil
	}
	if vis := m.kubeVisible(); vis != st.visible {
		st.visible = vis
		st.touch()
	}
	want := m.kubeWanted()
	switch {
	case want && !st.polling:
		st.polling = true
		st.gen++
		return m.kubeLoad()
	case !want && st.polling:
		st.stopPolling()
	}
	return nil
}

// stopPolling ends the chain: a late load or tick of this gen is dropped.
func (st *kubeState) stopPolling() {
	st.polling, st.loading = false, false
	st.gen++
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
}

type kubeLoadMsg struct {
	gen       uint64
	binary    string
	context   string
	namespace string
	pods      []kube.Pod
	deploys   []kube.Deployment
	services  []kube.Service
	jobs      []kube.Job
	err       error            // the whole load failed
	errs      map[string]error // one section failed
}

type kubeTickMsg struct{ gen uint64 }

// kubeLoad finds kubectl (first time), the context and namespace (when
// not known yet) and lists the four sections in parallel.
func (m *Model) kubeLoad() tea.Cmd {
	st := m.kube
	if st.cancel != nil {
		st.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	st.loading = true
	st.touch()
	gen := st.gen
	c := st.client()
	detected := st.detected
	return func() tea.Msg {
		defer cancel()
		msg := kubeLoadMsg{gen: gen, errs: map[string]error{}}
		if !detected {
			c.Binary = kube.Find(nil)
		}
		msg.binary = c.Binary
		if c.Binary == "" {
			msg.err = &kube.Error{Kind: kube.ErrNoKubectl}
			return msg
		}
		if c.Context == "" {
			cur, err := c.CurrentContext(ctx)
			if err != nil {
				msg.err = err
				return msg
			}
			c.Context = cur
		}
		if c.Namespace == "" {
			ns, err := c.ContextNamespace(ctx)
			if err != nil {
				msg.err = err
				return msg
			}
			c.Namespace = ns
		}
		msg.context, msg.namespace = c.Context, c.Namespace
		var wg sync.WaitGroup
		var mu sync.Mutex
		set := func(sec string, err error) {
			if err != nil {
				mu.Lock()
				msg.errs[sec] = err
				mu.Unlock()
			}
		}
		wg.Add(4)
		go func() { defer wg.Done(); var err error; msg.pods, err = c.Pods(ctx); set("pods", err) }()
		go func() { defer wg.Done(); var err error; msg.deploys, err = c.Deployments(ctx); set("deployments", err) }()
		go func() { defer wg.Done(); var err error; msg.services, err = c.Services(ctx); set("services", err) }()
		go func() { defer wg.Done(); var err error; msg.jobs, err = c.Jobs(ctx); set("jobs", err) }()
		wg.Wait()
		// The cluster itself does not answer: one line, not four.
		if len(msg.errs) == len(kubeSections) {
			for _, err := range msg.errs {
				if k := kube.KindOf(err); k == kube.ErrNoCluster || k == kube.ErrTimeout || k == kube.ErrNoConfig {
					msg.err = err
				}
			}
		}
		return msg
	}
}

func (m *Model) onKubeLoad(msg kubeLoadMsg) tea.Cmd {
	st := m.kube
	if st == nil || msg.gen != st.gen || !st.polling {
		return nil
	}
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.loading = false
	st.detected, st.binary = true, msg.binary
	if msg.binary == "" {
		// No kubectl: nothing to poll. R looks again.
		st.polling = false
		st.loaded, st.err = true, msg.err.Error()
		st.touch()
		return nil
	}
	if msg.context != "" {
		st.context = msg.context
	}
	if msg.namespace != "" {
		st.namespace = msg.namespace
	}
	st.loaded = true
	if msg.err != nil {
		st.err = msg.err.Error()
		st.delay = nextKubeBackoff(st.delay)
		st.touch()
		return kubeTick(st.gen, st.delay)
	}
	st.err = ""
	st.pods, st.deploys, st.services, st.jobs = msg.pods, msg.deploys, msg.services, msg.jobs
	st.errs = map[string]string{}
	for sec, err := range msg.errs {
		st.errs[sec] = err.Error()
	}
	st.loadedAt = time.Now()
	st.delay = kubeInterval
	if len(msg.errs) > 0 {
		st.delay = nextKubeBackoff(st.delay)
	}
	st.pruneForwards()
	st.clampCursor()
	st.touch()
	return kubeTick(st.gen, st.delay)
}

// nextKubeBackoff is the wait after a failed load: 10 s, then double, at
// most a minute.
func nextKubeBackoff(d time.Duration) time.Duration {
	if d < kubeBackoff {
		return kubeBackoff
	}
	if d *= 2; d > kubeMaxBackoff {
		return kubeMaxBackoff
	}
	return d
}

func kubeTick(gen uint64, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return kubeTickMsg{gen: gen} })
}

func (m *Model) onKubeTick(msg kubeTickMsg) tea.Cmd {
	st := m.kube
	if st == nil || msg.gen != st.gen || !st.polling {
		return nil
	}
	if !m.kubeWanted() {
		st.stopPolling()
		return nil
	}
	return m.kubeLoad()
}

// kubeReload is R: look for kubectl again when it was missing, load now.
func (m *Model) kubeReload() tea.Cmd {
	st := m.kube
	if st == nil {
		return nil
	}
	if st.binary == "" {
		st.detected = false
	}
	st.stopPolling()
	st.delay = 0
	return m.kubeSync()
}

// kubeRescope starts over in another context or namespace: the old rows
// go (they belong to the old scope) and a load starts now.
func (m *Model) kubeRescope(context, namespace string) tea.Cmd {
	st := m.kube
	st.context, st.namespace = context, namespace
	st.pods, st.deploys, st.services, st.jobs = nil, nil, nil, nil
	st.errs = map[string]string{}
	st.err, st.loaded = "", false
	st.cursor = 0
	return m.kubeReload()
}

// handleKubeMsg routes the Kubernetes messages. ok=false: not ours.
func (m *Model) handleKubeMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case kubeLoadMsg:
		return m.onKubeLoad(msg), true
	case kubeTickMsg:
		return m.onKubeTick(msg), true
	case kubePickMsg:
		return m.onKubePick(msg), true
	}
	return nil, false
}

// shutdown is the quit path: no kubectl may outlive termocode.
func (st *kubeState) shutdown() {
	if st == nil {
		return
	}
	st.stopPolling()
	for name, fw := range st.forwards {
		fw.Stop()
		delete(st.forwards, name)
	}
}

// pruneForwards drops forwards whose kubectl ended (the pod went away).
func (st *kubeState) pruneForwards() {
	for name, fw := range st.forwards {
		select {
		case <-fw.Done():
			delete(st.forwards, name)
		default:
		}
	}
}

// clampCursor keeps the cursor on a row.
func (st *kubeState) clampCursor() {
	n := len(st.rows())
	if st.cursor >= n {
		st.cursor = n - 1
	}
	if st.cursor < 0 {
		st.cursor = 0
	}
}
