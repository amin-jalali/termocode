//go:build unix

package clistep

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The tests never run the real claude or codex: every CLI here is a
// small shell script on a temp PATH that prints a recorded-style event
// stream (testdata/*.jsonl) and writes what it got (argv, stdin, env,
// working dir) to files.

const fakeScript = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_OUT/argv"
pwd > "$FAKE_OUT/pwd"
env > "$FAKE_OUT/env"
if [ "$1" = "auth" ] || [ "$1" = "login" ]; then
  echo "$FAKE_STATUS_TEXT"
  exit ${FAKE_STATUS_EXIT:-0}
fi
cat > "$FAKE_OUT/stdin"
if [ -n "$FAKE_SLEEP" ]; then
  sleep 30 &
  echo $! > "$FAKE_OUT/child"
  wait
fi
[ -n "$FAKE_FIXTURE" ] && cat "$FAKE_FIXTURE"
[ -n "$FAKE_STDERR" ] && echo "$FAKE_STDERR" >&2
exit ${FAKE_EXIT:-0}
`

type fake struct {
	dir, out string
	env      []string
}

// newFake puts fake claude and codex scripts on a temp PATH.
func newFake(t *testing.T, extra ...string) *fake {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	bin := t.TempDir()
	out := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "FAKE_OUT=" + out}
	return &fake{dir: bin, out: out, env: append(env, extra...)}
}

func (f *fake) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.out, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return "FAKE_FIXTURE=" + p
}

// fixtureIn is fixture name with its /work paths moved to root.
func fixtureIn(t *testing.T, name, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "/work", root)), 0o600); err != nil {
		t.Fatal(err)
	}
	return "FAKE_FIXTURE=" + p
}

type rec struct{ evs []ai.Event }

func (r *rec) emit(e ai.Event) { r.evs = append(r.evs, e) }

func (r *rec) tools() []string {
	var out []string
	for _, e := range r.evs {
		switch e.Kind {
		case ai.EventToolStart:
			out = append(out, "start "+e.Tool+": "+e.Detail)
		case ai.EventToolResult:
			out = append(out, "end "+e.Tool+": "+e.Detail)
		}
	}
	return out
}

func (r *rec) text() string {
	var b strings.Builder
	for _, e := range r.evs {
		if e.Kind == ai.EventText {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func TestArgs(t *testing.T) {
	cases := []struct {
		spec Spec
		want []string
	}{
		{Spec{CLI: ai.CLIClaude}, []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob"}},
		{Spec{CLI: ai.CLIClaude, Write: true}, []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob,Edit,Write", "--permission-mode", "acceptEdits"}},
		{Spec{CLI: ai.CLICodex, Dir: "/w"}, []string{"exec", "--json", "--color", "never", "--sandbox", "read-only", "-C", "/w", "-"}},
		{Spec{CLI: ai.CLICodex, Dir: "/w", Write: true}, []string{"exec", "--json", "--color", "never", "--sandbox", "workspace-write", "-C", "/w", "-"}},
	}
	for _, c := range cases {
		if got := Args(c.spec); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v:\n got %q\nwant %q", c.spec, got, c.want)
		}
	}
}

// CS-1: the prompt goes in on stdin (never in argv), the run happens in
// the project root, termocode's and the vendors' API keys are not passed
// on (the CLI uses its own subscription login), and the stream becomes
// text, tool and usage events.
func TestRunClaudeStream(t *testing.T) {
	root := t.TempDir()
	f := newFake(t, fixtureIn(t, "claude_ok.jsonl", root),
		"TERMOCODE_AI_API_KEY="+"sk-ant-"+"secret1", "ANTHROPIC_API_KEY="+"sk-ant-"+"secret2", "KEEP_ME=1")
	var r rec
	prompt := "Fix the nil map.\n### Task\nsecret-free prompt"
	res, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: root, Env: f.env, Write: true}, prompt, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Fixed the nil map in internal/a.go." {
		t.Errorf("text %q", res.Text)
	}
	if got := f.read(t, "stdin"); got != prompt {
		t.Errorf("stdin %q", got)
	}
	if argv := f.read(t, "argv"); strings.Contains(argv, "nil map") {
		t.Errorf("the prompt is in argv: %q", argv)
	}
	if pwd := strings.TrimSpace(f.read(t, "pwd")); pwd != root {
		if real, _ := filepath.EvalSymlinks(root); pwd != real {
			t.Errorf("pwd %q, want %q", pwd, root)
		}
	}
	env := f.read(t, "env")
	if strings.Contains(env, "secret1") || strings.Contains(env, "secret2") || !strings.Contains(env, "KEEP_ME=1") {
		t.Errorf("env:\n%s", env)
	}
	want := []string{
		"start Read: Read internal/a.go", "end Read: 3 lines",
		"start Edit: Edit internal/a.go", "end Edit: done",
	}
	if got := r.tools(); !reflect.DeepEqual(got, want) {
		t.Errorf("tools\n got %q\nwant %q", got, want)
	}
	if got := r.text(); got != "Let me read the file.\n\nFixed the nil map in internal/a.go." {
		t.Errorf("streamed text %q", got)
	}
	u := res.Usage
	if u == nil || u.Input != 40 || u.CacheWrite != 500 || u.CacheRead != 9300 || u.Output != 123 || u.Requests != 3 || u.Model != "claude-sonnet-4-5" {
		t.Errorf("usage %+v", u)
	}
	if !reflect.DeepEqual(res.Files, []string{"internal/a.go"}) {
		t.Errorf("files %q", res.Files)
	}
	if r.evs[0].Kind != ai.EventTurnStart {
		t.Errorf("first event %v", r.evs[0].Kind)
	}
}

func TestRunCodexStream(t *testing.T) {
	root := t.TempDir()
	f := newFake(t, fixtureIn(t, "codex_ok.jsonl", root), "OPENAI_API_KEY="+"sk-"+"proj-secret3", "CODEX_API_KEY="+"sk-"+"secret4")
	var r rec
	res, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: root, Env: f.env}, "review it", r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "The map is made before use now. Tests pass.\nVERDICT: PASS" {
		t.Errorf("text %q", res.Text)
	}
	if got := f.read(t, "stdin"); got != "review it" {
		t.Errorf("stdin %q", got)
	}
	if argv := f.read(t, "argv"); !strings.Contains(argv, "read-only") || strings.Contains(argv, "review it") {
		t.Errorf("argv %q", argv)
	}
	if env := f.read(t, "env"); strings.Contains(env, "secret3") || strings.Contains(env, "secret4") {
		t.Errorf("env:\n%s", env)
	}
	want := []string{
		"start shell: git diff --stat", "end shell: exit 0 · 2 lines",
		"start edit: internal/b.go", "end edit: update",
	}
	if got := r.tools(); !reflect.DeepEqual(got, want) {
		t.Errorf("tools\n got %q\nwant %q", got, want)
	}
	u := res.Usage
	if u == nil || u.Input != 600 || u.CacheRead != 1800 || u.Output != 150 || u.Requests != 1 {
		t.Errorf("usage %+v", u)
	}
	if !reflect.DeepEqual(res.Files, []string{"internal/b.go"}) {
		t.Errorf("files %q", res.Files)
	}
}

// CS-4: the CLI's usage limit is an *ai.LimitError with the reset time
// when the CLI gives one.
func TestRunClaudeLimit(t *testing.T) {
	f := newFake(t, fixture(t, "claude_limit.jsonl"), "FAKE_EXIT=1")
	_, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), Env: f.env}, "x", nil)
	var le *ai.LimitError
	if !errors.As(err, &le) {
		t.Fatalf("err %v", err)
	}
	if !le.ResetAt.Equal(time.Unix(1761580800, 0)) || le.CLI != ai.CLIClaude || !strings.Contains(le.Msg, "hit your limit") {
		t.Errorf("limit %+v", le)
	}
}

func TestRunCodexLimit(t *testing.T) {
	f := newFake(t, fixture(t, "codex_limit.jsonl"), "FAKE_EXIT=1")
	now := time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC)
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env, Now: func() time.Time { return now }}, "x", nil)
	var le *ai.LimitError
	if !errors.As(err, &le) {
		t.Fatalf("err %v", err)
	}
	if want := now.Add(90 * time.Minute); !le.ResetAt.Equal(want) {
		t.Errorf("reset %v, want %v", le.ResetAt, want)
	}
}

func TestResetTime(t *testing.T) {
	now := time.Date(2026, 10, 27, 13, 20, 0, 0, time.UTC)
	cases := []struct {
		text string
		want time.Time
	}{
		{"You've hit your limit · resets 4pm (UTC)", time.Date(2026, 10, 27, 16, 0, 0, 0, time.UTC)},
		{"5-hour limit reached ∙ resets 10:30am", time.Date(2026, 10, 28, 10, 30, 0, 0, time.UTC)},
		{"Claude AI usage limit reached|1761580800", time.Unix(1761580800, 0)},
		{"try again in 2 hours 5 minutes.", now.Add(2*time.Hour + 5*time.Minute)},
		{"or try again in 3 days 1 hour", now.Add(73 * time.Hour)},
		{"Please try again at 3:05 PM.", time.Date(2026, 10, 27, 15, 5, 0, 0, time.UTC)},
		{"usage limit reached", time.Time{}},
	}
	for _, c := range cases {
		if got := resetTime(c.text, now); !got.Equal(c.want) {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
	for _, s := range []string{"You've hit your limit", "Claude AI usage limit reached|1", "You've hit your usage limit.", "5-hour limit reached"} {
		if !isLimit(s) {
			t.Errorf("not a limit: %q", s)
		}
	}
	if isLimit("Fixed the rate limiter in limit.go") {
		t.Error("a normal answer is not a limit")
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitDead(t *testing.T, f *fake) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.out, "child"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still runs: the process group was not killed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A timeout kills the CLI and everything it started.
func TestRunTimeoutKillsGroup(t *testing.T) {
	f := newFake(t, "FAKE_SLEEP=1")
	start := time.Now()
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env, Timeout: 300 * time.Millisecond}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	waitDead(t, f)
}

// Stop (the context ends) kills the process group too.
func TestRunCancelKillsGroup(t *testing.T) {
	f := newFake(t, "FAKE_SLEEP=1")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := os.Stat(filepath.Join(f.out, "child")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	_, err := Run(ctx, Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), Env: f.env}, "x", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	waitDead(t, f)
}

func TestRunNotLoggedIn(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "login.jsonl")
	_ = os.WriteFile(bad, []byte(`{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login","usage":{}}`+"\n"), 0o600)
	f := newFake(t, "FAKE_FIXTURE="+bad, "FAKE_EXIT=1")
	_, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), Env: f.env}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "Claude Code is not logged in") || !strings.Contains(err.Error(), "claude auth login") {
		t.Fatalf("err %v", err)
	}
}

func TestRunFailShowsStderr(t *testing.T) {
	f := newFake(t, "FAKE_EXIT=2", "FAKE_STDERR=error: unexpected argument '--frob' found")
	_, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: t.TempDir(), Env: f.env}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "--frob") || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("err %v", err)
	}
}

func TestRunMissingCLI(t *testing.T) {
	env := []string{"PATH=" + t.TempDir()}
	_, err := Run(context.Background(), Spec{CLI: ai.CLIClaude, Dir: t.TempDir(), Env: env}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "Claude Code is not installed") {
		t.Fatalf("err %v", err)
	}
}

func TestCleanEnv(t *testing.T) {
	in := []string{"PATH=/bin", "HOME=/h", "TERMOCODE_AI_API_KEY=a", "TERMOCODE_GITHUB_TOKEN=b", "TERMOCODE_THEME=dark",
		"ANTHROPIC_API_KEY=c", "ANTHROPIC_AUTH_TOKEN=d", "OPENAI_API_KEY=e", "CODEX_API_KEY=f", "OPENROUTER_API_KEY=g", "GOPATH=/g"}
	got := CleanEnv(in)
	want := []string{"PATH=/bin", "HOME=/h", "TERMOCODE_THEME=dark", "GOPATH=/g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

// CS-5: Check says whether a CLI is there and logged in, by the CLI's
// own status command (never by reading its files).
func TestCheck(t *testing.T) {
	f := newFake(t, "FAKE_STATUS_EXIT=0", "FAKE_STATUS_TEXT=Logged in using ChatGPT")
	if got := Check(context.Background(), ai.CLICodex, f.env); got != StatusReady {
		t.Errorf("logged in: %v", got)
	}
	if argv := f.read(t, "argv"); argv != "login\nstatus\n" {
		t.Errorf("codex status argv %q", argv)
	}
	f = newFake(t, "FAKE_STATUS_EXIT=1", "FAKE_STATUS_TEXT=Not logged in")
	if got := Check(context.Background(), ai.CLICodex, f.env); got != StatusLoggedOut {
		t.Errorf("logged out: %v", got)
	}
	f = newFake(t, "FAKE_STATUS_EXIT=0", `FAKE_STATUS_TEXT={"loggedIn": false}`)
	if got := Check(context.Background(), ai.CLIClaude, f.env); got != StatusLoggedOut {
		t.Errorf("claude logged out (exit 0): %v", got)
	}
	if argv := f.read(t, "argv"); argv != "auth\nstatus\n" {
		t.Errorf("claude status argv %q", argv)
	}
	if got := Check(context.Background(), ai.CLIClaude, []string{"PATH=" + t.TempDir()}); got != StatusMissing {
		t.Errorf("missing: %v", got)
	}
	if LoginLine(ai.CLIClaude) != "claude auth login" || LoginLine(ai.CLICodex) != "codex login" {
		t.Error("login lines")
	}
	if InstallLine(ai.CLIClaude) != "npm install -g @anthropic-ai/claude-code" || InstallLine(ai.CLICodex) != "npm install -g @openai/codex" {
		t.Error("install lines")
	}
}

// CS-2: the hand-off carries the work so far as a capped git diff.
func TestGitDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nvar m map[string]int\n"), 0o644)
	git("add", "a.go")
	git("commit", "-q", "-m", "one")
	if got := GitDiff(context.Background(), dir, 4096); got != "" {
		t.Errorf("no change: %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nvar m = map[string]int{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.go"), []byte("package a\n"), 0o644)
	got := GitDiff(context.Background(), dir, 4096)
	for _, want := range []string{"+var m = map[string]int{}", "-var m map[string]int", "?? new.go", "```diff"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff misses %q:\n%s", want, got)
		}
	}
	if small := GitDiff(context.Background(), dir, 60); len(small) > 60+len(DiffCutNote)+16 || !strings.Contains(small, DiffCutNote) {
		t.Errorf("cap: %d bytes\n%s", len(small), small)
	}
	if GitDiff(context.Background(), t.TempDir(), 4096) != "" {
		t.Error("not a repo: nothing")
	}
}
