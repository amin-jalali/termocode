package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webAIWorktreeRig is a git repository (the workspace) with two agent
// worktrees left over: one changed a.txt and added x.txt and .env, the
// other added y.txt. The hub has one controller connection.
func webAIWorktreeRig(t *testing.T) (*webFilesRig, string) {
	t.Helper()
	m, dir := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	gitInit(t, dir)
	mk := func(id string, files map[string]string) {
		w, err := git.CreateAgentWorktree(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(w.Path, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("old-x", map[string]string{"a.txt": "agent\n", "x.txt": "x\n", ".env": "KEY=" + "sk-" + "test\n"})
	mk("old-y", map[string]string{"y.txt": "y\n"})
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, root: dir, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return r, dir
}

// Spec (WEBP-3): ai.worktrees lists the repository's agent worktrees as
// the laptop's AI: Agent Worktrees… does, with each one's branch and the
// files it changed; ai.worktree_text reads a file before and after the
// agent, but a file that may hold secrets stays on the laptop.
func TestWebAIWorktreesListAndText(t *testing.T) {
	r, _ := webAIWorktreeRig(t)
	var list webproto.AIWorktrees
	webResData(t, r.call(webproto.OpAIWorktrees, &webproto.EmptyArgs{}), &list)
	if list.Off || list.NoRepo || len(list.Items) != 2 {
		t.Fatalf("list: %+v", list)
	}
	byID := map[string]webproto.AIWorktree{}
	for _, it := range list.Items {
		byID[it.ID] = it
	}
	x := byID["old-x"]
	if x.Branch != git.AgentBranchPrefix+"old-x" || x.Owner != "" || x.Running {
		t.Errorf("old-x: %+v", x)
	}
	var files []string
	for _, f := range x.Files {
		files = append(files, f.Status+" "+f.Path)
	}
	if got := strings.Join(files, ","); got != "A .env,M a.txt,A x.txt" {
		t.Errorf("old-x files %q", got)
	}

	var txt webproto.AIWorktreeText
	webResData(t, r.call(webproto.OpAIWorktreeText, &webproto.AIWorktreeTextArgs{ID: "old-x", Path: "a.txt"}), &txt)
	if txt.Before != "mine\n" || txt.After != "agent\n" || txt.Secret || txt.Binary {
		t.Errorf("a.txt: %+v", txt)
	}
	f := r.call(webproto.OpAIWorktreeText, &webproto.AIWorktreeTextArgs{ID: "old-x", Path: ".env"})
	webResData(t, f, &txt)
	if !txt.Secret || txt.After != "" || strings.Contains(fmt.Sprint(f), "sk-"+"test") {
		t.Errorf(".env left the laptop: %v", f)
	}
	if code := webErrCode(r.call(webproto.OpAIWorktreeText, &webproto.AIWorktreeTextArgs{ID: "old-x", Path: "nope.txt"})); code != "not_found" {
		t.Errorf("unchanged file: %s", code)
	}
	if code := webErrCode(r.call(webproto.OpAIWorktreeText, &webproto.AIWorktreeTextArgs{ID: "gone", Path: "a.txt"})); code != "not_found" {
		t.Errorf("unknown worktree: %s", code)
	}
}

// Spec (WEBP-3): Merge and Discard from the page wait for a laptop card
// (Cancel first); nothing changes until the laptop confirms. Merge then
// writes the agent's files into the tree and removes its worktree;
// Discard removes the worktree and its branch, the tree unchanged.
func TestWebAIWorktreeMergeDiscardThroughTheLaptop(t *testing.T) {
	r, dir := webAIWorktreeRig(t)
	m := r.m
	f := r.call(webproto.OpAIWorktreeMerge, &webproto.AIWorktreeArgs{ID: "old-y"})
	if f["t"] != "res" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("merge asked: %v open %v", f, m.confirmOpen)
	}
	if _, err := os.Stat(filepath.Join(dir, "y.txt")); !os.IsNotExist(err) {
		t.Fatal("merged before the laptop said yes")
	}
	m.confirmOpen = false
	runWorktreeCmd(t, m, m.answerWebAsk(webproto.ChoiceConfirm))
	if got := readFile(filepath.Join(dir, "y.txt")); got != "y\n" {
		t.Errorf("y.txt %q", got)
	}
	if wt, err := git.AgentWorktreePath(dir, "old-y"); err != nil {
		t.Fatal(err)
	} else if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("old-y worktree still there after merge")
	}

	r.call(webproto.OpAIWorktreeDiscard, &webproto.AIWorktreeArgs{ID: "old-x"})
	m.confirmOpen = false
	if cmd := m.answerWebAsk(webproto.ChoiceCancel); cmd != nil {
		t.Fatal("cancel ran something")
	}
	if !gitBranchExists(t, dir, git.AgentBranchPrefix+"old-x") {
		t.Fatal("cancel discarded")
	}
	r.call(webproto.OpAIWorktreeDiscard, &webproto.AIWorktreeArgs{ID: "old-x"})
	m.confirmOpen = false
	runWorktreeCmd(t, m, m.answerWebAsk(webproto.ChoiceConfirm))
	if gitBranchExists(t, dir, git.AgentBranchPrefix+"old-x") || readFile(filepath.Join(dir, "a.txt")) != "mine\n" {
		t.Error("discard: branch still there or the tree changed")
	}
}

// Spec (WEBP-3): a worktree whose step still runs is in use: the page
// cannot read, merge or discard it.
func TestWebAIWorktreeInUse(t *testing.T) {
	r, _ := webAIWorktreeRig(t)
	s := r.m.aiS()
	s.conv = ai.Conversation{ID: "c1", Messages: []ai.StoredMessage{{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1"}, Steps: []ai.StoredStep{
		{ID: "left", Title: "Left", State: "running", Worktree: &ai.StoredWorktree{ID: "old-x", Branch: git.AgentBranchPrefix + "old-x", State: ai.WorktreeRunning}},
	}}}}
	s.run = &agentrun.Handle{}
	var list webproto.AIWorktrees
	webResData(t, r.call(webproto.OpAIWorktrees, &webproto.EmptyArgs{}), &list)
	for _, it := range list.Items {
		if it.ID == "old-x" && (!it.Running || it.Owner != "Left" || len(it.Files) != 0) {
			t.Errorf("running: %+v", it)
		}
	}
	for _, op := range []string{webproto.OpAIWorktreeMerge, webproto.OpAIWorktreeDiscard} {
		if code := webErrCode(r.call(op, &webproto.AIWorktreeArgs{ID: "old-x"})); code != "busy" || r.m.confirmOpen {
			t.Errorf("%s: %s (card %v)", op, code, r.m.confirmOpen)
		}
	}
	if code := webErrCode(r.call(webproto.OpAIWorktreeText, &webproto.AIWorktreeTextArgs{ID: "old-x", Path: "a.txt"})); code != "busy" {
		t.Errorf("text: %s", code)
	}
}
