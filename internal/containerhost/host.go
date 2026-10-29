package containerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// DefaultTimeout bounds one engine call of a file operation.
const DefaultTimeout = 10 * time.Second

// Config says which container folder to open and how to reach it.
type Config struct {
	// Binary is the engine CLI ("" = "docker").
	Binary string
	// Kind is "docker" or "podman" ("" = from Binary's base name).
	Kind string
	// Context is the engine context (docker) or connection (podman);
	// "" = the user's default. It is passed as a flag on each call and
	// never changes the user's default.
	Context string
	// Container is the container's name or ID.
	Container string
	// Root is the absolute workspace folder inside the container.
	Root string
	// User runs commands and file operations ("" = the container's own).
	User string
	// X runs the engine CLI (nil = services.SpawnExec{}).
	X services.Exec
	// Timeout bounds one file operation (0 = DefaultTimeout).
	Timeout time.Duration
}

// ErrNotRunning is returned when the container is stopped or gone.
var ErrNotRunning = errors.New("the container is not running")

// Host is a workspace Host inside one container.
type Host struct {
	cfg Config

	mu     sync.Mutex
	online bool
	image  string
	cuser  string // Config.User of the container
	mounts []mount
}

type mount struct {
	Type        string
	Source      string
	Destination string
	RW          bool
}

// New checks that the container runs and reads what it needs about it.
func New(ctx context.Context, cfg Config) (*Host, error) {
	if cfg.Binary == "" {
		cfg.Binary = "docker"
	}
	if cfg.Kind == "" {
		cfg.Kind = "docker"
		if strings.HasPrefix(filepath.Base(cfg.Binary), "podman") {
			cfg.Kind = "podman"
		}
	}
	if cfg.X == nil {
		cfg.X = services.SpawnExec{}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Container == "" || strings.HasPrefix(cfg.Container, "-") || strings.ContainsAny(cfg.Container, ":/ \t\n") {
		return nil, fmt.Errorf("not a container name: %q", cfg.Container)
	}
	if strings.HasPrefix(cfg.Context, "-") {
		return nil, fmt.Errorf("not a context name: %q", cfg.Context)
	}
	if !path.IsAbs(cfg.Root) {
		return nil, fmt.Errorf("the folder in the container must be absolute: %q", cfg.Root)
	}
	cfg.Root = path.Clean(cfg.Root)
	h := &Host{cfg: cfg}
	if err := h.inspect(ctx); err != nil {
		return nil, err
	}
	if !h.Online() {
		return nil, ErrNotRunning
	}
	return h, nil
}

// ID is the workspace ID: "container:<context>/<name>:<root>"; the
// context is "default" when none is set.
func (h *Host) ID() string {
	c := h.cfg.Context
	if c == "" {
		c = "default"
	}
	return string(workspace.KindContainer) + ":" + c + "/" + h.cfg.Container + ":" + h.cfg.Root
}

// Label is the container's name, for the status bar.
func (h *Host) Label() string { return h.cfg.Container }

// Online reports whether the container ran at the last check.
func (h *Host) Online() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.online
}

func (h *Host) setOnline(v bool) {
	h.mu.Lock()
	h.online = v
	h.mu.Unlock()
}

// argv is the engine command line for args, with the context flag.
func (h *Host) argv(args ...string) []string {
	a := []string{h.cfg.Binary}
	if h.cfg.Context != "" {
		if h.cfg.Kind == "podman" {
			a = append(a, "--connection", h.cfg.Context)
		} else {
			a = append(a, "--context", h.cfg.Context)
		}
	}
	return append(a, args...)
}

// call runs one engine command within the per-call time limit.
func (h *Host) call(ctx context.Context, input []byte, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, h.cfg.Timeout)
	defer cancel()
	if input != nil {
		return h.cfg.X.CaptureInput(ctx, "", input, h.argv(args...)...)
	}
	return h.cfg.X.Capture(ctx, "", h.argv(args...)...)
}

// engineErr turns a failed engine call into one short error. A gone or
// stopped container marks the host offline and wraps ErrNotRunning; a
// missing file wraps fs.ErrNotExist.
func (h *Host) engineErr(op, p, stderr string, code int) error {
	msg := strings.TrimSpace(stderr)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "no such container") || strings.Contains(low, "is not running"):
		h.setOnline(false)
		return &fs.PathError{Op: op, Path: p, Err: ErrNotRunning}
	case strings.Contains(low, "could not find the file") || strings.Contains(low, "no such file or directory"):
		return &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist}
	case msg == "":
		return &fs.PathError{Op: op, Path: p, Err: fmt.Errorf("engine command exited %d", code)}
	}
	return &fs.PathError{Op: op, Path: p, Err: errors.New(msg)}
}

// inspect reads the container's state, user, image and mounts.
func (h *Host) inspect(ctx context.Context) error {
	out, stderr, code, err := h.call(ctx, nil, "inspect", "--type", "container", h.cfg.Container)
	if err != nil {
		return err
	}
	if code != 0 {
		err := h.engineErr("inspect", h.cfg.Container, stderr, code)
		if errors.Is(err, ErrNotRunning) {
			return ErrNotRunning
		}
		return err
	}
	var items []struct {
		State struct {
			Running bool
		}
		Config struct {
			User  string
			Image string
		}
		Image  string
		Mounts []mount
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) == 0 {
		return fmt.Errorf("cannot read the container's details")
	}
	it := items[0]
	h.mu.Lock()
	h.online = it.State.Running
	h.image = it.Image
	if h.image == "" {
		h.image = it.Config.Image
	}
	h.cuser = it.Config.User
	h.mounts = it.Mounts
	h.mu.Unlock()
	return nil
}
