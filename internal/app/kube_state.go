package app

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/crash"
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
	// back / forward places (kube_nav.go)
	navBack, navFwd []kubePlace

	pods     []kube.Pod
	deploys  []kube.Deployment
	services []kube.Service
	jobs     []kube.Job
	// the folded sections (kube_kinds.go): rows by section, and which
	// came back at least once in this scope
	extra       map[string][]kubeObj
	extraLoaded map[string]bool
	loaded      bool              // one load came back
	err         string            // why the whole load failed ("" = it worked)
	errs        map[string]string // per section (forbidden, …)
	loadedAt    time.Time

	gen     uint64 // a load or tick of an older gen is dropped
	polling bool   // a load or a tick of the current gen is outstanding
	loading bool   // a load runs now
	cancel  context.CancelFunc
	delay   time.Duration // the wait before the next load
	off     bool          // a demo: never call kubectl

	cursor       int
	lastClick    time.Time // a second click on the cursor row opens it
	collapsed    map[string]bool
	filter       string
	filterTyping bool

	pendingRestart kubeRestartTarget // the restart card's deployment (kube_restart.go)
	pendingScale   kubeScaleTarget   // the scale prompt's and card's deployment (kube_scale.go)
	pendingDelete  kubeDeleteTarget  // the delete card's object (kube_delete.go)
	revealSecret   string            // CTX-40: the secret whose key is being picked (kube_secret.go)
	pendingReveal  kubeRevealTarget  // CTX-40: the reveal card's secret and key
	revealedValue  string            // CTX-40: the value on screen; wiped when its card closes
	pendingLogs    kubeLogsTarget    // the save-logs prompt's pod (kube_logs.go)
	pendingUndo    kubeUndoTarget    // the rollback card's revision (kube_rollout.go)
	pendingImage   kubeImageTarget   // the set image prompt's and card's change
	imageOf        kube.Deployment   // the deployment the image prompt asks about
	lastRollout    kubeRolloutMsg    // the rollout picker's status and history
	pendingHelm    kubeHelmTarget    // the helm rollback / uninstall card (kube_helm.go)
	helmCurrent    int               // the release's revision now (rollback card)
	pendingApply   kubeApplyTarget   // the apply card's file (kube_apply.go)
	pendingNode    kubeNodeTarget    // the node card's node and action (kube_nodes.go)
	nodes          []kube.Node       // the node picker's rows (kube_nodes.go)
	nodesContext   string            // the context they came from
	pendingFile    kubeFileTarget    // the pod file / debug prompt's and card's pod (kube_podfiles.go)

	forwards map[string]*kube.Forward // by context/namespace/pod (kube_forward.go)

	top     map[string]kube.PodMetrics // the last `top pods`, by pod name (kube_top.go)
	topOff  bool                       // no metrics-server: not asked until R
	metrics *kube.MetricsHistory       // sparkline samples

	kind      *kube.APIResource  // the : kind shown after JOBS (kube_kind.go); nil = none
	kindObjs  []kube.Object      // its objects
	kindAll   bool               // in every namespace
	apiRes    []kube.APIResource // the last api-resources answer
	apiResCtx string             // of this context

	seq    uint64 // bumped on every change (render cache)
	render struct {
		sync.Mutex
		key, out string
	}
}

func newKubeState() *kubeState {
	st := &kubeState{collapsed: map[string]bool{}, errs: map[string]string{}, forwards: map[string]*kube.Forward{},
		extra: map[string][]kubeObj{}, extraLoaded: map[string]bool{}}
	for _, sec := range kubeExtraSections {
		st.collapsed[sec] = true // they load only once opened
	}
	return st
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
	extra     map[string][]kubeObj // the open folded sections that loaded
	top       []kube.PodMetrics    // kube_top.go
	topErr    error
	topAsked  bool
	kindRef   string // the kind this load listed ("" = none; kube_kind.go)
	kindAll   bool
	kindObjs  []kube.Object
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
	open := st.openExtras()
	askTop := !st.topOff
	var kind kube.APIResource
	kindAll := st.kindAll
	if st.kind != nil {
		kind = *st.kind
	}
	return func() tea.Msg {
		defer cancel()
		msg := kubeLoadMsg{gen: gen, errs: map[string]error{}, extra: map[string][]kubeObj{}}
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
		wg.Add(4 + len(open))
		now := time.Now()
		for _, sec := range open {
			crash.Go("kubernetes "+sec, func() {
				defer wg.Done()
				xs, err := kubeLoadExtra(ctx, c, sec, now)
				set(sec, err)
				if err == nil {
					mu.Lock()
					msg.extra[sec] = xs
					mu.Unlock()
				}
			})
		}
		crash.Go("kubernetes pods", func() { defer wg.Done(); var err error; msg.pods, err = c.Pods(ctx); set("pods", err) })
		crash.Go("kubernetes deployments", func() { defer wg.Done(); var err error; msg.deploys, err = c.Deployments(ctx); set("deployments", err) })
		crash.Go("kubernetes services", func() { defer wg.Done(); var err error; msg.services, err = c.Services(ctx); set("services", err) })
		crash.Go("kubernetes jobs", func() { defer wg.Done(); var err error; msg.jobs, err = c.Jobs(ctx); set("jobs", err) })
		wanted := len(kubeSections)
		if kind.Name != "" { // the : kind, a fifth section
			wanted++
			msg.kindRef, msg.kindAll = kind.Ref(), kindAll
			kc := c
			kc.AllNamespaces = kindAll
			wg.Add(1)
			crash.Go("kubernetes kind", func() {
				defer wg.Done()
				var err error
				msg.kindObjs, err = kc.Objects(ctx, kind)
				set(kubeKindSection, err)
			})
		}
		if askTop { // not a section: no metrics never fails the load
			msg.topAsked = true
			wg.Add(1)
			crash.Go("kubernetes top", func() { defer wg.Done(); msg.top, msg.topErr = c.TopPods(ctx) })
		}
		wg.Wait()
		// The cluster itself does not answer: one line, not four.
		if len(msg.errs) == wanted+len(open) {
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
	for sec, xs := range msg.extra {
		st.extra[sec], st.extraLoaded[sec] = xs, true
	}
	st.errs = map[string]string{}
	for sec, err := range msg.errs {
		st.errs[sec] = err.Error()
	}
	st.kindObjs = nil
	if st.kind != nil && msg.kindRef == st.kind.Ref() && msg.kindAll == st.kindAll {
		st.kindObjs = msg.kindObjs
	}
	if msg.topAsked {
		st.noteTop(msg.top, msg.topErr)
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
	st.topOff = false // R asks for metrics again
	st.stopPolling()
	st.delay = 0
	return m.kubeSync()
}

// kubeRescope starts over in another context or namespace: the old rows
// go (they belong to the old scope) and a load starts now.
func (m *Model) kubeRescope(context, namespace string) tea.Cmd {
	m.kube.pushPlace() // kube_nav.go
	return m.kubeGo(context, namespace)
}

// kubeGo is kubeRescope without a new history place (back / forward).
func (m *Model) kubeGo(context, namespace string) tea.Cmd {
	st := m.kube
	st.context, st.namespace = context, namespace
	st.pods, st.deploys, st.services, st.jobs = nil, nil, nil, nil
	st.extra, st.extraLoaded = map[string][]kubeObj{}, map[string]bool{}
	st.top = nil
	st.kindObjs = nil
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
	case kubeDescribeMsg:
		return m.onKubeDescribe(msg), true
	case kubeYAMLMsg:
		return m.onKubeYAML(msg), true
	case kubeEventsMsg:
		return m.onKubeEvents(msg), true
	case kubeForwardMsg:
		return m.onKubeForward(msg), true
	case kubeScaleMsg:
		return m.onKubeScale(msg), true
	case kubeRestartMsg:
		return m.onKubeRestart(msg), true
	case kubeDeleteMsg:
		return m.onKubeDelete(msg), true
	case kubeSecretValueMsg:
		return m.onKubeSecretValue(msg), true
	case kubeLogsSavedMsg:
		return m.onKubeLogsSaved(msg), true
	case kubeTopMsg:
		return m.onKubeTop(msg), true
	case kubeKindsMsg:
		return m.onKubeKinds(msg), true
	case kubeHelmListMsg:
		return m.onKubeHelmList(msg), true
	case kubeHelmPageMsg:
		return m.onKubeHelmPage(msg), true
	case kubeHelmHistoryMsg:
		return m.onKubeHelmHistory(msg), true
	case kubeHelmWriteMsg:
		return m.onKubeHelmWrite(msg), true
	case kubeRolloutMsg:
		return m.onKubeRollout(msg), true
	case kubeUndoMsg:
		return m.onKubeUndo(msg), true
	case kubeImageMsg:
		return m.onKubeImage(msg), true
	case kubeDiffMsg:
		return m.onKubeDiff(msg), true
	case kubeApplyMsg:
		return m.onKubeApply(msg), true
	case kubeNodesMsg:
		return m.onKubeNodes(msg), true
	case kubeNodeActionMsg:
		return m.onKubeNodeAction(msg), true
	case kubePageMsg:
		return m.onKubePage(msg), true
	case kubeEditMsg:
		return m.onKubeEdit(msg), true
	case kubePodDirMsg:
		return m.onKubePodDir(msg), true
	case kubeCopyMsg:
		return m.onKubeCopy(msg), true
	case kubeHelmMsg:
		return m.onKubeHelm(msg), true
	case kubeAddClusterMsg:
		return m.onKubeAddCluster(msg), true
	case kubeKustomizeMsg:
		return m.onKubeKustomize(msg), true
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
