package app

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Project services from the page (WEBP-4): the laptop's SERVICES view's
// list (m.services) — Docker Compose services and local dev commands —
// with their state. The page names a service; the laptop runs its own
// actions (servicesStart, servicesRestart, servicesStop): compose up in
// its "services: up" tab, a local command in its own terminal tab, and a
// local command the laptop never ran asks there first. Stop waits for a
// laptop card. Logs pass the same redaction as the AI's. A listening
// browser loads the list once; Refresh (and every action) reads it again.

func init() {
	webTopics["services"] = webTopicDef{snapshot: webServicesSnapshot, apply: webApplyReplace[webproto.ServicesSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishServices)
	webOps[webproto.OpServicesRefresh] = (*Model).webServicesRefresh
	webOps[webproto.OpServicesLogs] = (*Model).webServicesLogs
	webOps[webproto.OpServicesStart] = func(m *Model, call *webCall) tea.Cmd { return m.webServicesAct(call, "start") }
	webOps[webproto.OpServicesRestart] = func(m *Model, call *webCall) tea.Cmd { return m.webServicesAct(call, "restart") }
	webAskOps[webproto.OpServicesStop] = (*Model).webServicesStop
}

const webServicesWait = 15 * time.Second

// Tests swap these: the laptop's start, restart and stop of a service and
// the compose logs.
var (
	webSvcStart       = func(m *Model, section, name string) (string, tea.Cmd) { return m.servicesStart(section, name) }
	webSvcRestart     = func(m *Model, section, name string) (string, tea.Cmd) { return m.servicesRestart(section, name, nil) }
	webSvcStop        = func(m *Model, section, name string) (string, tea.Cmd) { return m.servicesStop(section, name, nil) }
	webSvcComposeLogs = func(ctx context.Context, c *services.Compose, name string, lines int) (string, error) {
		return c.Logs(ctx, name, lines, 0)
	}
)

type webServicesPub struct {
	key  string
	last *webproto.ServicesSnap
}

type webServicesDelta webproto.ServicesSnap

func (webServicesDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webServicesDelta)
	return n, ok
}

// webServicesState is the laptop's services state, made on first use.
func (m *Model) webServicesState() *servicesState {
	if m.services == nil {
		m.services = newServicesState()
	}
	return m.services
}

func (m *Model) webServicesKey() string {
	st := m.webServicesState()
	return strconv.FormatBool(st.opened) + strconv.FormatBool(st.loaded) + "|" + strconv.FormatUint(st.seq, 10) + "|" + strconv.FormatUint(st.doneSeq, 10) +
		"|" + strconv.Itoa(len(st.busy)) + "|" + strconv.Itoa(len(m.terminalTabs))
}

func webServicesSnapshot(m *Model, _ string) (any, error) {
	snap := m.webServicesBuild()
	if m.web != nil {
		m.web.run.services = webServicesPub{key: m.webServicesKey(), last: &snap}
	}
	return snap, nil
}

// webPublishServices sends the list when it changed; the first listening
// browser makes the laptop read it.
func (m *Model) webPublishServices(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.run.services
	if !h.webListens("services") {
		p.last = nil
		return nil
	}
	var cmd tea.Cmd
	if st := m.webServicesState(); !st.loaded && !st.inflight {
		_, cmd = m.servicesRefreshTarget()
	}
	key := m.webServicesKey()
	if p.last != nil && key == p.key {
		return cmd
	}
	p.key = key
	cur := m.webServicesBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return cmd
	}
	p.last = &cur
	h.publish("services", webServicesDelta(cur))
	return cmd
}

// webServiceItem is one service for the page.
func (m *Model) webServiceItem(st *servicesState, section string, s services.Service) webproto.ServiceItem {
	ss := st.statusOf(section, s.Name)
	it := webproto.ServiceItem{Name: webName(s.Name), Kind: section, State: ss.State.String(), Health: webName(ss.Health),
		Busy: ss.State.Busy() || ss.State == services.Restarting, Source: webName(s.Source), Ports: []webproto.ServicePort{}}
	if ss.State == services.Exited {
		it.Exit = ss.ExitCode
	}
	if section == svcSecCompose {
		it.Image = webName(s.Image)
		it.Status = webName(ss.Text)
	} else {
		it.Command, _ = aiServicesRedact(webName(s.Command))
	}
	if ss.State.Up() && !ss.StartedAt.IsZero() {
		it.Since = ss.StartedAt.UnixMilli()
	}
	ports := ss.Ports
	if len(ports) == 0 {
		ports = s.Ports
	}
	for _, p := range ports {
		if p.Published == 0 {
			continue
		}
		sp := webproto.ServicePort{Published: p.Published, Target: p.Target, Proto: webName(p.Proto)}
		if ss.PortOpen != nil {
			sp.Open = map[bool]string{true: "yes", false: "no"}[*ss.PortOpen]
		}
		it.Ports = append(it.Ports, sp)
	}
	return it
}

// webServicesBuild is the services topic now.
func (m *Model) webServicesBuild() webproto.ServicesSnap {
	st := m.webServicesState()
	s := webproto.ServicesSnap{State: "loading", ComposeFiles: []string{}, Services: []webproto.ServiceItem{}}
	if !st.loaded {
		return s
	}
	d := st.det
	if d.Empty() {
		s.State = "none"
		return s
	}
	s.State, s.Project = "ready", webName(st.project)
	for _, f := range d.ComposeFiles {
		s.ComposeFiles = append(s.ComposeFiles, webName(f))
	}
	if st.problem.Kind != services.ProblemNone {
		s.Problem, _ = aiServicesRedact(webName(st.composeProblem().Message()))
	}
	if d.ConfigErr != nil {
		s.ConfigError = webName(d.ConfigErr.Error())
	}
	add := func(section string, list []services.Service) {
		for _, sv := range list {
			if len(s.Services) == webproto.MaxServices {
				s.More++
				continue
			}
			s.Services = append(s.Services, m.webServiceItem(st, section, sv))
		}
	}
	add(svcSecCompose, st.compose)
	add(svcSecLocal, st.local)
	return s
}

// webServicesRefresh reads the list again.
func (m *Model) webServicesRefresh(call *webCall) tea.Cmd {
	_, cmd := m.servicesRefreshTarget()
	call.reply(nil)
	return cmd
}

// webServiceFind is the section of a listed service, or a not_found answer.
func (m *Model) webServiceFind(call *webCall, name string) (string, bool) {
	st := m.webServicesState()
	if section, ok := st.aiFind(name); ok && st.loaded {
		return section, true
	}
	call.fail(webproto.CodeNotFound, "That service is not in the list.")
	return "", false
}

// webServicesAct starts or restarts a service (the page asked first).
func (m *Model) webServicesAct(call *webCall, verb string) tea.Cmd {
	name := call.args.(*webproto.ServiceArgs).Name
	section, ok := m.webServiceFind(call, name)
	if !ok {
		return nil
	}
	st := m.services
	if section == svcSecCompose && st.composeBackend() == nil {
		call.fail(webproto.CodeConflict, "Docker Compose is not available on the laptop.")
		return nil
	}
	run := webSvcStart
	if verb == "restart" {
		run = webSvcRestart
	}
	st.pendingLocal = ""
	note, cmd := run(m, section, name)
	if section == svcSecLocal && st.pendingLocal == name {
		m.webAuditOp(call, name, "asked on the laptop")
		call.reply(webproto.ServiceDone{Asked: true})
		return cmd
	}
	m.webAuditOp(call, name, "ok")
	call.reply(webproto.ServiceDone{Note: webSentence(note)})
	return cmd
}

// webServicesStop is a laptop card for stopping a running service.
func (m *Model) webServicesStop(call *webCall) *webAsk {
	name := call.args.(*webproto.ServiceArgs).Name
	section, ok := m.webServiceFind(call, name)
	if !ok {
		return nil
	}
	st := m.services
	ss := st.statusOf(section, name)
	if !ss.State.Up() && ss.State != services.Starting && ss.State != services.Restarting {
		call.fail(webproto.CodeConflict, "That service is not running.")
		return nil
	}
	what := "the Docker Compose service " + name
	if section == svcSecLocal {
		what = "the local command " + name
	}
	device := webName(call.conn.device)
	if device == "" {
		device = "web"
	}
	a := &webAsk{summary: name, title: "Stop service?", action: "Stop", body: "Stop " + what + "? What runs in it ends."}
	a.run = func(m *Model) tea.Cmd {
		note, cmd := webSvcStop(m, section, name)
		var t tea.Cmd
		if note != "" {
			m.toast, t = m.toast.PushDetail(toast.Info, "Not stopped: "+name, note)
		} else {
			m.toast, t = m.toast.PushDetail(toast.Info, "Stopping "+name, "Asked from "+device+".")
		}
		return tea.Batch(t, cmd)
	}
	return a
}

// webServicesLogs answers the newest log lines of a service, redacted
// and capped: compose's own logs, or a local service's terminal.
func (m *Model) webServicesLogs(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ServiceLogsArgs)
	section, ok := m.webServiceFind(call, a.Name)
	if !ok {
		return nil
	}
	lines := a.Lines
	if lines == 0 {
		lines = webproto.DefaultServiceLog
	}
	st := m.services
	var read func() (string, error)
	if section == svcSecCompose {
		comp := st.composeBackend()
		if comp == nil {
			call.fail(webproto.CodeConflict, "Docker Compose is not available on the laptop.")
			return nil
		}
		ctx := call.ctx
		read = func() (string, error) {
			c, cancel := context.WithTimeout(ctx, webServicesWait)
			defer cancel()
			return webSvcComposeLogs(c, comp, a.Name, lines)
		}
	} else {
		t, ok := m.svcTabAny(svcLocalTab(a.Name))
		if !ok {
			call.fail(webproto.CodeNotFound, "That service has not run yet.")
			return nil
		}
		c, buf := m.nvim, t.BufID
		read = func() (string, error) {
			got, err := webTaskLines(c, buf, lines+webTailSpare)
			for len(got) > 0 && strings.TrimSpace(got[len(got)-1]) == "" {
				got = got[:len(got)-1]
			}
			if len(got) > lines {
				got = got[len(got)-lines:]
			}
			return strings.Join(got, "\n"), err
		}
	}
	name := a.Name
	call.later()
	return func() tea.Msg {
		text, err := read()
		if err != nil {
			msg, _ := aiServicesRedact(webName(err.Error()))
			call.fail(webproto.CodeConflict, webSentence("the logs cannot be read: "+msg))
			return nil
		}
		text, n := aiServicesRedact(strings.TrimRight(text, "\n"))
		out := webproto.ServiceLogs{Name: name, Lines: []string{}, Redacted: n}
		if len(text) > webproto.MaxServiceLogs {
			text = text[len(text)-webproto.MaxServiceLogs:]
			if i := strings.IndexByte(text, '\n'); i >= 0 {
				text = text[i+1:]
			}
			out.Truncated = true
		}
		if text != "" {
			for _, l := range strings.Split(text, "\n") {
				out.Lines = append(out.Lines, sanitizeOutputLine(l))
			}
		}
		call.reply(out)
		return nil
	}
}
