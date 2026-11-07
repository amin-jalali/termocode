package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/devcontainer"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-7: "Workspace: Reopen in Dev Container…". termocode makes a
// container from the project's own devcontainer.json (image or build),
// after a card that lists every command it will run and where. Only a
// container termocode makes this way runs the lifecycle commands; an
// existing one (same name) is only started, the file is not read for it.

const confirmKindDevContainerTrust confirmKindEnum = 5419

// devcontainerExec runs the setup steps (tests swap it).
var devcontainerExec services.Exec = services.SpawnExec{}

// devcontainerStepTimeout caps one step (a build or npm install can be
// slow, a hung one must end).
var devcontainerStepTimeout = 30 * time.Minute

// devcontainerRun is the setup in progress.
type devcontainerRun struct {
	root    string
	engine  []string
	setup   devcontainer.Setup
	steps   []devcontainer.Step
	next    int
	envFile string
	envText string
	cfg     devcontainer.Config
}

type devcontainerExistsMsg struct{ exists bool }

type devcontainerStepMsg struct {
	i      int
	err    error
	stderr string
}

// devcontainerEngine is the engine command prefix: the detected binary
// and its context flag.
func (m Model) devcontainerEngine() ([]string, string) {
	e := m.services.containerEngine
	bin := e.Binary
	if bin == "" {
		bin = "docker"
	}
	out := []string{bin}
	if e.Context != "" {
		if e.Kind == "podman" {
			out = append(out, "--connection", e.Context)
		} else {
			out = append(out, "--context", e.Context)
		}
	}
	return out, e.Context
}

// devcontainerEnvPath is the 0600 env file of the container name.
func devcontainerEnvPath(name string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "termocode", "devcontainer", name+".env")
}

// openDevContainerReopen reads devcontainer.json and opens the trust card.
func (m *Model) openDevContainerReopen() tea.Cmd {
	ws := m.workspace()
	if ws.Kind == workspace.KindContainer {
		return m.pushToastDetail(toast.Info, "Already in a container", "Open the local folder first.")
	}
	if m.devcontainer != nil {
		return m.pushToastDetail(toast.Info, "A dev container is being made", "Wait for it to finish.")
	}
	root, err := m.rootDir()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "No workspace folder", err.Error())
	}
	file := devcontainer.Find(root)
	if file == "" {
		return m.pushToastDetail(toast.Info, "No devcontainer.json", "Add .devcontainer/devcontainer.json to this project first.")
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot read devcontainer.json", err.Error())
	}
	cfg, err := devcontainer.Parse(src)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "devcontainer.json has an error", err.Error())
	}
	envText, err := cfg.EnvFileText()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "devcontainer.json has an error", err.Error())
	}
	engine, _ := m.devcontainerEngine()
	envFile := ""
	if envText != "" {
		envFile = devcontainerEnvPath(devcontainer.ContainerName(root))
	}
	setup, err := cfg.PlanSetup(root, file, engine, envFile)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot make this dev container here", err.Error())
	}
	m.devcontainerPending = &devcontainerRun{root: root, engine: engine, setup: setup,
		envFile: envFile, envText: envText, cfg: cfg}
	m.confirm = confirm.New("Reopen in Dev Container", devcontainerCardText(cfg, setup, devcontainer.Trusted(root, cfg)),
		[]confirm.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "create", Title: "Create and Open"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindDevContainerTrust
	return nil
}

// devcontainerCardText says what will be made and lists every command
// of the file with where it runs.
func devcontainerCardText(cfg devcontainer.Config, s devcontainer.Setup, trusted bool) string {
	var b strings.Builder
	what := "image " + cfg.Image
	if cfg.Kind() == devcontainer.KindBuild {
		what = "an image built from " + cfg.Dockerfile
	}
	fmt.Fprintf(&b, "termocode makes the container %s from %s and opens %s in it.\n", s.Name, what, s.WorkspaceFolder)
	var cmds []devcontainer.Step
	for _, st := range append(append([]devcontainer.Step(nil), s.Create...), s.Lifecycle...) {
		if st.Lifecycle {
			cmds = append(cmds, st)
		}
	}
	if len(cmds) == 0 {
		b.WriteString("\nIt runs no setup commands.")
		return b.String()
	}
	b.WriteString("\nCommands from devcontainer.json it runs:\n")
	for _, plan := range cfg.Plan() {
		fmt.Fprintf(&b, "• %s on %s: %s\n", plan.Stage, plan.Where(), plan.String())
	}
	if trusted {
		b.WriteString("\nYou agreed to these same commands before.")
	} else {
		b.WriteString("\nOnly continue if you trust this project.")
	}
	return strings.TrimRight(b.String(), "\n")
}

// onDevContainerTrustConfirm handles Cancel / Create and Open.
func (m *Model) onDevContainerTrustConfirm(id string) tea.Cmd {
	run := m.devcontainerPending
	m.devcontainerPending = nil
	if id != "create" || run == nil {
		return nil
	}
	if err := devcontainer.Trust(run.root, run.cfg); err != nil {
		recordError("devcontainer trust: " + err.Error())
	}
	if run.envFile != "" {
		if err := os.MkdirAll(filepath.Dir(run.envFile), 0o700); err == nil {
			err = os.WriteFile(run.envFile, []byte(run.envText), 0o600)
		}
		if err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot write the env file", err.Error())
		}
	}
	m.devcontainer = run
	return tea.Batch(m.pushToastDetail(toast.Info, "Making the dev container", run.setup.Name),
		m.devcontainerInspectCmd())
}

// devcontainerInspectCmd asks whether the container is there already.
func (m Model) devcontainerInspectCmd() tea.Cmd {
	run := m.devcontainer
	if run == nil {
		return nil
	}
	name, engine := run.setup.Name, run.engine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _, code, err := devcontainerExec.Capture(ctx, "", append(append([]string(nil), engine...), "inspect", "--type", "container", name)...)
		return devcontainerExistsMsg{exists: err == nil && code == 0}
	}
}

// handleDevContainerMsg routes the setup messages. ok=false: not ours.
func (m *Model) handleDevContainerMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case devcontainerExistsMsg:
		run := m.devcontainer
		if run == nil {
			return nil, true
		}
		if msg.exists {
			// Made before: only start it; the file is not run again.
			eng := append([]string(nil), run.engine...)
			run.steps = []devcontainer.Step{{Title: "Start the container", Argv: append(eng, "start", run.setup.Name)}}
		} else {
			run.steps = append(append([]devcontainer.Step(nil), run.setup.Create...), run.setup.Lifecycle...)
		}
		run.next = 0
		return m.devcontainerStepCmd(), true
	case devcontainerStepMsg:
		run := m.devcontainer
		if run == nil || msg.i != run.next {
			return nil, true
		}
		st := run.steps[msg.i]
		if msg.err != nil {
			m.devcontainer = nil
			detail := msg.err.Error()
			if tail := lastLines(msg.stderr, 6); tail != "" {
				detail += "\n" + tail
			}
			return m.pushToastDetail(toast.Errr, "Dev container: "+st.Title+" failed", detail), true
		}
		run.next++
		if run.next < len(run.steps) {
			return tea.Batch(m.pushToastDetail(toast.Info,
				fmt.Sprintf("Dev container: %s (%d/%d)", run.steps[run.next].Title, run.next+1, len(run.steps)), ""),
				m.devcontainerStepCmd()), true
		}
		m.devcontainer = nil
		_, engineCtx := m.devcontainerEngine()
		return m.openContainerWorkspace(containerRef{Context: engineCtx, Name: run.setup.Name, Root: run.setup.WorkspaceFolder}), true
	}
	return nil, false
}

// devcontainerStepCmd runs the next step off the UI loop.
func (m Model) devcontainerStepCmd() tea.Cmd {
	run := m.devcontainer
	if run == nil || run.next >= len(run.steps) {
		return nil
	}
	i, st := run.next, run.steps[run.next]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), devcontainerStepTimeout)
		defer cancel()
		_, stderr, code, err := devcontainerExec.Capture(ctx, st.Dir, st.Argv...)
		if err == nil && code != 0 {
			err = fmt.Errorf("exited with code %d", code)
		}
		return devcontainerStepMsg{i: i, err: err, stderr: stderr}
	}
}

// lastLines is the last n non-empty lines of s.
func lastLines(s string, n int) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return strings.Join(out, "\n")
}
