package mcp

// Sandbox: an opt-in "sandbox" key on a stdio entry runs the server in a
// container (docker / podman) or under a sandbox tool (bwrap on Linux,
// sandbox-exec on macOS). A server is code from the net; this keeps it
// away from the user's files and the network.
//
//	"sandbox": true                       — the best tool found
//	"sandbox": "container" | "bwrap" | "sandbox-exec"
//	"sandbox": {"kind": "container", "image": "node:22", "cpus": "1",
//	            "memory": "512m", "network": false, "workspace": "ro"}
//
// Defaults: network off, the workspace mounted read-only, the rest of
// the home folder hidden. When no tool is installed the server does not
// start (never a silent run without the sandbox).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Sandbox kinds.
const (
	SandboxAuto        = "auto"
	SandboxContainer   = "container"
	SandboxBwrap       = "bwrap"
	SandboxSandboxExec = "sandbox-exec"
)

// Workspace mount modes.
const (
	WorkspaceRO   = "ro"
	WorkspaceRW   = "rw"
	WorkspaceNone = "none"
)

// Sandbox is the parsed "sandbox" key.
type Sandbox struct {
	Kind      string `json:"kind"`
	Image     string `json:"image"`
	Runtime   string `json:"runtime"` // "docker" or "podman"; "": the first found
	CPUs      string `json:"cpus"`
	Memory    string `json:"memory"`
	Network   bool   `json:"network"`
	Workspace string `json:"workspace"`
}

var (
	cpusRe   = lazyre.New(`^[0-9]+(\.[0-9]+)?$`)
	memoryRe = lazyre.New(`^[0-9]+[bkmgBKMG]?$`)
	imageRe  = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
)

// SandboxSpec reads the entry's "sandbox" key: nil when there is none
// (or it is false).
func (s Server) SandboxSpec() (*Sandbox, error) {
	if len(s.raw) == 0 {
		return nil, nil
	}
	var e struct {
		Sandbox json.RawMessage `json:"sandbox"`
	}
	if err := json.Unmarshal(s.raw, &e); err != nil || len(e.Sandbox) == 0 || string(e.Sandbox) == "null" {
		return nil, nil
	}
	sb := &Sandbox{}
	var on bool
	var kind string
	switch {
	case json.Unmarshal(e.Sandbox, &on) == nil:
		if !on {
			return nil, nil
		}
	case json.Unmarshal(e.Sandbox, &kind) == nil:
		sb.Kind = kind
	default:
		if err := json.Unmarshal(e.Sandbox, sb); err != nil {
			return nil, fmt.Errorf("sandbox: %v", err)
		}
	}
	if err := sb.check(); err != nil {
		return nil, err
	}
	if s.Type != "" && s.Type != TypeStdio {
		return nil, errors.New("sandbox: only local (stdio) servers can run in a sandbox")
	}
	return sb, nil
}

func (sb *Sandbox) check() error {
	sb.Kind = strings.ToLower(strings.TrimSpace(sb.Kind))
	switch sb.Kind {
	case "", "true":
		sb.Kind = SandboxAuto
	case "docker", "podman":
		sb.Runtime, sb.Kind = sb.Kind, SandboxContainer
	case SandboxAuto, SandboxContainer, SandboxBwrap, SandboxSandboxExec:
	default:
		return fmt.Errorf("sandbox: unknown kind %q (use container, bwrap or sandbox-exec)", sb.Kind)
	}
	switch sb.Runtime {
	case "", "docker", "podman":
	default:
		return fmt.Errorf("sandbox: unknown runtime %q (use docker or podman)", sb.Runtime)
	}
	switch sb.Workspace {
	case "":
		sb.Workspace = WorkspaceRO
	case WorkspaceRO, WorkspaceRW, WorkspaceNone:
	default:
		return fmt.Errorf("sandbox: workspace must be ro, rw or none")
	}
	if sb.CPUs != "" && !cpusRe.MatchString(sb.CPUs) {
		return fmt.Errorf("sandbox: cpus must be a number like 1 or 0.5")
	}
	if sb.Memory != "" && !memoryRe.MatchString(sb.Memory) {
		return fmt.Errorf("sandbox: memory must look like 512m or 2g")
	}
	if sb.Image != "" && !imageRe.MatchString(sb.Image) {
		return fmt.Errorf("sandbox: image %q is not a valid image name", sb.Image)
	}
	if sb.Kind == SandboxContainer && sb.Image == "" {
		return errors.New("sandbox: a container needs an image (\"image\": \"node:22\")")
	}
	return nil
}

// Describe is the short state text of the sandbox ("container (docker,
// node:22) · network off · workspace read-only · 1 CPU · 512m").
func (sb *Sandbox) Describe(tool string) string {
	parts := []string{}
	switch sb.Kind {
	case SandboxContainer:
		if tool == "" {
			parts = append(parts, fmt.Sprintf("container (%s)", sb.Image))
		} else {
			parts = append(parts, fmt.Sprintf("container (%s, %s)", tool, sb.Image))
		}
	default:
		parts = append(parts, tool)
	}
	if sb.Network {
		parts = append(parts, "network on")
	} else {
		parts = append(parts, "network off")
	}
	switch sb.Workspace {
	case WorkspaceRW:
		parts = append(parts, "workspace read-write")
	case WorkspaceNone:
		parts = append(parts, "no workspace")
	default:
		parts = append(parts, "workspace read-only")
	}
	if sb.CPUs != "" {
		parts = append(parts, sb.CPUs+" CPU")
	}
	if sb.Memory != "" {
		parts = append(parts, sb.Memory)
	}
	return strings.Join(parts, " · ")
}

// sandboxText is the status row's sandbox text: "" when the entry has
// none, else the planned sandbox ("bwrap · network off · workspace
// read-only") or its problem.
func sandboxText(s Server) string {
	sb, err := s.SandboxSpec()
	switch {
	case err != nil:
		return err.Error()
	case sb == nil:
		return ""
	}
	if sb.Kind == SandboxAuto && sb.Image != "" {
		sb.Kind = SandboxContainer // what applySandbox picks when a runtime is installed
	}
	tool := sb.Runtime
	if sb.Kind != SandboxContainer {
		tool = sb.Kind
	}
	return "sandbox: " + sb.Describe(tool)
}

// errNoSandboxTool is the clear message when nothing is installed.
func errNoSandboxTool(goos string) error {
	hint := "install docker or podman and set \"image\""
	switch goos {
	case "linux":
		hint = "install bubblewrap (bwrap), docker or podman"
	case "darwin":
		hint = "sandbox-exec is missing; install docker or podman and set \"image\""
	}
	return fmt.Errorf("sandbox: no sandbox tool found (%s); the server was not started", hint)
}

// sandboxEnv is what applySandbox needs from the machine (fakes in tests).
type sandboxEnv struct {
	look func(string) (string, error)
	goos string
	home string
	tmp  string
}

func hostSandboxEnv() sandboxEnv {
	home, _ := os.UserHomeDir()
	return sandboxEnv{look: exec.LookPath, goos: runtime.GOOS, home: home, tmp: os.TempDir()}
}

// ApplySandbox returns s rewritten to start inside its sandbox, and the
// state text. s without a "sandbox" key comes back as is (note "").
// roots are the workspace folders; the first one is mounted.
func ApplySandbox(s Server, roots []string) (Server, string, error) {
	return applySandbox(s, roots, hostSandboxEnv())
}

func applySandbox(s Server, roots []string, env sandboxEnv) (Server, string, error) {
	sb, err := s.SandboxSpec()
	if err != nil || sb == nil {
		return s, "", err
	}
	ws := ""
	if len(roots) > 0 && sb.Workspace != WorkspaceNone {
		ws = roots[0]
	}
	kind := sb.Kind
	if kind == SandboxAuto {
		kind = pickSandbox(sb, env)
		if kind == "" {
			return s, "", errNoSandboxTool(env.goos)
		}
	}
	var tool string
	switch kind {
	case SandboxContainer:
		tool, err = containerRuntime(sb, env)
		if err != nil {
			return s, "", err
		}
		s = wrapContainer(s, sb, tool, ws)
	case SandboxBwrap:
		p, lerr := env.look("bwrap")
		if lerr != nil || env.goos != "linux" {
			return s, "", errNoSandboxTool(env.goos)
		}
		tool = "bwrap"
		s = wrapBwrap(s, sb, p, ws, env)
	case SandboxSandboxExec:
		p, lerr := env.look("sandbox-exec")
		if lerr != nil || env.goos != "darwin" {
			return s, "", errNoSandboxTool(env.goos)
		}
		tool = "sandbox-exec"
		s = wrapSandboxExec(s, sb, p, ws, env)
	}
	note := "sandbox: " + sb.Describe(tool)
	if kind != SandboxContainer && (sb.CPUs != "" || sb.Memory != "") {
		note += " (cpu/memory limits need a container; not applied)"
	}
	return s, note, nil
}

// pickSandbox: a container when an image is set, else the OS tool, else
// "" (nothing installed).
func pickSandbox(sb *Sandbox, env sandboxEnv) string {
	if sb.Image != "" {
		if _, err := containerRuntime(sb, env); err == nil {
			return SandboxContainer
		}
	}
	switch env.goos {
	case "linux":
		if _, err := env.look("bwrap"); err == nil {
			return SandboxBwrap
		}
	case "darwin":
		if _, err := env.look("sandbox-exec"); err == nil {
			return SandboxSandboxExec
		}
	}
	return ""
}

func containerRuntime(sb *Sandbox, env sandboxEnv) (string, error) {
	names := []string{"docker", "podman"}
	if sb.Runtime != "" {
		names = []string{sb.Runtime}
	}
	for _, n := range names {
		if _, err := env.look(n); err == nil {
			return n, nil
		}
	}
	return "", fmt.Errorf("sandbox: %s not found; install it or remove \"sandbox\"; the server was not started", strings.Join(names, " or "))
}

// wrapContainer: `docker run -i --rm …  image command args`. The entry's
// env goes through as `-e NAME` (the value stays in the environment of
// the docker CLI, never in argv).
func wrapContainer(s Server, sb *Sandbox, tool, ws string) Server {
	args := []string{"run", "-i", "--rm", "--init"}
	if !sb.Network {
		args = append(args, "--network", "none")
	}
	if sb.CPUs != "" {
		args = append(args, "--cpus", sb.CPUs)
	}
	if sb.Memory != "" {
		args = append(args, "--memory", sb.Memory)
	}
	if ws != "" {
		mode := "ro"
		if sb.Workspace == WorkspaceRW {
			mode = "rw"
		}
		args = append(args, "-v", ws+":"+ws+":"+mode, "-w", ws)
	}
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", k)
	}
	args = append(args, sb.Image, s.Command)
	s.Args = append(args, s.Args...)
	s.Command = tool
	return s
}

// wrapBwrap: the whole file system read-only, the home folder hidden
// (tmpfs), the workspace bound back (ro or rw), all namespaces unshared
// (network only when allowed).
func wrapBwrap(s Server, sb *Sandbox, bwrap, ws string, env sandboxEnv) Server {
	args := []string{"--die-with-parent", "--new-session", "--unshare-all"}
	if sb.Network {
		args = append(args, "--share-net")
	}
	args = append(args, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
	if env.home != "" {
		args = append(args, "--tmpfs", env.home)
	}
	if ws != "" {
		bind := "--ro-bind"
		if sb.Workspace == WorkspaceRW {
			bind = "--bind"
		}
		args = append(args, bind, ws, ws, "--chdir", ws)
	}
	args = append(args, s.Command)
	s.Args = append(args, s.Args...)
	s.Command = bwrap
	return s
}

// wrapSandboxExec: an inline profile that denies the network (unless
// allowed) and every write outside the temp folder and a read-write
// workspace.
func wrapSandboxExec(s Server, sb *Sandbox, tool, ws string, env sandboxEnv) Server {
	var p strings.Builder
	p.WriteString("(version 1)(allow default)")
	if !sb.Network {
		p.WriteString("(deny network*)(allow network* (local unix))")
	}
	p.WriteString("(deny file-write*)(allow file-write* (literal \"/dev/null\") (literal \"/dev/tty\")")
	if env.tmp != "" {
		fmt.Fprintf(&p, " (subpath %q)", env.tmp)
	}
	if ws != "" && sb.Workspace == WorkspaceRW {
		fmt.Fprintf(&p, " (subpath %q)", ws)
	}
	p.WriteString(")")
	args := []string{"-p", p.String(), s.Command}
	s.Args = append(args, s.Args...)
	s.Command = tool
	if ws != "" && s.Cwd == "" {
		s.Cwd = ws
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
