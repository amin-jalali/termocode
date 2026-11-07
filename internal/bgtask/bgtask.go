// Package bgtask runs one agent task in a throwaway container that keeps
// working on its own (CMP-1): termocode, the client and the laptop may
// all go away while it runs. The container is started detached
// (`run -d`) on the container engine of the host termocode runs on —
// with the persistent session on your own server, that is the server.
//
// The sandbox sees only two folders, at their own paths: the worktree
// (read and write: the agent's edits land there) and the repository's
// git common dir (so `git` works inside the worktree). All capabilities
// are dropped and no-new-privileges is set; the agent runs as the host
// user, so the files it writes stay the user's. API keys are passed by
// variable NAME only (`-e NAME`): the engine CLI copies the value from
// termocode's environment, the value never is part of a command line.
//
// The package only builds argv and reads engine output; it runs
// nothing itself. A remote engine (a context or connection that is not
// this host) is refused: its bind mounts would name folders on another
// machine.
package bgtask

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agents"
)

// Labels on every task container.
const (
	LabelTask     = "termocode.task"
	LabelWorktree = "termocode.worktree"
	LabelAgent    = "termocode.agent"
	LabelTitle    = "termocode.title"
	NamePrefix    = "termocode-task-"
	// DefaultImage has node (npx), git and a shell.
	DefaultImage = "node:22-bookworm"
	// TitleMax is the runes of the task kept in the title label.
	TitleMax = 120
	// TaskMax is the runes of a task the agent gets.
	TaskMax = 8000
)

// KeyEnv are the variable names passed into the sandbox (values come
// from termocode's environment through the engine CLI; unset ones are
// skipped by the engine).
var KeyEnv = []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENROUTER_API_KEY", "DASHSCOPE_API_KEY"}

// headless is how each agent runs one task without a person: the words
// before and after the task. The container is the sandbox, so the
// agent's own approval prompts are turned off (nobody is there to
// answer them).
var headless = map[string]struct{ pre, post []string }{
	"claude":   {pre: []string{"-p"}, post: []string{"--dangerously-skip-permissions"}},
	"codex":    {pre: []string{"exec", "--dangerously-bypass-approvals-and-sandbox"}},
	"gemini":   {pre: []string{"--yolo", "-p"}},
	"qwen":     {pre: []string{"--yolo", "-p"}},
	"opencode": {pre: []string{"run"}},
	"aider":    {pre: []string{"--yes-always", "--message"}},
}

// Supported says whether the agent has a headless mode termocode knows.
func Supported(agentID string) bool {
	_, ok := headless[agentID]
	return ok
}

// SupportedIDs lists the agents with a headless mode, in Registry order.
func SupportedIDs() []string {
	var out []string
	for _, a := range agents.Registry {
		if Supported(a.ID) {
			out = append(out, a.ID)
		}
	}
	return out
}

// AgentArgv is the command inside the sandbox: the agent through npx
// when it is an npm package (the image needs no install), else its own
// program name (the image must have it).
func AgentArgv(agentID, task string) ([]string, error) {
	h, ok := headless[agentID]
	if !ok {
		return nil, fmt.Errorf("%s has no headless mode termocode knows", agentID)
	}
	a, ok := agents.ByID(agentID)
	if !ok {
		return nil, fmt.Errorf("unknown agent %s", agentID)
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return nil, errors.New("the task is empty")
	}
	if r := []rune(task); len(r) > TaskMax {
		return nil, fmt.Errorf("the task is longer than %d characters", TaskMax)
	}
	var argv []string
	if len(a.Packages) > 0 && agentID != "aider" {
		argv = []string{"npx", "-y", a.Packages[0]}
	} else {
		argv = []string{a.Bin}
	}
	argv = append(argv, h.pre...)
	argv = append(argv, task)
	return append(argv, h.post...), nil
}

// Spec is one task to start.
type Spec struct {
	ID       string // NewID
	Worktree string // absolute folder the agent works in
	Common   string // the git common dir ("" = none mounted)
	Agent    string // agents.Agent ID
	Task     string
	Image    string // "" = DefaultImage
	User     string // "uid:gid" ("" = the image's user)
}

// NewID is a task id from the start time: short, sortable, a valid
// container name part.
func NewID(now time.Time) string {
	return strings.ToLower(now.UTC().Format("20060102-150405")) + "-" + strconv.FormatInt(int64(now.Nanosecond()/1e6), 36)
}

// ValidID says whether id is safe as a container name part.
func ValidID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// Name is the container name of task id.
func Name(id string) string { return NamePrefix + id }

func cleanPath(p string) error {
	if p == "" || p[0] != '/' {
		return fmt.Errorf("%q is not an absolute folder", p)
	}
	if strings.ContainsAny(p, ":,\t\n\r") {
		return fmt.Errorf("%q has a character a bind mount cannot carry", p)
	}
	return nil
}

// Title is the one-line task text kept in the title label.
func Title(task string) string {
	t := strings.Join(strings.Fields(task), " ")
	if r := []rune(t); len(r) > TitleMax {
		t = string(r[:TitleMax-1]) + "…"
	}
	return t
}

// RunArgv is the engine argv (without the binary) that starts s
// detached.
func RunArgv(s Spec) ([]string, error) {
	if !ValidID(s.ID) {
		return nil, fmt.Errorf("bad task id %q", s.ID)
	}
	if err := cleanPath(s.Worktree); err != nil {
		return nil, err
	}
	if s.Common != "" {
		if err := cleanPath(s.Common); err != nil {
			return nil, err
		}
	}
	cmd, err := AgentArgv(s.Agent, s.Task)
	if err != nil {
		return nil, err
	}
	img := s.Image
	if img == "" {
		img = DefaultImage
	}
	if strings.HasPrefix(img, "-") || strings.ContainsAny(img, " \t\n") {
		return nil, fmt.Errorf("bad image %q", img)
	}
	argv := []string{"run", "-d", "--name", Name(s.ID),
		"--label", LabelTask + "=" + s.ID,
		"--label", LabelWorktree + "=" + s.Worktree,
		"--label", LabelAgent + "=" + s.Agent,
		"--label", LabelTitle + "=" + Title(s.Task),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", "1024",
		"-v", s.Worktree + ":" + s.Worktree,
	}
	if s.Common != "" && !strings.HasPrefix(s.Common+"/", s.Worktree+"/") {
		argv = append(argv, "-v", s.Common+":"+s.Common)
	}
	argv = append(argv, "-w", s.Worktree, "-e", "HOME=/tmp", "-e", "CI=1")
	for _, k := range KeyEnv {
		argv = append(argv, "-e", k)
	}
	if s.User != "" {
		argv = append(argv, "--user", s.User)
	}
	argv = append(argv, img)
	return append(argv, cmd...), nil
}

// ListArgv lists every task container, one tab-separated line each:
// name, state, status, task, worktree, agent, title. kind is the engine
// kind ("docker" or "podman").
func ListArgv(kind string) []string {
	f := func(k string) string { return `{{.Label "` + k + `"}}` }
	if kind == "podman" {
		f = func(k string) string { return `{{index .Labels "` + k + `"}}` }
	}
	format := strings.Join([]string{"{{.Names}}", "{{.State}}", "{{.Status}}",
		f(LabelTask), f(LabelWorktree), f(LabelAgent), f(LabelTitle)}, "\t")
	return []string{"ps", "-a", "--filter", "label=" + LabelTask, "--format", format}
}

// LogsArgv follows the task's output (from the last 200 lines).
func LogsArgv(id string) []string { return []string{"logs", "-f", "--tail", "200", Name(id)} }

// StopArgv stops a running task.
func StopArgv(id string) []string { return []string{"stop", Name(id)} }

// RemoveArgv removes a stopped task's container (the worktree stays).
func RemoveArgv(id string) []string { return []string{"rm", Name(id)} }

// State of a task.
type State string

const (
	Running State = "running"
	Done    State = "done"   // exited 0
	Failed  State = "failed" // exited non-zero
	Other   State = "other"  // created, paused, restarting …
)

// Task is one listed task container.
type Task struct {
	ID, Name, Worktree, Agent, Title string
	State                            State
	Exit                             int    // -1 = unknown or running
	Status                           string // the engine's own words
}

// ParseList reads ListArgv's output. Lines that are not termocode tasks
// are skipped.
func ParseList(out string) []Task {
	var tasks []Task
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 7 {
			continue
		}
		t := Task{Name: f[0], Status: f[2], ID: f[3], Worktree: f[4], Agent: f[5], Title: strings.Join(f[6:], " "), Exit: -1}
		if !ValidID(t.ID) || t.Name != Name(t.ID) {
			continue
		}
		t.State, t.Exit = state(strings.ToLower(f[1]), f[2])
		tasks = append(tasks, t)
	}
	return tasks
}

// state reads "running" / "exited" and "Exited (2) 3 minutes ago".
func state(s, status string) (State, int) {
	switch s {
	case "running":
		return Running, -1
	case "exited", "stopped":
		code := -1
		if i := strings.IndexByte(status, '('); i >= 0 {
			if j := strings.IndexByte(status[i:], ')'); j > 0 {
				if n, err := strconv.Atoi(status[i+1 : i+j]); err == nil {
					code = n
				}
			}
		}
		if code == 0 {
			return Done, 0
		}
		return Failed, code
	}
	return Other, -1
}

// Local says whether an engine context name means this host: none, the
// docker "default", or podman's local socket. A remote engine cannot
// bind-mount this host's folders.
func Local(contextName, dockerHost string) bool {
	if dockerHost != "" && !strings.HasPrefix(dockerHost, "unix://") && !strings.HasPrefix(dockerHost, "npipe://") {
		return false
	}
	return contextName == "" || contextName == "default" || contextName == "desktop-linux" || contextName == "rootless"
}
