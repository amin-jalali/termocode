package bgtask

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAgentArgvUsesHeadlessModeAndNpx(t *testing.T) {
	got, err := AgentArgv("claude", "  fix the toast race ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"npx", "-y", "@anthropic-ai/claude-code", "-p", "fix the toast race", "--dangerously-skip-permissions"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	got, err = AgentArgv("aider", "x")
	if err != nil || got[0] != "aider" {
		t.Fatalf("aider runs its own program, got %q %v", got, err)
	}
}

func TestAgentArgvRefusesUnknownAndEmpty(t *testing.T) {
	if _, err := AgentArgv("goose", "x"); err == nil {
		t.Fatal("goose has no known headless mode")
	}
	if _, err := AgentArgv("claude", "   "); err == nil {
		t.Fatal("an empty task is refused")
	}
	if _, err := AgentArgv("claude", strings.Repeat("a", TaskMax+1)); err == nil {
		t.Fatal("a too long task is refused")
	}
}

func TestRunArgvSandboxMountsOnlyTheWorktreeAndCommonDir(t *testing.T) {
	s := Spec{ID: "20261107-120000-a", Worktree: "/srv/app.worktrees/fix", Common: "/srv/app/.git",
		Agent: "codex", Task: "add tests\nfor the parser", User: "1000:1000"}
	argv, err := RunArgv(s)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(argv, " ")
	for _, want := range []string{
		"run -d --name termocode-task-20261107-120000-a",
		"--label termocode.title=add tests for the parser",
		"--cap-drop ALL", "--security-opt no-new-privileges",
		"-v /srv/app.worktrees/fix:/srv/app.worktrees/fix",
		"-v /srv/app/.git:/srv/app/.git",
		"-w /srv/app.worktrees/fix",
		"-e ANTHROPIC_API_KEY", "--user 1000:1000",
		DefaultImage + " npx -y @openai/codex exec",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("argv lacks %q:\n%s", want, line)
		}
	}
	for _, a := range argv {
		if strings.Contains(a, "=") && strings.HasPrefix(a, "ANTHROPIC") {
			t.Fatalf("a key value is on the command line: %q", a)
		}
	}
	if argv[len(argv)-1] != "add tests\nfor the parser" {
		t.Fatalf("the task is the last word, got %q", argv[len(argv)-1])
	}
}

func TestRunArgvCommonInsideWorktreeIsNotMountedTwice(t *testing.T) {
	argv, err := RunArgv(Spec{ID: "a1", Worktree: "/r", Common: "/r/.git", Agent: "claude", Task: "x"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range argv {
		if a == "-v" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mounts = %d, want 1: %q", n, argv)
	}
}

func TestRunArgvRefusesBadInput(t *testing.T) {
	ok := Spec{ID: "a1", Worktree: "/r", Agent: "claude", Task: "x"}
	for name, mut := range map[string]func(*Spec){
		"id":       func(s *Spec) { s.ID = "A B" },
		"relative": func(s *Spec) { s.Worktree = "r" },
		"colon":    func(s *Spec) { s.Worktree = "/r:/etc" },
		"common":   func(s *Spec) { s.Common = "/c,ro" },
		"image":    func(s *Spec) { s.Image = "--privileged" },
		"agent":    func(s *Spec) { s.Agent = "droid" },
	} {
		s := ok
		mut(&s)
		if _, err := RunArgv(s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
}

func TestParseListStates(t *testing.T) {
	out := strings.Join([]string{
		"termocode-task-a1\trunning\tUp 3 minutes\ta1\t/r/w\tclaude\tfix it",
		"termocode-task-b2\texited\tExited (0) 1 hour ago\tb2\t/r/w\tcodex\tdone one",
		"termocode-task-c3\texited\tExited (137) 2 days ago\tc3\t/r/x\tgemini\tkilled",
		"other\trunning\tUp\tzz\t/r\tclaude\tnot ours",
		"termocode-task-d4\tcreated\tCreated\td4\t/r\tclaude\twaiting",
		"short line",
	}, "\n")
	got := ParseList(out)
	if len(got) != 4 {
		t.Fatalf("tasks = %d, want 4: %+v", len(got), got)
	}
	want := []struct {
		s    State
		exit int
	}{{Running, -1}, {Done, 0}, {Failed, 137}, {Other, -1}}
	for i, w := range want {
		if got[i].State != w.s || got[i].Exit != w.exit {
			t.Errorf("%d: %s %d, want %s %d", i, got[i].State, got[i].Exit, w.s, w.exit)
		}
	}
	if got[0].Worktree != "/r/w" || got[0].Agent != "claude" || got[0].Title != "fix it" {
		t.Fatalf("fields: %+v", got[0])
	}
}

func TestListArgvPerEngine(t *testing.T) {
	d := strings.Join(ListArgv("docker"), " ")
	p := strings.Join(ListArgv("podman"), " ")
	if !strings.Contains(d, `{{.Label "termocode.task"}}`) || !strings.Contains(p, `{{index .Labels "termocode.task"}}`) {
		t.Fatalf("docker %s\npodman %s", d, p)
	}
	if !strings.Contains(d, "--filter label=termocode.task") {
		t.Fatal("only task containers are listed")
	}
}

func TestNewIDIsValidAndTitleIsOneLine(t *testing.T) {
	id := NewID(time.Date(2026, 11, 7, 12, 0, 1, 5e8, time.UTC))
	if !ValidID(id) || !strings.HasPrefix(id, "20261107-120001-") {
		t.Fatalf("id %q", id)
	}
	if got := Title("a\n\tb   c"); got != "a b c" {
		t.Fatalf("title %q", got)
	}
	if got := []rune(Title(strings.Repeat("x", 500))); len(got) != TitleMax {
		t.Fatalf("title runes %d", len(got))
	}
}

func TestLocal(t *testing.T) {
	for _, c := range []struct {
		ctx, host string
		want      bool
	}{{"", "", true}, {"default", "", true}, {"myserver", "", false}, {"", "ssh://me@box", false}, {"", "unix:///run/docker.sock", true}} {
		if got := Local(c.ctx, c.host); got != c.want {
			t.Errorf("Local(%q,%q) = %v", c.ctx, c.host, got)
		}
	}
}

func TestSupportedIDsFollowRegistry(t *testing.T) {
	ids := SupportedIDs()
	if len(ids) == 0 || ids[0] != "claude" || slices.Contains(ids, "goose") {
		t.Fatalf("ids %q", ids)
	}
}
