//go:build unix

package clistep

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// A chat's next turn goes on in the CLI's own session: Codex as
// `exec … resume <id> -` (the prompt still on stdin), Claude Code with
// --resume <id>. An id that could pass for a flag is left out.
func TestArgsResume(t *testing.T) {
	cases := []struct {
		spec Spec
		want []string
	}{
		{Spec{CLI: ai.CLICodex, Dir: "/w", Resume: "0199a7c2-5b1e"},
			[]string{"exec", "--json", "--color", "never", "--sandbox", "read-only", "-c", `approval_policy="never"`, "-C", "/w", "resume", "0199a7c2-5b1e", "-"}},
		{Spec{CLI: ai.CLIClaude, Resume: "6f1c"},
			[]string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob", "--resume", "6f1c"}},
		{Spec{CLI: ai.CLICodex, Dir: "/w", Resume: "--last"},
			[]string{"exec", "--json", "--color", "never", "--sandbox", "read-only", "-c", `approval_policy="never"`, "-C", "/w", "-"}},
		{Spec{CLI: ai.CLIClaude, Resume: "a b"},
			[]string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob"}},
	}
	for _, c := range cases {
		if got := Args(c.spec); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Args(%+v)\n got %q\nwant %q", c.spec, got, c.want)
		}
	}
}

// The run gives the CLI's session id back: Codex's thread.started
// thread_id, Claude Code's session_id.
func TestRunGivesSession(t *testing.T) {
	for _, c := range []struct{ cli, fixture, want string }{
		{ai.CLICodex, "codex_ok.jsonl", "0199a7c2-5b1e-7c30-9d2f-0c1e2a3b4c5d"},
		{ai.CLIClaude, "claude_ok.jsonl", "6f1c"},
	} {
		root := t.TempDir()
		f := newFake(t, fixtureIn(t, c.fixture, root))
		res, err := Run(context.Background(), Spec{CLI: c.cli, Dir: root, Env: f.env, Resume: "old-1"}, "and now?", nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Session != c.want {
			t.Errorf("%s session %q, want %q", c.cli, res.Session, c.want)
		}
		if argv := f.read(t, "argv"); !strings.Contains(argv, "old-1\n") {
			t.Errorf("%s argv misses the resume id:\n%s", c.cli, argv)
		}
		if got := f.read(t, "stdin"); got != "and now?" {
			t.Errorf("%s stdin %q", c.cli, got)
		}
	}
}
