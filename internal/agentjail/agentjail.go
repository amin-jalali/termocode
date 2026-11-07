// Package agentjail puts an outside agent (Claude Code, Codex, …) inside
// an OS sandbox for one lane (CMP-60): bwrap on Linux, sandbox-exec on
// macOS. The whole disk is read-only; only the lane's folder (in
// workspace-write mode), the temp folder, the agent's own config
// folders (its login and session state) and, for a git worktree, its
// own git folder plus the shared objects and refs are writable. Hooks
// and the repo config stay read-only, so the agent cannot plant code
// that runs later outside the sandbox. The network stays on: the agent
// talks to its own API. No sandbox tool: Wrap fails (never a silent run
// outside the sandbox).
//
// The sandbox limits writes, not reads: the agent can still read the
// user's files, as it can without it.
package agentjail

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Modes.
const (
	ReadOnly       = "read-only"
	WorkspaceWrite = "workspace-write"
)

// Tools.
const (
	ToolBwrap       = "bwrap"
	ToolSandboxExec = "sandbox-exec"
)

// ErrNoTool is the error when the machine has no sandbox tool.
var ErrNoTool = errors.New("no sandbox tool")

// Plan is one agent's sandbox.
type Plan struct {
	Mode  string // ReadOnly or WorkspaceWrite
	Dir   string // the lane's folder (absolute)
	Agent string // agents id ("claude", "codex", …) or ai CLI name; picks the config folders
	// Interactive: the agent runs in a terminal tab, so it keeps the
	// terminal's session (Ctrl-C, job control). Off, bwrap starts a new
	// session (no typing into the parent terminal).
	Interactive bool
}

// Host is what Wrap needs from the machine (fakes in tests).
type Host struct {
	Look func(string) (string, error)
	GOOS string
	Home string
	Tmp  string
}

// HostEnv is the real machine.
func HostEnv() Host {
	home, _ := os.UserHomeDir()
	return Host{Look: exec.LookPath, GOOS: runtime.GOOS, Home: home, Tmp: os.TempDir()}
}

// writable is one path the agent may write.
type writable struct {
	path   string
	prefix bool // a file whose siblings with the same prefix (temp, backup) are writable too
}

// configPaths are the agent's own state folders and files, relative to
// the home folder. Every agent also gets .cache.
var configPaths = map[string][]writable{
	"claude":   {{path: ".claude"}, {path: ".claude.json", prefix: true}},
	"codex":    {{path: ".codex"}},
	"gemini":   {{path: ".gemini"}},
	"opencode": {{path: ".config/opencode"}, {path: ".local/share/opencode"}, {path: ".local/state/opencode"}},
	"aider":    {{path: ".aider"}, {path: ".aider.chat.history.md", prefix: true}},
	"cursor":   {{path: ".cursor"}},
	"amp":      {{path: ".config/amp"}, {path: ".local/share/amp"}},
	"goose":    {{path: ".config/goose"}, {path: ".local/share/goose"}, {path: ".local/state/goose"}},
	"qwen":     {{path: ".qwen"}},
	"crush":    {{path: ".config/crush"}, {path: ".local/share/crush"}},
	"copilot":  {{path: ".copilot"}, {path: ".config/github-copilot"}},
	"kimi":     {{path: ".kimi"}},
	"droid":    {{path: ".factory"}},
	"pi":       {{path: ".pi"}},
}

// agentKey maps an ai CLI name to the agents id.
func agentKey(a string) string {
	if a == "claude-code" {
		return "claude"
	}
	return a
}

// Pick is the sandbox tool for this machine, or ErrNoTool.
func Pick(h Host) (tool, path string, err error) {
	switch h.GOOS {
	case "linux":
		if p, err := h.Look(ToolBwrap); err == nil {
			return ToolBwrap, p, nil
		}
		return "", "", fmt.Errorf("agent sandbox: bwrap is not installed; install bubblewrap or set agent_os_sandbox to false; the agent was not started: %w", ErrNoTool)
	case "darwin":
		if p, err := h.Look(ToolSandboxExec); err == nil {
			return ToolSandboxExec, p, nil
		}
		return "", "", fmt.Errorf("agent sandbox: sandbox-exec is missing; set agent_os_sandbox to false; the agent was not started: %w", ErrNoTool)
	}
	return "", "", fmt.Errorf("agent sandbox: not available on %s; set agent_os_sandbox to false; the agent was not started: %w", h.GOOS, ErrNoTool)
}

// check validates the plan.
func (p Plan) check() error {
	if p.Mode != ReadOnly && p.Mode != WorkspaceWrite {
		return fmt.Errorf("agent sandbox: unknown mode %q (use %s or %s)", p.Mode, ReadOnly, WorkspaceWrite)
	}
	if p.Dir == "" || !filepath.IsAbs(p.Dir) {
		return fmt.Errorf("agent sandbox: the folder %q is not an absolute path", p.Dir)
	}
	return nil
}

// Wrap returns the program and arguments that run bin args inside the
// sandbox of p.
func Wrap(p Plan, h Host, bin string, args []string) (string, []string, error) {
	if err := p.check(); err != nil {
		return "", nil, err
	}
	tool, path, err := Pick(h)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Clean(p.Dir)
	w := p.writables(h, dir)
	switch tool {
	case ToolBwrap:
		return path, bwrapArgs(p, h, dir, w, bin, args), nil
	default:
		if p.Mode == WorkspaceWrite {
			w = append(w, writable{path: dir}) // bwrap binds dir itself; the profile must list it
		}
		out := []string{"-p", seatbeltProfile(h, w), bin}
		return path, append(out, args...), nil
	}
}

// Line is the shell line that runs bin inside the sandbox, every word
// single-quoted (typed into a lane's terminal).
func Line(p Plan, h Host, bin string) (string, error) {
	prog, args, err := Wrap(p, h, bin, nil)
	if err != nil {
		return "", err
	}
	words := make([]string, 0, len(args)+1)
	words = append(words, quote(prog))
	for _, a := range args {
		words = append(words, quote(a))
	}
	return strings.Join(words, " "), nil
}

// Describe is the short state text ("sandbox: bwrap · worktree
// writable · network on").
func Describe(p Plan, tool string) string {
	what := "read-only"
	if p.Mode == WorkspaceWrite {
		what = "worktree writable"
	}
	return "sandbox: " + tool + " · " + what + " · network on"
}

// Note is Describe with the tool this machine would use ("" when none).
func Note(p Plan, h Host) string {
	tool, _, err := Pick(h)
	if err != nil {
		return ""
	}
	return Describe(p, tool)
}

// writables are the extra writable paths besides the temp folder: the
// lane folder (workspace-write), its git dirs, the agent's config.
func (p Plan) writables(h Host, dir string) []writable {
	var out []writable
	if h.Home != "" {
		for _, c := range append(configPaths[agentKey(p.Agent)], writable{path: ".cache"}) {
			out = append(out, writable{path: filepath.Join(h.Home, c.path), prefix: c.prefix})
		}
	}
	if p.Mode == WorkspaceWrite {
		for _, g := range gitDirs(dir) {
			out = append(out, writable{path: g})
		}
	}
	return out
}

// gitDirs: for a linked git worktree (dir/.git is a "gitdir: X" file),
// its own git folder and the shared objects, refs and logs. Hooks and
// config are left out on purpose. A normal repo (dir/.git a folder) is
// inside dir already.
func gitDirs(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return nil
	}
	gd, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return nil
	}
	gd = strings.TrimSpace(gd)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	gd = filepath.Clean(gd)
	out := []string{gd}
	c, err := os.ReadFile(filepath.Join(gd, "commondir"))
	if err != nil {
		return out
	}
	common := strings.TrimSpace(string(c))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gd, common)
	}
	common = filepath.Clean(common)
	for _, s := range []string{"objects", "refs", "logs", "packed-refs"} {
		out = append(out, filepath.Join(common, s))
	}
	return out
}

// bwrapArgs: the whole disk read-only, fresh /dev /proc /tmp, the
// writable paths bound back (missing ones skipped), the lane folder
// bound rw or ro, every namespace unshared but the network.
func bwrapArgs(p Plan, h Host, dir string, w []writable, bin string, args []string) []string {
	out := []string{"--die-with-parent"}
	if !p.Interactive {
		out = append(out, "--new-session")
	}
	out = append(out, "--unshare-all", "--share-net",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
	if t := filepath.Clean(h.Tmp); h.Tmp != "" && t != "/tmp" {
		out = append(out, "--bind-try", t, t)
	}
	for _, x := range w {
		out = append(out, "--bind-try", x.path, x.path)
	}
	bind := "--ro-bind"
	if p.Mode == WorkspaceWrite {
		bind = "--bind"
	}
	out = append(out, bind, dir, dir, "--chdir", dir, "--", bin)
	return append(out, args...)
}

// seatbeltProfile: everything allowed but writes, which are denied
// except to the terminal, the temp folders and the writable paths.
func seatbeltProfile(h Host, w []writable) string {
	var b strings.Builder
	b.WriteString("(version 1)(allow default)(deny file-write*)")
	b.WriteString(`(allow file-write* (literal "/dev/null") (literal "/dev/tty") (regex #"^/dev/ttys[0-9]+$") (regex #"^/dev/fd/")`)
	b.WriteString(` (subpath "/tmp") (subpath "/private/tmp") (subpath "/private/var/folders")`)
	if h.Tmp != "" {
		fmt.Fprintf(&b, " (subpath %q)", filepath.Clean(h.Tmp))
	}
	for _, x := range w {
		if strings.ContainsAny(x.path, "\"\\") {
			continue // never a profile string we cannot write safely
		}
		if x.prefix {
			b.WriteString(` (regex #"^` + regexQuote(x.path) + `")`)
		} else {
			fmt.Fprintf(&b, " (subpath %q)", x.path)
		}
	}
	b.WriteString(")")
	return b.String()
}

// regexQuote escapes the regex specials of a path.
func regexQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
