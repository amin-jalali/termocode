package containerhost

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/services"
)

// Exec runs commands inside the container: `exec -i -w <dir> [-u user]
// <c> argv…`. dir "" is the workspace root and a relative dir is under
// it. argv goes to the engine as separate arguments: no local shell ever
// reads it.
func (h *Host) Exec() services.Exec { return containerExec{h} }

type containerExec struct{ h *Host }

func (x containerExec) wrap(dir string, argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command")
	}
	return x.h.argv(x.h.execArgs("-i", dir, argv)...), nil
}

// execArgs is "exec <flag> -w <dir> [-u user] <c> argv…".
func (h *Host) execArgs(flag, dir string, argv []string) []string {
	args := []string{"exec", flag, "-w", h.workdir(dir)}
	if h.cfg.User != "" {
		args = append(args, "-u", h.cfg.User)
	}
	args = append(args, h.cfg.Container)
	return append(args, argv...)
}

func (h *Host) workdir(dir string) string {
	if dir == "" {
		return h.cfg.Root
	}
	if !path.IsAbs(dir) {
		return path.Join(h.cfg.Root, dir)
	}
	return path.Clean(dir)
}

// noteStopped marks the host offline when stderr says the container is
// gone or stopped.
func (h *Host) noteStopped(code int, stderr string) {
	if code == 0 {
		return
	}
	low := strings.ToLower(stderr)
	if strings.Contains(low, "is not running") || strings.Contains(low, "no such container") {
		h.setOnline(false)
	}
}

func (x containerExec) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	a, err := x.wrap(dir, argv)
	if err != nil {
		return "", "", -1, err
	}
	out, stderr, code, err := x.h.cfg.X.Capture(ctx, "", a...)
	x.h.noteStopped(code, stderr)
	return out, stderr, code, err
}

func (x containerExec) CaptureInput(ctx context.Context, dir string, input []byte, argv ...string) (string, string, int, error) {
	a, err := x.wrap(dir, argv)
	if err != nil {
		return "", "", -1, err
	}
	out, stderr, code, err := x.h.cfg.X.CaptureInput(ctx, "", input, a...)
	x.h.noteStopped(code, stderr)
	return out, stderr, code, err
}

func (x containerExec) Stream(ctx context.Context, dir string, argv ...string) (io.ReadCloser, func(), error) {
	a, err := x.wrap(dir, argv)
	if err != nil {
		return nil, nil, err
	}
	return x.h.cfg.X.Stream(ctx, "", a...)
}

// TermArgv is the local command line of a terminal tab that runs argv in
// dir inside the container (`exec -it`). No argv opens bash, else sh.
func (h *Host) TermArgv(dir string, argv []string) []string {
	if len(argv) == 0 {
		argv = []string{"sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}
	}
	return h.argv(h.execArgs("-it", dir, argv)...)
}
