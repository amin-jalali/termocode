package services

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Command is a visible job the app runs in a terminal tab.
type Command struct {
	Label string
	Argv  []string
	Dir   string
	Env   map[string]string
}

// Compose drives `docker compose` for one project.
type Compose struct {
	Root     string
	Files    []string // -f files (root-relative or absolute)
	Profiles []string
	Project  string // -p; "" = compose decides
	Docker   string // the docker binary; "" = "docker"
	X        Exec
	Timeout  time.Duration // per captured call; 0 = 60 s
}

func (c *Compose) docker() string {
	if c.Docker != "" {
		return c.Docker
	}
	return "docker"
}

// base is `docker compose -f … -p … --profile …`.
func (c *Compose) base() []string {
	argv := []string{c.docker(), "compose"}
	for _, f := range c.Files {
		argv = append(argv, "-f", f)
	}
	if c.Project != "" {
		argv = append(argv, "-p", c.Project)
	}
	for _, p := range c.Profiles {
		argv = append(argv, "--profile", p)
	}
	return argv
}

func (c *Compose) argv(args ...string) []string { return append(c.base(), args...) }

// capture runs one compose call with the timeout and classifies a
// failure as a Problem.
func (c *Compose) capture(ctx context.Context, timeout time.Duration, args ...string) (string, Problem) {
	if timeout <= 0 {
		timeout = c.Timeout
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	x := c.X
	if x == nil {
		x = SpawnExec{}
	}
	out, errText, code, err := x.Capture(ctx, c.Root, c.argv(args...)...)
	switch {
	case errors.Is(err, exec.ErrNotFound) || isNotExist(err):
		return "", Problem{Kind: ProblemNoDocker}
	case errors.Is(err, context.DeadlineExceeded):
		return "", Problem{Kind: ProblemOther, Detail: fmt.Sprintf("docker compose %s timed out", args[0])}
	case err != nil:
		return "", Problem{Kind: ProblemOther, Detail: err.Error()}
	case code != 0:
		p := ProblemFromStderr(errText)
		if p.Kind == ProblemNone {
			p = Problem{Kind: ProblemOther, Detail: fmt.Sprintf("docker compose %s: exit %d", args[0], code)}
		}
		return out, p
	}
	return out, Problem{}
}

func isNotExist(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such file or directory")
}

// Load reads the project's services (`compose config --format json`).
func (c *Compose) Load(ctx context.Context) (Config, Problem) {
	out, p := c.capture(ctx, 0, "config", "--format", "json")
	if p.Kind != ProblemNone {
		return Config{}, p
	}
	cfg, err := ParseConfig([]byte(out))
	if err != nil {
		return Config{}, Problem{Kind: ProblemOther, Detail: err.Error()}
	}
	return cfg, Problem{}
}

// Status reads `compose ps --all --format json`.
func (c *Compose) Status(ctx context.Context) (map[string]Status, Problem) {
	out, p := c.capture(ctx, 0, "ps", "--all", "--format", "json")
	if p.Kind != ProblemNone {
		return nil, p
	}
	st, err := ParsePS([]byte(out), time.Now())
	if err != nil {
		first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		return nil, Problem{Kind: ProblemOther, Detail: "cannot read compose ps: " + clip(first, 60)}
	}
	return st, Problem{}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (c *Compose) action(ctx context.Context, verb string, names []string) error {
	_, p := c.capture(ctx, 0, append([]string{verb}, names...)...)
	if p.Kind != ProblemNone {
		return errors.New(p.Message())
	}
	return nil
}

// Stop runs `compose stop names…` (all when names is empty).
func (c *Compose) Stop(ctx context.Context, names []string) error {
	return c.action(ctx, "stop", names)
}

// Restart runs `compose restart names…`.
func (c *Compose) Restart(ctx context.Context, names []string) error {
	return c.action(ctx, "restart", names)
}

// Down runs `compose down` (removes containers, keeps volumes). The UI
// asks first; it is never an AI tool.
func (c *Compose) Down(ctx context.Context) error {
	return c.action(ctx, "down", nil)
}

// Logs captures the last lines of one service ("" = all), for the AI.
func (c *Compose) Logs(ctx context.Context, name string, lines int, since time.Duration) (string, error) {
	args := []string{"logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(lines)}
	if since > 0 {
		args = append(args, "--since", since.String())
	}
	if name != "" {
		args = append(args, name)
	}
	out, p := c.capture(ctx, 10*time.Second, args...)
	if p.Kind != ProblemNone {
		return out, errors.New(p.Message())
	}
	return out, nil
}

// Up is the visible `up -d [names]` job.
func (c *Compose) Up(names []string) Command {
	return Command{Label: "services: up", Argv: c.argv(append([]string{"up", "-d"}, names...)...), Dir: c.Root}
}

// Recreate is the visible `up -d --force-recreate name` job.
func (c *Compose) Recreate(name string) Command {
	return Command{Label: "services: up", Argv: c.argv("up", "-d", "--force-recreate", name), Dir: c.Root}
}

// LogsFollow is the visible `logs -f` job for one service ("" = all).
func (c *Compose) LogsFollow(name string) Command {
	if name == "" {
		return Command{Label: "logs: compose", Argv: c.argv("logs", "-f", "--tail", "100"), Dir: c.Root}
	}
	return Command{Label: "logs: " + name, Argv: c.argv("logs", "-f", "--tail", "200", name), Dir: c.Root}
}

// Shell is the visible `exec name sh` job.
func (c *Compose) Shell(name string) Command {
	return Command{Label: "shell: " + name, Argv: c.argv("exec", name, "sh"), Dir: c.Root}
}
