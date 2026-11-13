package containerhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The tools sidecar (plan RC, phase 6): a second, disposable container
// next to the target, from the target's own image (same libc and
// toolchain), that shares its volumes, network and processes and gets
// termocode's tools from a read-only volume. The debugger runs there, so
// nothing is ever put in the target. v1: docker only, and only for a
// bind-mounted (or volume-backed) workspace: the writable layer would
// need CAP_SYS_PTRACE and /proc/1/root.

// SidecarLabel marks a sidecar with its workspace key; old ones are found
// by it and removed.
const SidecarLabel = "termocode.sidecar"

// SidecarTools is where the tools volume is mounted in the sidecar.
const SidecarTools = "/opt/termocode"

var (
	// ErrSidecarNeedsMount: the workspace folder is in the container's
	// own writable layer.
	ErrSidecarNeedsMount = errors.New("the tools sidecar works only for a folder that is a bind mount or a volume in the container")
	// ErrSidecarPodman: podman is left out of v1.
	ErrSidecarPodman = errors.New("the tools sidecar works only with docker for now")
	// ErrNoToolsVolume: termocode's tools volume for this CPU and libc
	// is not on the engine.
	ErrNoToolsVolume = errors.New("termocode's tools volume is not on this engine")
)

// SidecarName is the sidecar container's name for a workspace key.
func SidecarName(key string) string { return "termocode-sc-" + key }

// ToolsVolume is the tools volume of a CPU and libc.
func ToolsVolume(arch, libc string) string { return "termocode-tools-" + arch + "-" + libc }

// Sidecar is a running tools sidecar.
type Sidecar struct {
	Name string
	h    *Host
}

// rootMounted reports whether the workspace root lies in a bind mount or
// a volume of the target (not its writable layer).
func (h *Host) rootMounted() bool {
	h.mu.Lock()
	mounts := append([]mount(nil), h.mounts...)
	h.mu.Unlock()
	root := h.cfg.Root
	for _, m := range mounts {
		dst := path.Clean(m.Destination)
		if (m.Type == "bind" || m.Type == "volume") && (dst == root || dst == "/" || strings.HasPrefix(root, dst+"/")) {
			return true
		}
	}
	return false
}

// sidecarReady says why no sidecar can run, or the tools volume name.
func (h *Host) sidecarReady() (string, error) {
	if h.cfg.Kind == "podman" {
		return "", ErrSidecarPodman
	}
	if !h.rootMounted() {
		return "", ErrSidecarNeedsMount
	}
	c := h.Caps()
	if c.Arch == "" || c.Libc == "" {
		return "", errors.New("the container's CPU or C library is not known, so no tools fit it")
	}
	return ToolsVolume(c.Arch, c.Libc), nil
}

// SidecarArgv is the `run` of the sidecar for workspace key, its env in
// envFile.
func (h *Host) SidecarArgv(key, envFile string) ([]string, error) {
	vol, err := h.sidecarReady()
	if err != nil {
		return nil, err
	}
	if key == "" || strings.HasPrefix(key, "-") {
		return nil, fmt.Errorf("bad sidecar key %q", key)
	}
	uid, gid, ok := h.Owner()
	if !ok {
		return nil, errors.New("the container's user is not known")
	}
	h.mu.Lock()
	image := h.image
	h.mu.Unlock()
	if image == "" || strings.HasPrefix(image, "-") {
		return nil, errors.New("the container's image is not known")
	}
	t := h.cfg.Container
	return h.argv("run", "-d", "--name", SidecarName(key),
		"--label", SidecarLabel+"="+key,
		"--volumes-from", t, "--network", "container:"+t, "--pid", "container:"+t,
		"--user", strconv.Itoa(uid)+":"+strconv.Itoa(gid), "--env-file", envFile,
		"-v", vol+":"+SidecarTools+":ro",
		"--entrypoint", SidecarTools+"/bin/tc-sleep", image), nil
}

// StartSidecar starts the sidecar of workspace key with env (passed
// only through a 0600 env-file, removed after the run). An old sidecar
// of the same key is removed first.
func (h *Host) StartSidecar(ctx context.Context, key string, env map[string]string) (*Sidecar, error) {
	vol, err := h.sidecarReady()
	if err != nil {
		return nil, err
	}
	for k, v := range env {
		if k == "" || strings.ContainsAny(k, "=\n\r") || strings.ContainsAny(v, "\n\r") {
			return nil, fmt.Errorf("the variable %q cannot be passed to the sidecar", k)
		}
	}
	if _, stderr, code, err := h.call(ctx, nil, "volume", "inspect", vol); err != nil {
		return nil, err
	} else if code != 0 {
		return nil, fmt.Errorf("%w (%s): %s", ErrNoToolsVolume, vol, strings.TrimSpace(stderr))
	}
	if err := h.removeSidecars(ctx, key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "termocode-sc-*.env")
	if err != nil {
		return nil, err
	}
	envFile := f.Name()
	defer func() { _ = os.Remove(envFile) }()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	argv, err := h.SidecarArgv(key, envFile)
	if err != nil {
		return nil, err
	}
	_, stderr, code, err := h.cfg.X.Capture(ctx, "", argv...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("the tools sidecar did not start: %s", strings.TrimSpace(stderr))
	}
	return &Sidecar{Name: SidecarName(key), h: h}, nil
}

// removeSidecars removes every sidecar labelled with key.
func (h *Host) removeSidecars(ctx context.Context, key string) error {
	out, stderr, code, err := h.call(ctx, nil, "ps", "-aq", "--filter", "label="+SidecarLabel+"="+key)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("cannot list old sidecars: %s", strings.TrimSpace(stderr))
	}
	for _, id := range strings.Fields(out) {
		if strings.HasPrefix(id, "-") {
			continue
		}
		if _, stderr, code, err := h.call(ctx, nil, "rm", "-f", id); err != nil {
			return err
		} else if code != 0 {
			return fmt.Errorf("cannot remove the old sidecar %s: %s", id, strings.TrimSpace(stderr))
		}
	}
	return nil
}

// ExecArgv runs argv in the sidecar with stdin open (a debug adapter).
func (s *Sidecar) ExecArgv(argv ...string) []string {
	return s.h.argv(append([]string{"exec", "-i", s.Name}, argv...)...)
}

// Stop removes the sidecar (a gone one is fine).
func (s *Sidecar) Stop(ctx context.Context) error {
	_, stderr, code, err := s.h.call(ctx, nil, "rm", "-f", s.Name)
	if err != nil {
		return err
	}
	if code != 0 && !strings.Contains(strings.ToLower(stderr), "no such container") {
		return fmt.Errorf("cannot remove the tools sidecar: %s", strings.TrimSpace(stderr))
	}
	return nil
}
