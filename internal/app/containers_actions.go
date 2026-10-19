package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

const (
	containerBusyPrefix = "container/"
	containerTabPrefix  = "container:"
)

type containerActionDoneMsg struct {
	id, verb string
	err      error
}

type containerGroupDoneMsg struct {
	project  string
	fallback bool
	err      error
}

func (m *Model) onContainerGroupDone(msg containerGroupDoneMsg) tea.Cmd {
	for _, c := range m.services.containerRows {
		if c.Project == msg.project {
			delete(m.services.busy, containerBusyPrefix+c.ID)
		}
	}
	m.services.touch()
	if msg.err != nil {
		return tea.Batch(m.servicesToast(toast.Errr, "Project action failed", errLine(msg.err.Error())), m.containersRefresh("containers"))
	}
	if msg.fallback {
		return tea.Batch(m.servicesToast(toast.Info, "Project folder not available", "Ran the action on this project's existing containers"), m.containersRefresh("containers"))
	}
	return m.containersRefresh("containers")
}

func (m *Model) selectedContainer() (containers.Container, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostContainer || r.idx < 0 || r.idx >= len(m.services.containerRows) {
		return containers.Container{}, false
	}
	return m.services.containerRows[r.idx], true
}

func (m *Model) containerAction(verb string, c containers.Container) tea.Cmd {
	st := m.services
	if st == nil || !st.containerEngine.Reachable {
		return m.servicesToast(toast.Info, "Container engine is not ready", "Refresh the Containers view and try again")
	}
	key := containerBusyPrefix + c.ID
	if st.busy[key].Busy() {
		name := c.ID
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		return m.servicesToast(toast.Info, "Container is busy", name)
	}
	e := st.containerEngine
	var run func(context.Context) error
	switch verb {
	case "start":
		run = func(ctx context.Context) error { return e.Start(ctx, c.ID) }
	case "stop":
		run = func(ctx context.Context) error { return e.Stop(ctx, c.ID) }
	case "restart":
		run = func(ctx context.Context) error { return e.Restart(ctx, c.ID) }
	case "pause":
		run = func(ctx context.Context) error { return e.Pause(ctx, c.ID) }
	case "unpause":
		run = func(ctx context.Context) error { return e.Unpause(ctx, c.ID) }
	default:
		return nil
	}
	state := services.Restarting
	if verb == "start" {
		state = services.Starting
	}
	if verb == "stop" {
		state = services.Stopping
	}
	st.busy[key] = state
	st.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return containerActionDoneMsg{id: c.ID, verb: verb, err: run(ctx)}
	}
}

func (m *Model) selectedProjectGroup() (string, string, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowGroup || !strings.HasPrefix(r.section, "grp/containers/") || r.name == "other" {
		return "", "", false
	}
	dir := ""
	for _, c := range m.services.containerRows {
		if c.Project == r.name && c.WorkingDir != "" {
			dir = c.WorkingDir
			break
		}
	}
	return r.name, dir, true
}

func (m *Model) projectGroupAction(verb string) tea.Cmd {
	project, dir, ok := m.selectedProjectGroup()
	if !ok {
		return nil
	}
	if dir == "" && project == m.services.project {
		dir = m.services.root
	}
	if dir == "" || !projectDirExists(dir) {
		return m.projectGroupFallback(project, verb)
	}
	args := []string{verb}
	if verb == "up" {
		args = append(args, "-d")
	}
	cmd := m.services.containerEngine.ComposeWithFiles(project, dir, m.projectConfigFiles(project), args...)
	key := containerTabPrefix + "compose:" + project
	if err := m.servicesRunCommand(cmd, key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot "+verb+" project", errLine(err.Error()))
	}
	state := services.Restarting
	if verb == "up" {
		state = services.Starting
	}
	if verb == "stop" {
		state = services.Stopping
	}
	for _, c := range m.services.containerRows {
		if c.Project == project {
			m.services.busy[containerBusyPrefix+c.ID] = state
		}
	}
	m.services.touch()
	return nil
}

func (m *Model) projectConfigFiles(project string) []string {
	for _, c := range m.services.containerRows {
		if c.Project == project && len(c.ConfigFiles) > 0 {
			return append([]string(nil), c.ConfigFiles...)
		}
	}
	return nil
}

func projectDirExists(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func (m *Model) projectGroupFallback(project, verb string) tea.Cmd {
	var ids []string
	for _, c := range m.services.containerRows {
		if c.Project == project {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return m.servicesToast(toast.Info, "Project folder is unavailable", "No containers were found for "+project)
	}
	e := m.services.containerEngine
	state := services.Restarting
	if verb == "up" {
		state = services.Starting
	}
	if verb == "stop" {
		state = services.Stopping
	}
	for _, id := range ids {
		m.services.busy[containerBusyPrefix+id] = state
	}
	m.services.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, id := range ids {
			var err error
			switch verb {
			case "up":
				err = e.Start(ctx, id)
			case "stop":
				err = e.Stop(ctx, id)
			case "restart":
				err = e.Restart(ctx, id)
			}
			if err != nil {
				return containerGroupDoneMsg{project: project, err: err}
			}
		}
		return containerGroupDoneMsg{project: project, fallback: true}
	}
}

func (m *Model) askProjectDown() tea.Cmd {
	project, dir, ok := m.selectedProjectGroup()
	if !ok {
		return nil
	}
	if dir == "" && project == m.services.project {
		dir = m.services.root
	}
	if dir == "" || !projectDirExists(dir) {
		return m.servicesToast(toast.Errr, "Cannot safely remove project", "The project folder is missing; no compose down command was run")
	}
	files := m.projectConfigFiles(project)
	cmd := m.services.containerEngine.ComposeWithFiles(project, dir, files, "down")
	m.confirm = confirm.New("Remove project containers?", "This stops and removes the containers and networks for "+project+". Volumes stay.\n\nCommand: "+svcShellLine(cmd.Argv), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "down", Title: "Down", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerProjectDown
	m.pendingContainerProject, m.pendingContainerDir = project, dir
	m.pendingContainerFiles = files
	return nil
}

func (m *Model) onContainerActionDone(msg containerActionDoneMsg) tea.Cmd {
	delete(m.services.busy, containerBusyPrefix+msg.id)
	m.services.touch()
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.servicesToast(toast.Errr, "Could not "+msg.verb+" container", errLine(msg.err.Error())))
	}
	cmds = append(cmds, m.containersRefresh("containers"))
	return tea.Batch(cmds...)
}

func (m *Model) askRemoveContainer(c containers.Container) {
	running := strings.EqualFold(c.State, "running")
	command := fmt.Sprintf("%s rm %s", m.services.containerEngine.Binary, c.ID)
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "remove", Title: "Remove", Style: confirm.StyleDestructive}}
	body := "This removes " + containerDisplayName(c, c.Project) + ".\n\nCommand: " + command
	if running {
		command = fmt.Sprintf("%s stop %s && %s rm %s", m.services.containerEngine.Binary, c.ID, m.services.containerEngine.Binary, c.ID)
		body = "This stops and removes " + containerDisplayName(c, c.Project) + ".\n\nCommand: " + command
		buttons[1] = confirm.Button{ID: "stop-remove", Title: "Stop and remove", Style: confirm.StyleDestructive}
	}
	m.confirm = confirm.New("Remove container?", body, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindContainerRemove
	m.pendingContainerID = c.ID
	m.pendingContainerStop = running
}

func (m *Model) runContainerRemove(stop bool) tea.Cmd {
	id := m.pendingContainerID
	m.pendingContainerID, m.pendingContainerStop = "", false
	e := m.services.containerEngine
	m.services.busy[containerBusyPrefix+id] = services.Stopping
	m.services.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var err error
		if stop {
			err = e.Stop(ctx, id)
		}
		if err == nil {
			err = e.RemoveContainer(ctx, id)
		}
		return containerActionDoneMsg{id: id, verb: "remove", err: err}
	}
}

func (m *Model) openContainerTerminal(c containers.Container, kind string) tea.Cmd {
	e := m.services.containerEngine
	var cmd services.Command
	key := containerTabPrefix + kind + ":" + c.ID
	switch kind {
	case "logs":
		cmd = e.LogsFollow(c.ID)
	case "shell":
		cmd = e.Shell(c.ID)
	case "attach":
		cmd = e.Attach(c.ID)
	case "stats":
		cmd = e.StatsFollow(c.ID)
	default:
		return nil
	}
	if m.servicesFocusTab(key) {
		return nil
	}
	cmd.Label = kind + ": " + containerDisplayName(c, c.Project)
	if err := m.servicesRunCommand(cmd, key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open "+kind, err.Error())
	}
	if kind == "attach" {
		return m.servicesToast(toast.Info, "Attached to "+containerDisplayName(c, c.Project), "Ctrl+P Ctrl+Q detaches")
	}
	return nil
}

func (m *Model) inspectContainer(c containers.Container) tea.Cmd {
	e := m.services.containerEngine
	id := c.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		value, err := e.Inspect(ctx, "container", id)
		if err != nil {
			return containerInspectMsg{id: id, err: err}
		}
		data, err := json.MarshalIndent(value, "", "  ")
		return containerInspectMsg{id: id, text: string(data), err: err}
	}
}

type containerInspectMsg struct {
	id, text string
	err      error
}

func (m *Model) onContainerInspect(msg containerInspectMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot inspect container", errLine(msg.err.Error()))
	}
	name := "container " + msg.id
	if c, ok := m.containerByID(msg.id); ok {
		name = containerDisplayName(c, c.Project)
	}
	if err := m.openReadOnlyPage("inspect: "+name, msg.text, "", "json"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open inspect page", err.Error())
	}
	return nil
}

func (m *Model) containerByID(id string) (containers.Container, bool) {
	for _, c := range m.services.containerRows {
		if c.ID == id {
			return c, true
		}
	}
	return containers.Container{}, false
}

func (m *Model) containerOpenPort(c containers.Container) tea.Cmd {
	for _, p := range c.Ports {
		if p.PublicPort < 1 {
			continue
		}
		host := p.IP
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "localhost"
		}
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		url := fmt.Sprintf("http://%s:%d", host, p.PublicPort)
		if conn := os.Getenv("SSH_CONNECTION"); conn != "" || os.Getenv("SSH_TTY") != "" {
			m.copyToClipboard(url)
			remote := "<host>"
			if fields := strings.Fields(conn); len(fields) >= 3 {
				remote = fields[2]
			}
			return m.servicesToast(toast.Info, "Copied "+url, fmt.Sprintf("over SSH: ssh -L %d:localhost:%d %s", p.PublicPort, p.PublicPort, remote))
		}
		if err := osOpen(url); err != nil {
			return m.servicesToast(toast.Errr, "Cannot open port", err.Error())
		}
		return nil
	}
	return m.servicesToast(toast.Info, "No published port", containerDisplayName(c, c.Project)+" has no published port")
}
