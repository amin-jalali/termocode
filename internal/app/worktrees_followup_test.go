package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

const (
	wtFbThreads = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"parser.go","line":12,"diffSide":"RIGHT",
  "comments":{"nodes":[{"databaseId":501,"author":{"login":"bob"},"body":"Why a map?","createdAt":"","diffHunk":"","url":"u501"}]}}]}}}}}`
	wtFbConvo = `[{"id":902,"user":{"login":"ann"},"body":"@termocode add a test","created_at":"","html_url":"u902"}]`
	wtFbRuns  = `[{"databaseId":30,"workflowName":"CI","displayTitle":"x","status":"completed","conclusion":"failure","headSha":"bbb","url":"u30"}]`
)

// wtFbGH answers the gh calls of the feedback read; it counts them.
type wtFbGH struct{ calls []string }

func (f *wtFbGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	line := strings.Join(args, " ")
	f.calls = append(f.calls, line)
	switch {
	case strings.HasPrefix(line, "api graphql"):
		return wtFbThreads, "", nil
	case strings.HasPrefix(line, "api repos/{owner}/{repo}/issues/14/comments"):
		return wtFbConvo, "", nil
	case strings.HasPrefix(line, "run list"):
		return wtFbRuns, "", nil
	case strings.HasPrefix(line, "pr view"):
		return `{"number":14,"title":"t","state":"OPEN","url":"u"}`, "", nil
	case strings.HasPrefix(line, "run view"):
		return "test\tgo test\t--- FAIL: TestX\n", "", nil
	}
	return "", "unknown " + line, wtErrString("exit status 1")
}

// wtFbModel: wt-sidebar has PR #14 and a Claude Code tab; gh is faked and
// the typing is recorded.
func wtFbModel(t *testing.T) (*Model, *wtFbGH, *[]string) {
	t.Helper()
	wtUnicodeIcons(t)
	withConfigDir(t)
	m := wtSleepModel(t)
	for i := range m.wt.entries {
		if m.wt.entries[i].Path == wtSide {
			m.wt.entries[i].Meta.PR = 14
		}
	}
	m.wt.cursorPath = wtSide
	gh := &wtFbGH{}
	oldRun := wtGHRun
	wtGHRun = func() ghci.Runner { return gh.run }
	var typed []string
	oldType := wtFeedbackType
	wtFeedbackType = func(_ *Model, buf int, line string) { typed = append(typed, line) }
	t.Cleanup(func() { wtGHRun, wtFeedbackType = oldRun, oldType })
	return m, gh, &typed
}

// CMP-31: r reads the PR (threads, mentions, failing CI), asks on a
// Cancel-first card that names the agent, and Send writes the file in
// the git folder and types one line into the agent tab; what was sent
// is marked seen.
func TestWorktreeFeedbackSend(t *testing.T) {
	m, _, typed := wtFbModel(t)
	wtLinkDrive(t, m, wtKey(t, m, "W"))
	if !m.confirmOpen || m.confirmKind != confirmKindWTFeedback || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"PR #14", "1 review comment", "1 mention", "CI failed", "parser.go:12", "Why a map?"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[1].ID != "send" {
		t.Fatalf("buttons %+v", b)
	}
	m.confirmOpen = false
	m.answerWTFeedback("send")
	if len(*typed) != 1 || strings.ContainsAny((*typed)[0], "\r\n") || !strings.Contains((*typed)[0], "PR #14") {
		t.Fatalf("typed %q", *typed)
	}
	file := m.wt.wtFeedbackFile(wtSide, 14)
	if !strings.HasPrefix(file, m.wt.common) || !strings.Contains((*typed)[0], file) {
		t.Errorf("file %q not in the git folder or not named: %q", file, (*typed)[0])
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"parser.go:12", "Why a map?", "@termocode add a test", "--- FAIL: TestX", "do not push"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("file misses %q:\n%s", want, body)
		}
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v", fi.Mode().Perm())
	}
	md, _ := worktrees.Load(m.wt.common)
	if got := md[wtSide]; len(got.FollowSeen) != 2 || got.FollowRun != 30 {
		t.Errorf("seen not kept: %+v", got)
	}
}

// Cancel and Esc send nothing and mark nothing; without an agent tab the
// card offers the AI chat instead.
func TestWorktreeFeedbackCancelAndNoAgent(t *testing.T) {
	m, _, typed := wtFbModel(t)
	wtLinkDrive(t, m, wtKey(t, m, "W"))
	m.confirmOpen = false
	m.answerWTFeedback("")
	if len(*typed) != 0 {
		t.Errorf("Esc typed %q", *typed)
	}
	if md, _ := worktrees.Load(m.wt.common); len(md[wtSide].FollowSeen) != 0 {
		t.Errorf("Esc marked seen: %+v", md[wtSide])
	}
	m.terminalTabs = m.terminalTabs[1:] // the Claude Code tab ends
	wtLinkDrive(t, m, wtKey(t, m, "W"))
	if b := m.confirm.Buttons(); !m.confirmOpen || len(b) != 2 || b[1].ID != "chat" {
		t.Fatalf("no AI chat button: %+v", b)
	}
}

// w follows a linked PR (the card says so); the loop asks with Skip
// first, Skip marks it seen and the next read asks nothing; w again
// stops it. Without a PR w only says to link one.
func TestWorktreeFollowLoop(t *testing.T) {
	m, gh, typed := wtFbModel(t)
	wtKey(t, m, "w")
	if md, _ := worktrees.Load(m.wt.common); !md[wtSide].Follow {
		t.Fatalf("not following: %+v", md[wtSide])
	}
	if l := wtLinkLine(m.wtMetaOf(wtSide)); !strings.Contains(l, "following") {
		t.Errorf("link line %q", l)
	}
	now := time.Now()
	wtLinkDrive(t, m, m.wtFollowRefresh(now))
	if !m.confirmOpen || m.confirmKind != confirmKindWTFeedback || m.confirm.SafeID() != "skip" {
		t.Fatalf("no Skip-first card: open %v safe %q", m.confirmOpen, m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.answerWTFeedback("skip")
	if len(*typed) != 0 {
		t.Errorf("Skip typed %q", *typed)
	}
	// Not due yet: no read at all.
	n := len(gh.calls)
	if c := m.wtFollowRefresh(now.Add(time.Second)); c != nil {
		wtLinkDrive(t, m, c)
	}
	if len(gh.calls) != n {
		t.Errorf("read before it was due: %v", gh.calls[n:])
	}
	// Due again: everything is seen, so no card.
	wtLinkDrive(t, m, m.wtFollowRefresh(now.Add(wtFollowEvery+time.Minute)))
	if m.confirmOpen {
		t.Errorf("asked again about seen feedback:\n%s", ansi.Strip(m.confirm.Box()))
	}
	wtKey(t, m, "w")
	if md, _ := worktrees.Load(m.wt.common); md[wtSide].Follow {
		t.Error("w again did not stop it")
	}
	m.wt.cursorPath = wtToast // no PR linked
	wtKey(t, m, "w")
	if md, _ := worktrees.Load(m.wt.common); md[wtToast].Follow {
		t.Error("followed without a PR")
	}
}
