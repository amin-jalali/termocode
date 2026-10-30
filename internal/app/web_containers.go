package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Containers from the page (WEB-44, web design 2.9): the host's
// containers, a log tail, and start / restart (an in-page confirm) and
// stop / remove (a laptop card: stopping ends what runs, removing cannot
// be undone). The engine is the Containers view's (the session's context
// included), the same one the AI's container tools use. Every engine
// call runs off the UI thread with a time limit; a container is named
// only by its hex id from a fresh listing, never by text from the page.
// The logs pass the same redaction as the AI's, then webData's. No
// prune, run, pull, exec or shell from the web.

const webContainersWait = 30 * time.Second

func init() {
	webOps[webproto.OpContainersList] = (*Model).webContainersList
	webOps[webproto.OpContainersLogs] = (*Model).webContainersLogs
	webOps[webproto.OpContainersStart] = func(m *Model, call *webCall) tea.Cmd { return m.webContainersAct(call, "start") }
	webOps[webproto.OpContainersRestart] = func(m *Model, call *webCall) tea.Cmd { return m.webContainersAct(call, "restart") }
	webAskOps[webproto.OpContainersStop] = (*Model).webContainersStop
	webAskOps[webproto.OpContainersRemove] = (*Model).webContainersRemove
}

// webContainersHost is the engine of the Containers view, read on Update.
func (m *Model) webContainersHost(call *webCall) (aiContainersHost, bool) {
	if m.services == nil {
		m.services = newServicesState()
	}
	h, err := aiContainersGetHost(call.ctx, func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	})
	if err != nil {
		call.fail(webproto.CodeNotFound, err.Error())
		return h, false
	}
	return h, true
}

// webContainerItem is one container for the page.
func webContainerItem(c containers.Container, busy bool) webproto.ContainerItem {
	ports := strings.TrimSpace(c.PortsText)
	project := c.Project
	if project == "" {
		project = c.Labels["com.docker.compose.project"]
	}
	return webproto.ContainerItem{ID: c.ID, Name: aiContainerName(c), Image: c.Image, State: c.State, Status: c.Status,
		Ports: ports, Project: project, Busy: busy}
}

// webContainersFind lists the host and finds id in it.
func webContainersFind(ctx context.Context, h aiContainersHost, id string) (containers.Container, error) {
	list, err := h.e.ListContainers(ctx)
	if err != nil {
		return containers.Container{}, errors.New(aiContainersRedact(err.Error()))
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return containers.Container{}, errNotListed
}

var errNotListed = errors.New("that container is not on this host now")

// webContainersList answers the host's containers and keeps them for
// stop and remove.
func (m *Model) webContainersList(call *webCall) tea.Cmd {
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		list, err := h.e.ListContainers(ctx)
		if err != nil {
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		label := h.contextLabel(ctx)
		return applyMsg(func(m *Model) tea.Cmd {
			out := webproto.ContainersList{Engine: h.bin(), Context: label, Containers: []webproto.ContainerItem{}}
			keep := map[string]webproto.ContainerItem{}
			for i, c := range list {
				busy := false
				if m.services != nil {
					busy = m.services.busy[containerBusyPrefix+c.ID].Busy()
				}
				it := webContainerItem(c, busy)
				if i == webproto.MaxContainers {
					out.More = len(list) - i
					break
				}
				out.Containers = append(out.Containers, it)
				keep[c.ID] = it
			}
			if m.web != nil {
				m.web.more.ctrs = keep
			}
			call.reply(out)
			return nil
		})
	}
}

// webContainersLogs answers the newest lines of a container's logs,
// redacted and capped.
func (m *Model) webContainersLogs(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ContainerLogsArgs)
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	lines := a.Lines
	if lines == 0 {
		lines = webproto.DefaultContainerLog
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		c, err := webContainersFind(ctx, h, a.ID)
		if err != nil {
			call.fail(webproto.CodeNotFound, err.Error())
			return nil
		}
		text, err := h.e.Logs(ctx, c.ID, lines, "")
		if err != nil {
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		text, n := aiServicesRedact(strings.TrimRight(text, "\n"))
		out := webproto.ContainerLogs{ID: c.ID, Lines: []string{}, Redacted: n}
		if len(text) > webproto.MaxContainerLogs {
			text = text[len(text)-webproto.MaxContainerLogs:]
			if i := strings.IndexByte(text, '\n'); i >= 0 {
				text = text[i+1:]
			}
			out.Truncated = true
		}
		if text != "" {
			out.Lines = strings.Split(text, "\n")
		}
		call.reply(out)
		return nil
	}
}

// webContainersRun runs action on a listed container off the UI thread:
// the Containers view shows it busy meanwhile; done answers on Update.
// parent ends the work early (the asking connection for start and
// restart; nothing for a card the laptop confirmed).
func webContainersRun(parent context.Context, h aiContainersHost, id, action string, done func(m *Model, c containers.Container, err error) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, webContainersWait)
		defer cancel()
		c, err := webContainersFind(ctx, h, id)
		if err == nil {
			run := map[string]func(context.Context, string) error{"start": h.e.Start, "stop": h.e.Stop,
				"restart": h.e.Restart, "remove": h.e.RemoveContainer}[action]
			err = run(ctx, c.ID)
			if err != nil {
				err = errors.New(aiContainersRedact(err.Error()))
			} else if action != "remove" {
				if now, ferr := webContainersFind(ctx, h, id); ferr == nil {
					c = now
				}
			}
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.services != nil {
				delete(m.services.busy, containerBusyPrefix+id)
				m.services.touch()
			}
			return tea.Batch(done(m, c, err), m.containersRefresh("containers"))
		})
	}
}

// webContainersBusy marks a container busy in the laptop's view.
func (m *Model) webContainersBusy(id, action string) {
	if m.services == nil {
		return
	}
	state := services.Restarting
	switch action {
	case "start":
		state = services.Starting
	case "stop", "remove":
		state = services.Stopping
	}
	m.services.busy[containerBusyPrefix+id] = state
	m.services.touch()
}

// webContainersAct starts or restarts a container (the page asked first).
func (m *Model) webContainersAct(call *webCall, action string) tea.Cmd {
	id := call.args.(*webproto.ContainerArgs).ID
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	if m.services.busy[containerBusyPrefix+id].Busy() {
		call.fail(webproto.CodeBusy, "That container is busy. Try again.")
		return nil
	}
	m.webContainersBusy(id, action)
	call.later()
	return webContainersRun(call.ctx, h, id, action, func(m *Model, c containers.Container, err error) tea.Cmd {
		if errors.Is(err, errNotListed) {
			call.fail(webproto.CodeNotFound, err.Error())
			return nil
		}
		if err != nil {
			m.webAuditOp(call, aiShortID(id), "failed")
			call.fail(webproto.CodeInternal, action+" failed: "+err.Error())
			return nil
		}
		m.webAuditOp(call, aiShortID(id)+" "+aiContainerName(c), "ok")
		call.reply(webproto.ContainerDone{State: c.State, Status: c.Status})
		return nil
	})
}

// webContainersAsk is a laptop card for stop or remove of a container
// the last containers.list gave.
func (m *Model) webContainersAsk(call *webCall, action string) *webAsk {
	id := call.args.(*webproto.ContainerArgs).ID
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	it, listed := m.web.more.ctrs[id]
	if !listed {
		call.fail(webproto.CodeNotFound, "That container is not in the list. Open the list again.")
		return nil
	}
	if m.services.busy[containerBusyPrefix+id].Busy() {
		call.fail(webproto.CodeBusy, "That container is busy. Try again.")
		return nil
	}
	a := &webAsk{summary: aiShortID(id) + " " + it.Name}
	switch action {
	case "stop":
		if it.State != "running" && it.State != "restarting" && it.State != "paused" {
			call.fail(webproto.CodeConflict, "That container is not running.")
			return nil
		}
		a.title, a.action = "Stop container?", "Stop"
		a.body = "Stop " + it.Name + " (" + it.Image + ")? What runs in it ends."
	case "remove":
		if it.State == "running" || it.State == "restarting" || it.State == "paused" {
			call.fail(webproto.CodeConflict, "Stop the container first.")
			return nil
		}
		a.title, a.action = "Remove container?", "Remove"
		a.body = "Remove the stopped container " + it.Name + " (" + it.Image + ")? This cannot be undone."
	}
	device := webName(call.conn.device)
	if device == "" {
		device = "web"
	}
	done := map[string]string{"stop": "Stopped", "remove": "Removed"}
	a.run = func(m *Model) tea.Cmd {
		m.webContainersBusy(id, action)
		return webContainersRun(context.Background(), h, id, action, func(m *Model, c containers.Container, err error) tea.Cmd {
			var t tea.Cmd
			if err != nil {
				m.toast, t = m.toast.PushDetail(toast.Errr, a.title[:len(a.title)-1]+" failed", err.Error())
			} else {
				m.toast, t = m.toast.PushDetail(toast.Info, done[action]+" "+it.Name, "Asked from "+device+".")
			}
			return t
		})
	}
	return a
}

func (m *Model) webContainersStop(call *webCall) *webAsk   { return m.webContainersAsk(call, "stop") }
func (m *Model) webContainersRemove(call *webCall) *webAsk { return m.webContainersAsk(call, "remove") }
