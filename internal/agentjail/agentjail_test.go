package agentjail

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeLook(have ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, h := range have {
			if h == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func host(goos string, have ...string) Host {
	return Host{Look: fakeLook(have...), GOOS: goos, Home: "/home/u", Tmp: "/tmp"}
}

// argPair: args holds flag followed by a.
func argPair(args []string, flag, a string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == a {
			return true
		}
	}
	return false
}

// argTriple: args holds flag followed by a and b.
func argTriple(args []string, flag, a, b string) bool {
	for i := 0; i+2 < len(args); i++ {
		if args[i] == flag && args[i+1] == a && args[i+2] == b {
			return true
		}
	}
	return false
}

func has(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// Linux, workspace-write: the disk is read-only, the worktree is bound
// back read-write, the agent's own config folders are writable, the
// network stays on (the agent talks to its API), and the agent comes
// after "--".
func TestWrapBwrapWorkspaceWrite(t *testing.T) {
	ws := t.TempDir()
	prog, args, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: ws, Agent: "claude"}, host("linux", "bwrap"), "claude", []string{"-p"})
	if err != nil {
		t.Fatal(err)
	}
	if prog != "/usr/bin/bwrap" {
		t.Fatalf("prog = %q", prog)
	}
	if !argTriple(args, "--ro-bind", "/", "/") || !argTriple(args, "--bind", ws, ws) || !argPair(args, "--chdir", ws) {
		t.Fatalf("binds: %q", args)
	}
	if !argTriple(args, "--bind-try", "/home/u/.claude", "/home/u/.claude") || !argTriple(args, "--bind-try", "/home/u/.claude.json", "/home/u/.claude.json") {
		t.Fatalf("claude config not writable: %q", args)
	}
	if has(args, "--unshare-net") || !has(args, "--share-net") {
		t.Fatalf("the network must stay on: %q", args)
	}
	if !has(args, "--new-session") {
		t.Fatalf("a background step gets its own session: %q", args)
	}
	n := len(args)
	if args[n-3] != "--" || args[n-2] != "claude" || args[n-1] != "-p" {
		t.Fatalf("tail = %q", args[n-3:])
	}
	// The ro-bind of / comes before the writable binds (later binds win).
	if strings.Index(strings.Join(args, " "), "--ro-bind / /") > strings.Index(strings.Join(args, " "), "--bind "+ws) {
		t.Fatalf("order: %q", args)
	}
}

// Read-only: the worktree is bound read-only, never read-write.
func TestWrapBwrapReadOnly(t *testing.T) {
	ws := t.TempDir()
	_, args, err := Wrap(Plan{Mode: ReadOnly, Dir: ws, Agent: "codex"}, host("linux", "bwrap"), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if argTriple(args, "--bind", ws, ws) || !argTriple(args, "--ro-bind", ws, ws) {
		t.Fatalf("read-only bind: %q", args)
	}
	if !argTriple(args, "--bind-try", "/home/u/.codex", "/home/u/.codex") {
		t.Fatalf("codex config not writable: %q", args)
	}
}

// An interactive lane (a terminal tab) keeps the terminal's session so
// Ctrl-C and job control still work.
func TestWrapInteractiveKeepsSession(t *testing.T) {
	_, args, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: t.TempDir(), Agent: "claude", Interactive: true}, host("linux", "bwrap"), "claude", nil)
	if err != nil {
		t.Fatal(err)
	}
	if has(args, "--new-session") {
		t.Fatalf("interactive must not start a new session: %q", args)
	}
}

// No sandbox tool: a clear error, never a silent run outside the
// sandbox.
func TestWrapNoToolRefuses(t *testing.T) {
	for _, h := range []Host{host("linux"), host("darwin"), host("windows", "bwrap")} {
		_, _, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: t.TempDir(), Agent: "claude"}, h, "claude", nil)
		if err == nil || !errors.Is(err, ErrNoTool) {
			t.Fatalf("%s: err = %v", h.GOOS, err)
		}
		if !strings.Contains(err.Error(), "agent_os_sandbox") {
			t.Fatalf("the error must say how to turn it off: %v", err)
		}
	}
}

// A folder that is not there, or a mode we do not know, is refused.
func TestWrapBadPlan(t *testing.T) {
	if _, _, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: "", Agent: "claude"}, host("linux", "bwrap"), "claude", nil); err == nil {
		t.Fatal("empty dir accepted")
	}
	if _, _, err := Wrap(Plan{Mode: "full", Dir: t.TempDir(), Agent: "claude"}, host("linux", "bwrap"), "claude", nil); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, _, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: "rel/dir", Agent: "claude"}, host("linux", "bwrap"), "claude", nil); err == nil {
		t.Fatal("relative dir accepted")
	}
}

// macOS: sandbox-exec with a profile that denies every write except the
// temp folder, the worktree (workspace-write) and the agent's config.
func TestWrapSandboxExec(t *testing.T) {
	ws := t.TempDir()
	prog, args, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: ws, Agent: "claude"}, host("darwin", "sandbox-exec"), "claude", []string{"-p"})
	if err != nil {
		t.Fatal(err)
	}
	if prog != "/usr/bin/sandbox-exec" || args[0] != "-p" {
		t.Fatalf("prog=%q args=%q", prog, args)
	}
	p := args[1]
	for _, want := range []string{"(deny file-write*)", `(subpath "` + ws + `")`, `(subpath "/home/u/.claude")`, `(subpath "/tmp")`} {
		if !strings.Contains(p, want) {
			t.Fatalf("profile lacks %s:\n%s", want, p)
		}
	}
	if strings.Contains(p, "deny network") {
		t.Fatalf("the network must stay on:\n%s", p)
	}
	if args[2] != "claude" || args[3] != "-p" {
		t.Fatalf("tail = %q", args[2:])
	}
	_, ro, _ := Wrap(Plan{Mode: ReadOnly, Dir: ws, Agent: "claude"}, host("darwin", "sandbox-exec"), "claude", nil)
	if strings.Contains(ro[1], `(subpath "`+ws+`")`) {
		t.Fatalf("read-only profile lets the worktree be written:\n%s", ro[1])
	}
}

// A git worktree: its own git folder and the shared objects/refs are
// writable (the agent can commit in its lane); hooks and config stay
// read-only (no way to plant code that runs later outside the sandbox).
func TestWrapGitWorktreeDirs(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "repo", ".git")
	gitdir := filepath.Join(common, "worktrees", "lane1")
	ws := filepath.Join(root, "lane1")
	for _, d := range []string{gitdir, ws, filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "hooks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, args, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: ws, Agent: "claude"}, host("linux", "bwrap"), "claude", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{gitdir, filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs")} {
		if !argTriple(args, "--bind-try", d, d) {
			t.Fatalf("%s not writable: %q", d, args)
		}
	}
	for _, d := range []string{filepath.Join(common, "hooks"), filepath.Join(common, "config"), common} {
		if argTriple(args, "--bind-try", d, d) || argTriple(args, "--bind", d, d) {
			t.Fatalf("%s must stay read-only: %q", d, args)
		}
	}
	// Read-only lanes get no git writes either.
	_, ro, _ := Wrap(Plan{Mode: ReadOnly, Dir: ws, Agent: "claude"}, host("linux", "bwrap"), "claude", nil)
	if argTriple(ro, "--bind-try", gitdir, gitdir) {
		t.Fatalf("read-only lane can write git: %q", ro)
	}
}

// Line is the shell line typed into a lane's terminal: every word
// quoted, so a path with a space or a quote stays one word.
func TestLineQuotes(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "it's here")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	line, err := Line(Plan{Mode: WorkspaceWrite, Dir: ws, Agent: "claude", Interactive: true}, host("linux", "bwrap"), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "'/usr/bin/bwrap' ") || !strings.HasSuffix(line, " '--' 'claude'") {
		t.Fatalf("line = %s", line)
	}
	if !strings.Contains(line, `'`+strings.ReplaceAll(ws, "'", `'\''`)+`'`) {
		t.Fatalf("dir not quoted: %s", line)
	}
}

// Describe is the short chip text.
func TestDescribe(t *testing.T) {
	if got := Describe(Plan{Mode: WorkspaceWrite}, "bwrap"); got != "sandbox: bwrap · worktree writable · network on" {
		t.Fatalf("got %q", got)
	}
	if got := Describe(Plan{Mode: ReadOnly}, "sandbox-exec"); got != "sandbox: sandbox-exec · read-only · network on" {
		t.Fatalf("got %q", got)
	}
}

// Unknown agents still get a cache folder, never the whole home.
func TestUnknownAgentConfig(t *testing.T) {
	_, args, err := Wrap(Plan{Mode: WorkspaceWrite, Dir: t.TempDir(), Agent: "mystery"}, host("linux", "bwrap"), "mystery", nil)
	if err != nil {
		t.Fatal(err)
	}
	if argTriple(args, "--bind-try", "/home/u", "/home/u") || argTriple(args, "--bind", "/home/u", "/home/u") {
		t.Fatalf("home writable: %q", args)
	}
	if !argTriple(args, "--bind-try", "/home/u/.cache", "/home/u/.cache") {
		t.Fatalf("no cache: %q", args)
	}
}
