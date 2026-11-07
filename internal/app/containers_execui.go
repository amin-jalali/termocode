package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Exec options (CTX-12): E on a running container asks three short
// questions (shell or command, user, workdir; each may stay empty) and
// opens a terminal tab with that exec. A command runs as argv, never
// through a shell.

const (
	promptKindContainerExecRun     promptKindEnum = 4909
	promptKindContainerExecUser    promptKindEnum = 4910
	promptKindContainerExecWorkdir promptKindEnum = 4911
)

// containerExecState holds the answers while the prompts run.
type containerExecState struct {
	c         containers.Container
	run, user string
}

// containerExecOptions turns the answers into exec options: one word is
// a shell, more words are a command.
func containerExecOptions(run, user, workdir string) containers.ExecOptions {
	o := containers.ExecOptions{User: strings.TrimSpace(user), Workdir: strings.TrimSpace(workdir)}
	switch f := strings.Fields(run); len(f) {
	case 0:
	case 1:
		o.Shell = f[0]
	default:
		o.Command = f
	}
	return o
}

// askContainerExecOptions starts the prompts for c.
func (m *Model) askContainerExecOptions(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") {
		return m.servicesToast(toast.Info, "Start the container first", "exec needs a running container")
	}
	m.services.execOpts = containerExecState{c: c}
	m.askContainerExecStep(promptKindContainerExecRun)
	return nil
}

func (m *Model) askContainerExecStep(kind promptKindEnum) {
	name := containerShownName(m.services.execOpts.c)
	label, note := "Shell or command (empty = bash, else sh):", "One word is a shell (zsh, ash); more words run as a command, without a shell."
	switch kind {
	case promptKindContainerExecUser:
		label, note = "User (empty = the container's user):", "A name or uid, like root or 1000:1000."
	case promptKindContainerExecWorkdir:
		label, note = "Folder to start in (empty = the container's):", "An absolute path, like /app."
	}
	m.prompt = prompt.New("Exec in "+sanitizeOutputLine(name), label, "")
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetNote(note)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, kind
}

// onContainerExecPrompt takes one answer and asks the next, or runs.
func (m *Model) onContainerExecPrompt(kind promptKindEnum, value string) tea.Cmd {
	st := &m.services.execOpts
	if st.c.ID == "" {
		return nil
	}
	switch kind {
	case promptKindContainerExecRun:
		st.run = value
		m.askContainerExecStep(promptKindContainerExecUser)
		return nil
	case promptKindContainerExecUser:
		st.user = value
		m.askContainerExecStep(promptKindContainerExecWorkdir)
		return nil
	}
	c, o := st.c, containerExecOptions(st.run, st.user, value)
	*st = containerExecState{}
	cmd, err := m.services.containerEngine.ExecCommand(c.ID, o)
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot open exec", err.Error())
	}
	key := containerTabPrefix + "exec:" + c.ID + ":" + strings.Join(cmd.Argv[1:], " ")
	if m.servicesFocusTab(key) {
		return nil
	}
	if !strings.HasPrefix(cmd.Label, "exec:") {
		cmd.Label = "shell: " + containerShownName(c)
	}
	if err := m.servicesRunCommand(cmd, key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open exec", err.Error())
	}
	return nil
}
