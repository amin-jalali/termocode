//go:build unix

package sdk

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// fakeRuntime is a control socket that answers like a running termocode.
type fakeRuntime struct {
	mu    sync.Mutex
	reqs  []agentctl.Request
	state string // the state of tab t1
	tabs  []agentctl.Tab
}

func (f *fakeRuntime) handle(r agentctl.Request) agentctl.Reply {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r)
	switch r.Op {
	case agentctl.OpList:
		tabs := append([]agentctl.Tab(nil), f.tabs...)
		for i := range tabs {
			if tabs[i].ID == "t1" {
				tabs[i].State = f.state
			}
		}
		return agentctl.Reply{OK: true, Tabs: tabs}
	case agentctl.OpPrompt:
		if r.Tab != "t1" {
			return agentctl.Reply{Error: "no tab " + r.Tab}
		}
		f.state = "working"
		return agentctl.Reply{OK: true}
	case agentctl.OpWait:
		if r.Until == "working" {
			return agentctl.Reply{OK: true, State: f.state}
		}
		f.state = r.Until
		return agentctl.Reply{OK: true, State: r.Until}
	case agentctl.OpReport, agentctl.OpWorktreeNote, agentctl.OpWorktreeStatus, agentctl.OpWorktreeLink:
		return agentctl.Reply{OK: true}
	case agentctl.OpWorktreeList:
		return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{{Path: "/w/a", Branch: "feat/a", Kind: "agent", Here: true}}}
	}
	return agentctl.Reply{Error: "unknown op " + r.Op}
}

func (f *fakeRuntime) last() agentctl.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func startFake(t *testing.T) (*fakeRuntime, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcsdk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeRuntime{state: "waiting", tabs: []agentctl.Tab{
		{ID: "t1", Name: "claude", Agent: "claude", Source: "hook", Active: true},
		{ID: "t2", Name: "shell"},
	}}
	go agentctl.Serve(ln, nil, f.handle)
	return f, sock
}

func TestTabsListsTheRuntimeTabs(t *testing.T) {
	_, sock := startFake(t)
	c := Dial(sock)
	tabs, err := c.Tabs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 || tabs[0].ID != "t1" || tabs[0].Agent != "claude" || tabs[0].State != StateWaiting || !tabs[0].Active || !tabs[0].Hooked {
		t.Fatalf("tabs = %+v", tabs)
	}
	if tabs[1].State != "" || tabs[1].Hooked {
		t.Fatalf("plain shell tab = %+v", tabs[1])
	}
}

func TestPromptSendsTextAndEnter(t *testing.T) {
	f, sock := startFake(t)
	c := Dial(sock)
	if err := c.Prompt(context.Background(), "t1", "fix the tests"); err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r.Op != agentctl.OpPrompt || r.Tab != "t1" || r.Text != "fix the tests" || (r.Enter != nil && !*r.Enter) {
		t.Fatalf("request = %+v", r)
	}
	if err := c.Type(context.Background(), "t1", "draft"); err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Enter == nil || *r.Enter {
		t.Fatalf("Type must not press Enter: %+v", r)
	}
}

func TestPromptErrorFromRuntime(t *testing.T) {
	_, sock := startFake(t)
	err := Dial(sock).Prompt(context.Background(), "t9", "x")
	if err == nil || err.Error() != "no tab t9" {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitUsesTheContextDeadline(t *testing.T) {
	f, sock := startFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st, err := Dial(sock).Wait(ctx, "t1", StateDone)
	if err != nil || st != StateDone {
		t.Fatalf("wait = %q, %v", st, err)
	}
	r := f.last()
	if r.Op != agentctl.OpWait || r.Until != "done" || r.TimeoutS < 80 || r.TimeoutS > 90 {
		t.Fatalf("request = %+v", r)
	}
}

func TestWaitRejectsABadState(t *testing.T) {
	_, sock := startFake(t)
	if _, err := Dial(sock).Wait(context.Background(), "t1", State("idle")); err == nil {
		t.Fatal("want an error for an unknown state")
	}
}

func TestRunPromptsThenWaitsForDone(t *testing.T) {
	f, sock := startFake(t)
	st, err := Dial(sock).Run(context.Background(), "t1", "write the docs")
	if err != nil || st != StateDone {
		t.Fatalf("run = %q, %v", st, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var ops []string
	for _, r := range f.reqs {
		ops = append(ops, r.Op+":"+r.Until)
	}
	if len(ops) < 2 || ops[0] != "prompt:" || ops[len(ops)-1] != "wait:done" {
		t.Fatalf("ops = %v", ops)
	}
}

func TestReportAndWorktrees(t *testing.T) {
	f, sock := startFake(t)
	c := Dial(sock)
	ctx := context.Background()
	if err := c.Report(ctx, "t1", "codex", StateWorking); err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Op != agentctl.OpReport || r.Agent != "codex" || r.State != "working" || r.Tab != "t1" {
		t.Fatalf("report = %+v", r)
	}
	wts, err := c.Worktrees(ctx, "/w/a/sub")
	if err != nil || len(wts) != 1 || wts[0].Branch != "feat/a" || !wts[0].Here {
		t.Fatalf("worktrees = %+v, %v", wts, err)
	}
	if r := f.last(); r.Dir != "/w/a/sub" {
		t.Fatalf("dir = %q", r.Dir)
	}
	if err := c.WorktreeNote(ctx, "/w/a", "half done"); err != nil {
		t.Fatal(err)
	}
	if err := c.WorktreeStatus(ctx, "/w/a", "blocked"); err != nil {
		t.Fatal(err)
	}
	if r := f.last(); r.Op != agentctl.OpWorktreeStatus || r.State != "blocked" {
		t.Fatalf("status = %+v", r)
	}
	if err := c.WorktreeStatus(ctx, "/w/a", "lost"); err == nil {
		t.Fatal("want an error for an unknown worktree status")
	}
	if err := c.WorktreeLink(ctx, "/w/a", "#12"); err != nil {
		t.Fatal(err)
	}
}

func TestFindTab(t *testing.T) {
	_, sock := startFake(t)
	c := Dial(sock)
	tab, err := c.FindTab(context.Background(), func(t Tab) bool { return t.Agent == "claude" })
	if err != nil || tab.ID != "t1" {
		t.Fatalf("find = %+v, %v", tab, err)
	}
	_, err = c.FindTab(context.Background(), func(t Tab) bool { return t.Agent == "gemini" })
	if !errors.Is(err, ErrNoTab) {
		t.Fatalf("err = %v, want ErrNoTab", err)
	}
}

func TestWatchSendsStateChanges(t *testing.T) {
	f, sock := startFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Dial(sock).Watch(ctx, 10*time.Millisecond)
	first := <-ch
	if first.Tab.ID != "t1" || first.State != StateWaiting || first.Was != "" {
		t.Fatalf("first event = %+v", first)
	}
	f.mu.Lock()
	f.state = "done"
	f.mu.Unlock()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Tab.ID == "t1" && ev.State == StateDone {
				if ev.Was != StateWaiting {
					t.Fatalf("was = %q", ev.Was)
				}
				cancel()
				return
			}
		case <-deadline:
			t.Fatal("no change event")
		}
	}
}

func TestDialWithNoSocket(t *testing.T) {
	if _, err := Dial("").Tabs(context.Background()); err == nil {
		t.Fatal("want an error with no socket")
	}
}

func TestFromEnvPrefersTheSocketVariable(t *testing.T) {
	t.Setenv(agentctl.EnvSocket, "/tmp/x.sock")
	t.Setenv(agentctl.EnvTab, "t7")
	t.Setenv(agentctl.EnvMarker, "1")
	c, err := FromEnv()
	if err != nil || c.Socket() != "/tmp/x.sock" {
		t.Fatalf("from env = %v, %v", c, err)
	}
	if MyTab() != "t7" || !InTerminal() {
		t.Fatalf("my tab = %q, in terminal = %v", MyTab(), InTerminal())
	}
}
