package agentlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	claudeFixture = "testdata/claude/-work-demo/3f9a2c1e-0000-4000-8000-000000000001.jsonl"
	codexFixture  = "testdata/codex/sessions/2026/11/05/rollout-2026-11-05T11-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001.jsonl"
)

func parseFixture(t *testing.T, cli CLI, path string) Session {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(cli, f)
	if err != nil {
		t.Fatalf("Parse %s: %v", path, err)
	}
	return s
}

// turnsText is one line per turn: "kind | tool | args | text | failed".
func turnsText(ts []Turn) string {
	var b strings.Builder
	for _, t := range ts {
		text := strings.ReplaceAll(t.Text, "\n", "⏎")
		if r := []rune(text); len(r) > 40 {
			text = string(r[:40])
		}
		fail := ""
		if t.Failed {
			fail = " | failed"
		}
		b.WriteString(t.Kind.String() + " | " + t.Tool + " | " + t.Args + " | " + text + fail + "\n")
	}
	return b.String()
}

// Spec (AGX-7): a Claude Code session file is read into turns: the
// user's prompts, the assistant's text, each tool call with a one-line
// summary (the file, the command, +N -M for an edit) and each tool
// result (collapsed later). Side tasks, meta lines (local command
// output), summaries, lines of a kind this version does not know and
// lines that are not JSON are skipped, never fatal. Usage: one count per
// assistant message (a message split over lines counts once), the
// context is the last message's input.
func TestParseClaude(t *testing.T) {
	s := parseFixture(t, Claude, claudeFixture)
	want := "user |  |  | Add the pinned row as in the issue. Run \n" +
		"assistant |  |  | I will add a PINNED group to the worktre\n" +
		"tool | Read | /work/demo/internal/app/worktrees_view.go | \n" +
		"result | Read |  | package app⏎⏎func worktreesView() string\n" +
		"tool | Edit | /work/demo/internal/app/worktrees_view.go +4 -1 | \n" +
		"result | Edit |  | The file has been updated.\n" +
		"tool | Bash | go test ./internal/app/ -run Worktree | \n" +
		"result | Bash |  | ok  \tdemo/internal/app\t0.31s\n" +
		"assistant |  |  | Done. The pinned rows sit first; 2 tests\n"
	if got := turnsText(s.Turns); got != want {
		t.Errorf("turns:\n%s\nwant:\n%s", got, want)
	}
	if s.ID != "3f9a2c1e-0000-4000-8000-000000000001" || s.Cwd != "/work/demo" || s.Branch != "feat/pin-row" {
		t.Errorf("id %q cwd %q branch %q", s.ID, s.Cwd, s.Branch)
	}
	if s.Model != "model-a" {
		t.Errorf("model %q", s.Model)
	}
	if !s.Start.Equal(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)) || !s.End.Equal(time.Date(2026, 11, 5, 10, 1, 1, 0, time.UTC)) {
		t.Errorf("start %v end %v", s.Start, s.End)
	}
	if s.Prompts() != 1 || s.FirstPrompt() != "Add the pinned row as in the issue. Run go test." {
		t.Errorf("prompts %d first %q", s.Prompts(), s.FirstPrompt())
	}
	// msg_1 twice counts once: in 6500+6400+7300+82000, out 80+200+40+120.
	if s.Usage.In != 6500+6400+7300+82000 || s.Usage.Out != 440 || s.Usage.Context != 82000 {
		t.Errorf("usage %+v", s.Usage)
	}
	if s.Skipped != 2 { // the unknown kind and the line that is not JSON
		t.Errorf("skipped %d, want 2", s.Skipped)
	}
}

// Spec (AGX-7): a Codex rollout file: session_meta gives the id and the
// folder, the injected <environment_context> message is not a prompt,
// event_msg copies of the messages are not doubled, a shell call shows
// its command without the "bash -lc" wrapper and fails on a non-zero
// exit code, apply_patch shows the file and +N -M. token_count gives the
// totals, the context and its window, and the plan limit (used percent,
// reset time).
func TestParseCodex(t *testing.T) {
	s := parseFixture(t, Codex, codexFixture)
	want := "user |  |  | Fix the failing test in queue.go\n" +
		"tool | shell | go test ./... | \n" +
		"result | shell |  | --- FAIL: TestQueue⏎FAIL⏎ | failed\n" +
		"tool | apply_patch | queue.go +2 -1 | \n" +
		"result | apply_patch |  | Success. Updated the following files:⏎M \n" +
		"assistant |  |  | Fixed: the queue now drains before it re\n"
	if got := turnsText(s.Turns); got != want {
		t.Errorf("turns:\n%s\nwant:\n%s", got, want)
	}
	if s.ID != "0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001" || s.Cwd != "/work/demo" || s.Branch != "main" || s.Model != "model-b" {
		t.Errorf("id %q cwd %q branch %q model %q", s.ID, s.Cwd, s.Branch, s.Model)
	}
	u := s.Usage
	if u.In != 18000 || u.Out != 2000 || u.Context != 9000 || u.Window != 272000 {
		t.Errorf("usage %+v", u)
	}
	reset := time.Date(2026, 11, 5, 11, 0, 21, 0, time.UTC).Add(5400 * time.Second)
	if u.LimitPct != 62 || !u.LimitReset.Equal(reset) {
		t.Errorf("limit %v %v, want 62 %v", u.LimitPct, u.LimitReset, reset)
	}
	if s.Skipped != 1 { // a_new_kind
		t.Errorf("skipped %d, want 1", s.Skipped)
	}
}

// Spec (AGX-7): the CLI is still writing the last line: it is left out
// (no error, no half turn); the lines before it are read.
func TestParseTruncatedLastLine(t *testing.T) {
	s := parseFixture(t, Codex, "testdata/codex_truncated.jsonl")
	if got := turnsText(s.Turns); got != "user |  |  | Second prompt\n" {
		t.Errorf("turns:\n%s", got)
	}
	if s.Skipped != 0 {
		t.Errorf("skipped %d: a half line is not a bad line", s.Skipped)
	}
}

// Spec (AGX-7, plan §4): a file in a format this version does not know
// (no line understood) fails safely with ErrUnknownFormat, so the view
// says "cannot read this session" instead of drawing a wrong chat. An
// empty file is an empty session, not an error.
func TestParseUnknownFormat(t *testing.T) {
	for _, cli := range []CLI{Claude, Codex} {
		f, err := os.Open("testdata/unknown_format.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse(cli, f)
		f.Close()
		if !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%s: err %v, want ErrUnknownFormat", cli, err)
		}
		if s, err := Parse(cli, strings.NewReader("")); err != nil || len(s.Turns) != 0 {
			t.Errorf("%s empty: %v %d turns", cli, err, len(s.Turns))
		}
	}
	// A Claude file read as Codex is not understood either.
	f, _ := os.Open(claudeFixture)
	defer f.Close()
	if _, err := Parse(Codex, f); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("claude file as codex: %v", err)
	}
}

// Spec (AGX-7, D1): tool results hold file contents and may hold
// secrets: every text that leaves the package (prompts, answers, tool
// arguments, results) is redacted first.
func TestParseRedactsSecrets(t *testing.T) {
	key := "sk-" + "ant-" + "api03-" + strings.Repeat("Q", 40)
	lines := []string{
		`{"type":"user","sessionId":"s1","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"my key is ` + key + `"}}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-11-05T10:00:01Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"curl -H 'x-api-key: ` + key + `' https://example.invalid"}},{"type":"text","text":"using ` + key + `"}]}}`,
		`{"type":"user","sessionId":"s1","timestamp":"2026-11-05T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ANTHROPIC_API_KEY=` + key + `"}]}}`,
	}
	s, err := Parse(Claude, strings.NewReader(strings.Join(lines, "\n")+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 4 {
		t.Fatalf("turns:\n%s", turnsText(s.Turns))
	}
	for _, tr := range s.Turns {
		if strings.Contains(tr.Text, "QQQQ") || strings.Contains(tr.Args, "QQQQ") {
			t.Errorf("secret left in %s turn: %q %q", tr.Kind, tr.Args, tr.Text)
		}
	}
	if s.FirstPrompt() == "" || strings.Contains(s.FirstPrompt(), "QQQQ") {
		t.Errorf("first prompt %q", s.FirstPrompt())
	}
}

// Spec (AGX-7): at most MaxRead (16 MB) bytes of a file are read; a
// longer one keeps its newest part (from the first whole line in the
// window) and is marked Truncated. The test lowers the cap to 64 KB.
func TestReadFileCap(t *testing.T) {
	if MaxRead != 16<<20 {
		t.Errorf("MaxRead %d, want 16 MB", MaxRead)
	}
	old := maxRead
	maxRead = 64 << 10
	t.Cleanup(func() { maxRead = old })
	dir := t.TempDir()
	p := filepath.Join(dir, "big.jsonl")
	var b bytes.Buffer
	pad := strings.Repeat("x", 1000)
	i := 0
	for b.Len() < 3*int(maxRead)/2 {
		b.WriteString(`{"type":"user","sessionId":"big","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"prompt ` + pad + `"}}` + "\n")
		i++
	}
	b.WriteString(`{"type":"user","sessionId":"big","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"the last prompt"}}` + "\n")
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadFile(Claude, p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Truncated || s.Path != p {
		t.Errorf("truncated %v path %q", s.Truncated, s.Path)
	}
	if n := len(s.Turns); n >= i || n < i/2 {
		t.Errorf("%d turns of %d: want only the newest part", n, i+1)
	}
	if s.Skipped != 0 {
		t.Errorf("skipped %d: the cut line in the middle must be dropped, not counted", s.Skipped)
	}
	if last := s.Turns[len(s.Turns)-1]; last.Text != "the last prompt" {
		t.Errorf("last turn %q", last.Text)
	}
}
