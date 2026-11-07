package clistep

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentjail"
	"github.com/amin-jalali/termocode/internal/ai"
)

func jailHost(goos string, have ...string) agentjail.Host {
	return agentjail.Host{GOOS: goos, Home: "/home/u", Tmp: "/tmp", Look: func(n string) (string, error) {
		for _, h := range have {
			if h == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}}
}

// CMP-60: OSSandbox off runs the CLI as is.
func TestCommandNoSandbox(t *testing.T) {
	s := Spec{CLI: ai.CLIClaude, Dir: t.TempDir()}
	prog, args, note, err := Command(s, "/bin/claude", jailHost("linux", "bwrap"))
	if err != nil || prog != "/bin/claude" || note != "" || strings.Join(args, " ") != strings.Join(Args(s), " ") {
		t.Fatalf("prog=%q args=%q note=%q err=%v", prog, args, note, err)
	}
}

// OSSandbox on puts the CLI inside the OS sandbox: Write maps to a
// writable worktree, off to read-only; the CLI and its args come last.
func TestCommandOSSandbox(t *testing.T) {
	dir := t.TempDir()
	for _, write := range []bool{true, false} {
		s := Spec{CLI: ai.CLICodex, Dir: dir, Write: write, OSSandbox: true}
		prog, args, note, err := Command(s, "/bin/codex", jailHost("linux", "bwrap"))
		if err != nil {
			t.Fatal(err)
		}
		if prog != "/usr/bin/bwrap" {
			t.Fatalf("prog = %q", prog)
		}
		joined := strings.Join(args, " ")
		if !strings.HasSuffix(joined, "-- /bin/codex "+strings.Join(Args(s), " ")) {
			t.Fatalf("tail: %s", joined)
		}
		rw := strings.Contains(joined, "--bind "+dir+" "+dir)
		if rw != write {
			t.Fatalf("write=%v but rw bind=%v: %s", write, rw, joined)
		}
		if !strings.HasPrefix(note, "sandbox: bwrap") {
			t.Fatalf("note = %q", note)
		}
	}
}

// No sandbox tool: the step fails, it never runs outside the sandbox.
func TestCommandOSSandboxMissingTool(t *testing.T) {
	s := Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), OSSandbox: true}
	if _, _, _, err := Command(s, "/bin/claude", jailHost("linux")); !errors.Is(err, agentjail.ErrNoTool) {
		t.Fatalf("err = %v", err)
	}
}
