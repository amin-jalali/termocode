//go:build unix

package clistep

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// stream writes lines as a fake CLI's output and returns its env entry.
func stream(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "out.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return "FAKE_FIXTURE=" + p
}

func TestRunRefusesUnknownCLIAndMissingFolder(t *testing.T) {
	f := newFake(t)
	if _, err := Run(context.Background(), Spec{CLI: "aider", Dir: t.TempDir(), Env: f.env}, "x", nil); err == nil || !strings.Contains(err.Error(), `unknown CLI "aider"`) {
		t.Errorf("unknown cli: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	_, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: gone, Env: f.env}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "is not there") {
		t.Errorf("missing folder: %v", err)
	}
}

// A CLI binary that cannot be executed (a broken install) is a clear
// error, not a hang.
func TestRunBrokenBinary(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("\x00\x01not a program"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + bin}
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: env}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "Codex did not start") {
		t.Errorf("err = %v", err)
	}
	if got := Check(context.Background(), ai.CLICodex, env); got != StatusUnknown {
		t.Errorf("check = %v", got)
	}
}

func TestRunClaudeToolLines(t *testing.T) {
	root := t.TempDir()
	f := newFake(t, stream(t,
		`not json`,
		`{broken`,
		`{"type":"assistant","message":{"content":[7,{"type":"tool_use","id":"a","name":"Bash","input":{"command":"go   test ./..."}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b","name":"Grep","input":{"pattern":"TODO"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"c","name":"WebFetch","input":{"url":"https://go.dev"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"d","name":"Task","input":{"description":"look around"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"e","name":"TodoWrite","input":{}}]}}`,
		`{"type":"user","message":{"content":[7,{"type":"text","text":"x"},{"type":"tool_result","tool_use_id":"a","is_error":true,"content":"exit 1: FAIL"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":[{"type":"text","text":"a.go:1\n"},{"type":"text","text":"b.go:2"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"c","content":""}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"d","content":{"odd":1}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"e","content":"one line"}]}}`,
		`{"type":"result","subtype":"success","result":"done","usage":{}}`,
	))
	var r rec
	res, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: root, Env: f.env}, "x", r.emit)
	if err != nil || res.Text != "done" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if res.Usage != nil {
		t.Errorf("no counts must give no usage: %+v", res.Usage)
	}
	want := []string{
		"start Bash: Bash: go test ./...",
		"start Grep: Grep TODO",
		"start WebFetch: WebFetch https://go.dev",
		"start Task: Task: look around",
		"start TodoWrite: TodoWrite",
		"end Bash: error: exit 1: FAIL",
		"end Grep: 2 lines",
		"end WebFetch: 0 lines",
		"end Task: 0 lines",
		"end TodoWrite: 1 line",
	}
	if got := r.tools(); !reflect.DeepEqual(got, want) {
		t.Errorf("tools\n got %q\nwant %q", got, want)
	}
}

func TestRunClaudeErrorResults(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		check func(error) bool
	}{
		{"subtype when no text", []string{`{"type":"result","subtype":"error_max_turns","is_error":true}`},
			func(err error) bool { return err.Error() == "Claude Code: error_max_turns" }},
		{"limit in the result text", []string{`{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1767225600"}`},
			func(err error) bool {
				var le *ai.LimitError
				return errors.As(err, &le) && le.ResetAt.Equal(time.Unix(1767225600, 0))
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t, stream(t, c.lines...), "FAKE_EXIT=1")
			_, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), Env: f.env}, "x", nil)
			if err == nil || !c.check(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestRunCodexItems(t *testing.T) {
	root := t.TempDir()
	f := newFake(t, stream(t,
		`{"type":"item.started"}`,
		`{"type":"item.started","item":{"id":"f","type":"file_change","changes":[{"path":"x.go","kind":"add"}]}}`,
		`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"gh","tool":"issues"}}`,
		`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"gh","tool":"issues","status":"completed"}}`,
		`{"type":"item.started","item":{"id":"w","type":"web_search","query":"go generics"}}`,
		`{"type":"item.completed","item":{"id":"w","type":"web_search","query":"go generics"}}`,
		`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"ls","aggregated_output":"a\nb\n"}}`,
		`{"type":"item.completed","item":{"id":"e","type":"error","message":""}}`,
		`{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"All good."}}`,
		`{"type":"turn.completed"}`,
	))
	var r rec
	res, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: root, Env: f.env}, "x", r.emit)
	if err != nil || res.Text != "All good." {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := []string{
		"start gh.issues: gh.issues", "end gh.issues: completed",
		"start web_search: go generics", "end web_search: done",
		"start shell: ls", "end shell: 2 lines",
	}
	if got := r.tools(); !reflect.DeepEqual(got, want) {
		t.Errorf("tools\n got %q\nwant %q", got, want)
	}
	if len(res.Files) != 0 {
		t.Errorf("a started change is no change yet: %q", res.Files)
	}
}

func TestRunCodexErrorItem(t *testing.T) {
	f := newFake(t, stream(t,
		`{"type":"item.completed","item":{"id":"e","type":"error","message":"model not supported"}}`,
		`{"type":"turn.completed"}`,
	))
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env}, "x", nil)
	if err == nil || err.Error() != "Codex: model not supported" {
		t.Errorf("err = %v", err)
	}
}

func TestRunExitErrors(t *testing.T) {
	cases := []struct {
		name  string
		env   []string
		check func(error) bool
	}{
		{"logged out on stderr", []string{"FAKE_EXIT=1", "FAKE_STDERR=Error: Not logged in"},
			func(err error) bool { return strings.Contains(err.Error(), "Codex is not logged in") }},
		{"limit on stderr", []string{"FAKE_EXIT=1", "FAKE_STDERR=You've hit your usage limit. Try again in 2 hours"},
			func(err error) bool {
				var le *ai.LimitError
				return errors.As(err, &le) && le.ResetAt.Equal(time.Unix(1000, 0).Add(2*time.Hour))
			}},
		{"silent exit", []string{"FAKE_EXIT=3"},
			func(err error) bool {
				return strings.Contains(err.Error(), "Codex stopped with an error: exit status 3")
			}},
		{"no answer", nil,
			func(err error) bool { return err.Error() == "Codex ended without an answer" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t, c.env...)
			now := func() time.Time { return time.Unix(1000, 0) }
			_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env, Now: now}, "x", nil)
			if err == nil || !c.check(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

// A CLI that prints a flood on stderr gives a short, one-line error.
func TestRunLongStderrIsCut(t *testing.T) {
	f := newFake(t, "FAKE_EXIT=1", "FAKE_STDERR="+strings.Repeat("word ", 2000))
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env}, "x", nil)
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") || len([]rune(msg)) > 400 || !strings.HasSuffix(msg, "…") {
		t.Errorf("error not cut (%d runes): %q", len([]rune(msg)), msg)
	}
}

func TestReadLinesSkipsHugeLineAndKeepsLastLine(t *testing.T) {
	in := strings.Repeat("x", maxLine+10) + "\n" + `{"a":1}` + "\n" + `{"b":2}`
	var got []string
	readLines(strings.NewReader(in), func(b []byte) { got = append(got, string(b)) })
	if !reflect.DeepEqual(got, []string{`{"a":1}`, `{"b":2}`}) {
		t.Errorf("lines = %q", got)
	}
}

func TestTailBufKeepsEnd(t *testing.T) {
	var tb tailBuf
	_, _ = tb.Write([]byte(strings.Repeat("a", 4000)))
	_, _ = tb.Write([]byte(strings.Repeat("b", 200)))
	s := tb.String()
	if len(s) != 4096 || !strings.HasSuffix(s, strings.Repeat("b", 200)) {
		t.Errorf("len %d", len(s))
	}
	if got := lastLines("1\n2\n3\n4\n", 2); got != "3 · 4" {
		t.Errorf("lastLines = %q", got)
	}
}

func TestResetTimeEdges(t *testing.T) {
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if got := resetTime("try again in 30 seconds", now); !got.Equal(now.Add(30 * time.Second)) {
		t.Errorf("seconds: %v", got)
	}
	if got := resetTime("resets 12am", now); !got.Equal(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("midnight: %v", got)
	}
	if got := resetTime("resets 25:00", now); !got.IsZero() {
		t.Errorf("bad clock time: %v", got)
	}
}

func TestStatusTextAndUnknownCLI(t *testing.T) {
	for s, want := range map[Status]string{StatusReady: "ready", StatusMissing: "not installed", StatusLoggedOut: "not logged in", StatusUnknown: "unknown"} {
		if s.String() != want {
			t.Errorf("%d = %q", s, s.String())
		}
	}
	if Args(Spec{CLI: "aider"}) != nil || statusArgs("aider") != nil || LoginLine("aider") != "" {
		t.Error("an unknown CLI has no args or login line")
	}
	if got := Check(context.Background(), "aider", newFake(t).env); got != StatusMissing {
		t.Errorf("unknown cli check = %v", got)
	}
}

func TestLookPathSkipsEmptyAndNonPrograms(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(bin, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=::" + bin}
	for _, cli := range []string{"claude", "codex"} {
		if _, err := lookPath(cli, env); !errors.Is(err, exec.ErrNotFound) {
			t.Errorf("%s: %v", cli, err)
		}
	}
	if _, err := lookPath("claude", nil); err == nil {
		t.Error("no PATH finds nothing")
	}
}

func TestGitDiffNoCommitYetAndRuneCut(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	c := exec.Command("git", "-C", dir, "init", "-q")
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "ü.txt"), []byte("ü"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := GitDiff(context.Background(), dir, 0)
	if !strings.Contains(got, "ü.txt") && !strings.Contains(got, `\303\274.txt`) {
		t.Fatalf("no-commit repo: %q", got)
	}
	// Cut inside a two-byte rune: the cut moves back to a whole rune.
	long := strings.Repeat("ü", 20)
	if err := os.WriteFile(filepath.Join(dir, long+".txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := exec.Command("git", "-C", dir, "config", "core.quotePath", "false")
	if out, err := cfg.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	small := GitDiff(context.Background(), dir, 10)
	body := strings.TrimSuffix(small, DiffCutNote)
	if !strings.HasSuffix(small, DiffCutNote) || !utf8Valid(body) {
		t.Errorf("cut = %q", small)
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }
