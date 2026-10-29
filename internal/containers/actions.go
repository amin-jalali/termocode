package containers

import (
	"context"
	"fmt"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

func (e *Engine) action(ctx context.Context, timeout time.Duration, args ...string) error {
	_, stderr, code, err := e.captureTimeout(ctx, timeout, args...)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	p := services.ProblemFromStderr(stderr)
	if p.Kind != services.ProblemNone {
		return fmt.Errorf("%s", p.Message())
	}
	return fmt.Errorf("engine command exited %d", code)
}
func (e *Engine) Start(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "start", id)
}
func (e *Engine) Stop(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "stop", id)
}
func (e *Engine) Restart(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "restart", id)
}
func (e *Engine) Pause(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "pause", id)
}
func (e *Engine) Unpause(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "unpause", id)
}
func (e *Engine) RemoveContainer(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "rm", id)
}
func (e *Engine) Tag(ctx context.Context, src, dst string) error {
	return e.action(ctx, 60*time.Second, "tag", src, dst)
}

// UseContext runs `docker context use <name>` (podman: `podman system
// connection default <name>`): the user's machine-wide default. It never
// carries the session's override.
func (e *Engine) UseContext(ctx context.Context, name string) error {
	plain := *e
	plain.Context = ""
	return plain.action(ctx, 60*time.Second, e.UseContextArgs(name)...)
}

// UseContextArgs is the argv (without the binary) of UseContext.
func (e *Engine) UseContextArgs(name string) []string {
	if e.Kind == "podman" {
		return []string{"system", "connection", "default", name}
	}
	return []string{"context", "use", name}
}
func (e *Engine) Compose(project, dir string, args ...string) services.Command {
	return e.ComposeWithFiles(project, dir, nil, args...)
}

// ComposeWithFiles preserves the project metadata reported on its containers.
func (e *Engine) ComposeWithFiles(project, dir string, files []string, args ...string) services.Command {
	argv := []string{e.Binary, "compose"}
	if project != "" {
		argv = append(argv, "-p", project)
	}
	if dir != "" {
		argv = append(argv, "--project-directory", dir)
	}
	for _, file := range files {
		argv = append(argv, "-f", file)
	}
	argv = append(argv, args...)
	label := "compose: " + project
	return services.Command{Label: label, Argv: argv, Dir: dir}
}

// LogsFollow, Shell, Attach, StatsFollow, and PullCommand produce visible
// terminal jobs; their output is intentionally not captured.
func (e *Engine) LogsFollow(id string) services.Command {
	return services.Command{Label: "logs: " + id, Argv: []string{e.Binary, "logs", "-f", "--tail", "200", id}}
}
func (e *Engine) Shell(id string) services.Command {
	return services.Command{Label: "shell: " + id, Argv: []string{e.Binary, "exec", "-it", id, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}}
}
func (e *Engine) Attach(id string) services.Command {
	return services.Command{Label: "attach: " + id, Argv: []string{e.Binary, "attach", "--sig-proxy=false", id}}
}
func (e *Engine) StatsFollow(id string) services.Command {
	return services.Command{Label: "stats: " + id, Argv: []string{e.Binary, "stats", id}}
}
func (e *Engine) PullCommand(ref string) services.Command {
	return services.Command{Label: "pull: " + ref, Argv: []string{e.Binary, "pull", ref}}
}

// PushCommand produces a visible terminal job for pushing one image.
func (e *Engine) PushCommand(ref string) services.Command {
	return services.Command{Label: "push: " + ref, Argv: []string{e.Binary, "push", ref}}
}

// RunCommand produces a visible, argument-based docker run job.
func (e *Engine) RunCommand(ref, name string, ports, env []string) services.Command {
	args := []string{e.Binary, "run", "--rm", "-it"}
	if name != "" {
		args = append(args, "--name", name)
	}
	for _, port := range ports {
		args = append(args, "-p", port)
	}
	for _, value := range env {
		args = append(args, "-e", value)
	}
	args = append(args, ref)
	return services.Command{Label: "run: " + ref, Argv: args}
}

func (e *Engine) CreateNetwork(ctx context.Context, name string) error {
	return e.action(ctx, 60*time.Second, "network", "create", name)
}

func (e *Engine) CreateVolume(ctx context.Context, name string) error {
	return e.action(ctx, 60*time.Second, "volume", "create", name)
}
