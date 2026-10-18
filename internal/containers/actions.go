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
func (e *Engine) Pull(ctx context.Context, ref string) error {
	return e.action(ctx, 10*time.Minute, "pull", ref)
}
func (e *Engine) Tag(ctx context.Context, src, dst string) error {
	return e.action(ctx, 60*time.Second, "tag", src, dst)
}
func (e *Engine) RemoveImage(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "rmi", id)
}
func (e *Engine) RemoveNetwork(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "network", "rm", id)
}
func (e *Engine) RemoveVolume(ctx context.Context, id string) error {
	return e.action(ctx, 60*time.Second, "volume", "rm", id)
}
func (e *Engine) UseContext(ctx context.Context, name string) error {
	return e.action(ctx, 60*time.Second, "context", "use", name)
}
func (e *Engine) Prune(ctx context.Context, kind string) error {
	switch kind {
	case "containers":
		return e.action(ctx, 60*time.Second, "container", "prune", "-f")
	case "images":
		return e.action(ctx, 60*time.Second, "image", "prune", "-f")
	case "networks":
		return e.action(ctx, 60*time.Second, "network", "prune", "-f")
	case "volumes":
		return e.action(ctx, 60*time.Second, "volume", "prune", "-f")
	case "system":
		return e.action(ctx, 60*time.Second, "system", "prune", "-f")
	default:
		return fmt.Errorf("unknown prune kind: %s", kind)
	}
}
func (e *Engine) Compose(project, dir string, args ...string) services.Command {
	argv := []string{e.Binary, "compose"}
	if project != "" {
		argv = append(argv, "-p", project)
	}
	if dir != "" {
		argv = append(argv, "--project-directory", dir)
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
