package containers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

const defaultTimeout = 10 * time.Second

// Engine describes the detected CLI and the last successful daemon probe.
type Engine struct {
	Binary    string
	Kind      string
	Version   Version
	Reachable bool
	Problem   services.Problem
	X         services.Exec
	Dir       string
	Timeout   time.Duration
	// Context is the per-session DOCKER_CONTEXT ("" = leave the
	// environment alone). It never changes the user's global default.
	Context string
}

// ErrorMessage explains why the host engine is unavailable.
func (e Engine) ErrorMessage() string {
	if e.Binary == "" {
		return "no container engine installed (Docker or Podman)"
	}
	switch e.Problem.Kind {
	case services.ProblemDaemonDown:
		return e.Kind + " daemon is not running"
	case services.ProblemNoPermission:
		return "permission denied accessing the " + e.Kind + " daemon"
	case services.ProblemNone:
		return ""
	default:
		if e.Problem.Detail != "" {
			return e.Problem.Detail
		}
		return e.Kind + " engine is unavailable"
	}
}

type DetectOptions struct {
	Preferred string
	Android   bool
	LookPath  func(string) (string, error)
	Exec      services.Exec
	Dir       string
	Context   string
	Timeout   time.Duration // per call; 0 = 10 s
}

// Detect selects docker before podman, then probes its version and daemon.
func Detect(ctx context.Context, opts DetectOptions) Engine {
	if opts.Android {
		return Engine{}
	}
	lp := opts.LookPath
	if lp == nil {
		lp = exec.LookPath
	}
	x := opts.Exec
	if x == nil {
		x = services.SpawnExec{}
	}
	// containers_engine: auto (docker, then podman) or one named engine.
	names := []string{"docker", "podman"}
	if opts.Preferred == "docker" || opts.Preferred == "podman" {
		names = []string{opts.Preferred}
	}
	e := Engine{X: x, Dir: opts.Dir, Timeout: defaultTimeout, Context: opts.Context}
	if opts.Timeout > 0 {
		e.Timeout = opts.Timeout
	}
	for _, name := range names {
		path, err := lp(name)
		if err == nil {
			e.Binary, e.Kind = path, name
			break
		}
	}
	if e.Binary == "" {
		e.Problem = services.Problem{Kind: services.ProblemNoDocker}
		return e
	}
	out, stderr, code, err := e.capture(ctx, "version", "--format", "{{json .}}")
	if err != nil || code != 0 {
		e.Problem = classifyEngineError(stderr, err)
		return e
	}
	e.Version, err = ParseVersion([]byte(out))
	if err != nil {
		e.Problem = services.Problem{Kind: services.ProblemOther, Detail: "cannot read engine version"}
		return e
	}
	e.Reachable = true
	return e
}

func classifyEngineError(stderr string, err error) services.Problem {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return services.Problem{Kind: services.ProblemDaemonDown, Detail: "engine version timed out"}
		}
		if errors.Is(err, exec.ErrNotFound) || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			return services.Problem{Kind: services.ProblemNoDocker}
		}
	}
	p := services.ProblemFromStderr(stderr)
	if p.Kind == services.ProblemNone {
		p = services.Problem{Kind: services.ProblemOther, Detail: strings.TrimSpace(stderr)}
	}
	return p
}

func (e *Engine) capture(parent context.Context, args ...string) (string, string, int, error) {
	return e.captureTimeout(parent, e.Timeout, args...)
}

// exec is X with DOCKER_CONTEXT set when the session uses a context.
func (e *Engine) exec() services.Exec {
	x := e.X
	if x == nil {
		x = services.SpawnExec{}
	}
	if e.Context != "" && e.Kind != "podman" {
		x = services.WithEnv(x, "DOCKER_CONTEXT="+e.Context)
	}
	return x
}

func (e *Engine) captureTimeout(parent context.Context, timeout time.Duration, args ...string) (string, string, int, error) {
	d := timeout
	if d <= 0 {
		d = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	return e.exec().Capture(ctx, e.Dir, append([]string{e.Binary}, args...)...)
}

func (e *Engine) run(ctx context.Context, args ...string) error {
	_, stderr, code, err := e.capture(ctx, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		p := services.ProblemFromStderr(stderr)
		if p.Kind == services.ProblemNone {
			return fmt.Errorf("engine command exited %d", code)
		}
		return errors.New(p.Message())
	}
	return nil
}

func (e *Engine) list(ctx context.Context, args ...string) (string, error) {
	out, stderr, code, err := e.capture(ctx, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		p := services.ProblemFromStderr(stderr)
		if p.Kind == services.ProblemNone {
			return "", fmt.Errorf("engine command exited %d", code)
		}
		return "", errors.New(p.Message())
	}
	return out, nil
}

func parseList[T any](ctx context.Context, e *Engine, args []string, parse func([]byte) ([]T, error)) ([]T, error) {
	out, err := e.list(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parse([]byte(out))
}

// Watch starts the host-wide event stream. The returned channel closes after
// cancellation or stream termination; cancel always stops and waits for it.
func (e *Engine) Watch(ctx context.Context) (<-chan Event, func(), error) {
	watchCtx, cancel := context.WithCancel(ctx)
	x := e.exec()
	args := []string{e.Binary, "events", "--format", "{{json .}}"}
	out, stop, err := x.Stream(watchCtx, e.Dir, args...)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		defer cancel()
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 4096), 4*1024*1024)
		for s.Scan() {
			items, err := ParseEvents(s.Bytes())
			if err != nil {
				continue
			}
			for _, ev := range items {
				select {
				case ch <- ev:
				case <-watchCtx.Done():
					return
				default:
				}
			}
		}
		stop()
	}()
	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			cancel()
			stop()
		})
	}
	return ch, stopFn, nil
}
