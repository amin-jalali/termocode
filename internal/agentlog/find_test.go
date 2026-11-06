package agentlog_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// Spec (AGX-7): the session folders are ~/.claude and ~/.codex, or the
// folders the CLIs' own variables name (CLAUDE_CONFIG_DIR, CODEX_HOME).
func TestHomesFrom(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if h := agentlog.HomesFrom("/home/u", get); h.Claude != "/home/u/.claude" || h.Codex != "/home/u/.codex" {
		t.Errorf("default homes %+v", h)
	}
	env["CLAUDE_CONFIG_DIR"], env["CODEX_HOME"] = "/cfg/claude", "/cfg/codex"
	if h := agentlog.HomesFrom("/home/u", get); h.Claude != "/cfg/claude" || h.Codex != "/cfg/codex" {
		t.Errorf("env homes %+v", h)
	}
}

// Spec (AGX-7): Claude Code keeps a folder's sessions under projects/
// <the folder with every character that is not a letter or digit made
// a dash>.
func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"/work/demo":                 "-work-demo",
		"/home/fu/termocode":         "-home-fu-termocode",
		"/r/my_repo.worktrees/pin-1": "-r-my-repo-worktrees-pin-1",
	} {
		if got := agentlog.Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec (AGX-7): the session file of a tab is the newest file of its CLI
// for the tab's folder that changed since the agent started: Claude Code
// by the folder's slug, Codex by the cwd in the file's first line. No
// file for another folder, none older than the start.
func TestLatest(t *testing.T) {
	h := agentlog.HomesFrom(agentlogtest.FakeHome(t), func(string) string { return "" })
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, cli := range []agentlog.CLI{agentlog.Claude, agentlog.Codex} {
		ref, ok := h.Latest(cli, agentlogtest.Folder, old)
		if !ok || ref.CLI != cli || !strings.HasSuffix(ref.Path, ".jsonl") {
			t.Errorf("%s: %+v %v", cli, ref, ok)
		}
		if _, ok := h.Latest(cli, "/work/other", old); ok {
			t.Errorf("%s: a file for another folder", cli)
		}
		if _, ok := h.Latest(cli, agentlogtest.Folder, time.Now().Add(time.Hour)); ok {
			t.Errorf("%s: a file older than the start", cli)
		}
	}
	// A newer Claude Code file of the same folder wins.
	dir := filepath.Join(h.Claude, "projects", "-work-demo")
	newer := filepath.Join(dir, "aaaa0000-0000-4000-8000-000000000009.jsonl")
	if err := os.WriteFile(newer, []byte(`{"type":"user","sessionId":"x","cwd":"/work/demo","timestamp":"2026-11-06T10:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(newer, future, future)
	if ref, _ := h.Latest(agentlog.Claude, agentlogtest.Folder, old); ref.Path != newer {
		t.Errorf("latest %s, want the newer file", ref.Path)
	}
	// Side task files (agent-*.jsonl) are not sessions.
	side := filepath.Join(dir, "agent-1234.jsonl")
	_ = os.WriteFile(side, []byte("{}\n"), 0o600)
	far := future.Add(time.Minute)
	_ = os.Chtimes(side, far, far)
	if ref, _ := h.Latest(agentlog.Claude, agentlogtest.Folder, old); ref.Path != newer {
		t.Errorf("latest %s: a side task file is not a session", ref.Path)
	}
}

// Spec (AGX-7, AGX-9): Files lists the sessions of a project from both
// CLIs: the folder itself and the folders under it, and its worktrees
// beside it (<repo>.worktrees/…); not a folder that only starts with the
// same letters.
func TestFilesOfProject(t *testing.T) {
	home := agentlogtest.FakeHome(t)
	h := agentlog.HomesFrom(home, func(string) string { return "" })
	mk := func(cwd string) {
		dir := filepath.Join(h.Claude, "projects", agentlog.Slug(cwd))
		_ = os.MkdirAll(dir, 0o700)
		line := `{"type":"user","sessionId":"s","cwd":"` + cwd + `","timestamp":"2026-11-06T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "s-"+agentlog.Slug(cwd)+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk("/work/demo/sub")
	mk("/work/demo.worktrees/pin")
	mk("/work/demo-old")
	var got []string
	for _, r := range h.Files("/work/demo") {
		got = append(got, string(r.CLI)+" "+filepath.Base(filepath.Dir(r.Path)))
	}
	sort.Strings(got)
	want := "claude -work-demo|claude -work-demo-sub|claude -work-demo-worktrees-pin|codex 05"
	if strings.Join(got, "|") != want {
		t.Errorf("files %v, want %s", got, want)
	}
}

// Spec (AGX-7): a tail reads what the CLI added since the last poll;
// a half line waits for its newline; a file that got shorter (a new
// session in the same file) is read again from the start.
func TestTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(s string, appendTo bool) {
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		f, err := os.OpenFile(p, flag, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(s)
		f.Close()
	}
	user := func(text string) string {
		return `{"type":"user","sessionId":"s","timestamp":"2026-11-06T10:00:00Z","message":{"role":"user","content":"` + text + `"}}`
	}
	write(user("one")+"\n", false)
	tl := agentlog.NewTail(agentlog.Claude, p)
	if changed, err := tl.Poll(); !changed || err != nil {
		t.Fatalf("first poll %v %v", changed, err)
	}
	if changed, _ := tl.Poll(); changed {
		t.Error("no new bytes, but changed")
	}
	half := user("two")
	write(half[:20], true)
	if _, err := tl.Poll(); err != nil || len(tl.Session().Turns) != 1 {
		t.Errorf("half line: %v, %d turns", err, len(tl.Session().Turns))
	}
	write(half[20:]+"\n", true)
	if changed, _ := tl.Poll(); !changed || len(tl.Session().Turns) != 2 || tl.Session().Turns[1].Text != "two" {
		t.Errorf("after the newline: %+v", tl.Session().Turns)
	}
	write(user("fresh")+"\n", false)
	if changed, _ := tl.Poll(); !changed {
		t.Error("a shorter file is not read again")
	}
	if s := tl.Session(); len(s.Turns) != 1 || s.Turns[0].Text != "fresh" {
		t.Errorf("after a restart: %+v", s.Turns)
	}
}

// Spec (AGX-7): the first poll of a 10 MB Claude Code transcript (the
// chat view opening on a long session) is within agent_transcript_tail_10mb.
func TestPerfAgentTranscriptTail10MB(t *testing.T) {
	perfbudget.Require(t)
	p := filepath.Join(t.TempDir(), "big.jsonl")
	var b strings.Builder
	body := strings.Repeat("line of a file the agent read\\n", 60)
	for i := 0; b.Len() < 10<<20; i++ {
		b.WriteString(`{"parentUuid":"p","isSidechain":false,"cwd":"/work/demo","sessionId":"big","type":"assistant","message":{"id":"m` + itoa(i) + `","role":"assistant","model":"model-a","content":[{"type":"tool_use","id":"t` + itoa(i) + `","name":"Read","input":{"file_path":"/work/demo/f.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-11-06T10:00:00Z"}` + "\n")
		b.WriteString(`{"parentUuid":"p","isSidechain":false,"cwd":"/work/demo","sessionId":"big","type":"user","message":{"role":"user","content":[{"tool_use_id":"t` + itoa(i) + `","type":"tool_result","content":"` + body + `"}]},"timestamp":"2026-11-06T10:00:01Z"}` + "\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var ds []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		tl := agentlog.NewTail(agentlog.Claude, p)
		if _, err := tl.Poll(); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	perfbudget.Check(t, "agent_transcript_tail_10mb", float64(ds[len(ds)/2].Microseconds())/1000)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}
