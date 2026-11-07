package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/multirepo"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

func wtMultiTestForm(t *testing.T) *wtMultiForm {
	t.Helper()
	return &wtMultiForm{
		repos: []multirepo.Repo{
			{Name: "api", Main: "/w/api", Common: t.TempDir()},
			{Name: "web", Main: "/w/web", Common: t.TempDir()},
		},
		home: "/home/u",
	}
}

func wtMultiModel(t *testing.T, f *wtMultiForm, picks map[string]int) *Model {
	t.Helper()
	withConfigDir(t)
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.wt.multi = f
	m.connect = connectcard.New(wtMultiCardFor(f, "feat/login", "", picks))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return m
}

func TestWTMultiCardCancelFirstAndFolders(t *testing.T) {
	c := wtMultiCardFor(wtMultiTestForm(t), "feat/login", "", nil)
	if c.Title != wtMultiTitle || len(c.Buttons) < 2 || c.Buttons[0].ID != "cancel" {
		t.Fatalf("card %q buttons %+v: Cancel must come first", c.Title, c.Buttons)
	}
	var text []string
	for _, r := range c.Rows {
		text = append(text, r.Text)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"api", "web", "api.worktrees", "web.worktrees", "feat-login"} {
		if !strings.Contains(all, want) {
			t.Fatalf("card misses %q:\n%s", want, all)
		}
	}
}

func TestWTMultiNeedsTwoRepos(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 150, h: 42, wt: newWTState()}
	_ = m.onWTMultiPrep(wtMultiPrepMsg{f: &wtMultiForm{repos: []multirepo.Repo{{Name: "api", Main: "/w/api"}}}})
	if m.connectOpen || m.wt.multi != nil {
		t.Fatal("one repository is no multi-repo workspace: no card")
	}
}

func TestWTMultiCreateSkipsUnticked(t *testing.T) {
	f := wtMultiTestForm(t)
	m := wtMultiModel(t, f, map[string]int{"repo-1": 1}) // web: no
	var got []multirepo.Item
	old := wtMultiCreateRun
	wtMultiCreateRun = func(items []multirepo.Item, branch, ref string) error {
		got = items
		return nil
	}
	t.Cleanup(func() { wtMultiCreateRun = old })
	oldProbe := wtMultiProbe
	wtMultiProbe = func() multirepo.Probe { return multirepo.Probe{} } // no git: every repo is fine
	t.Cleanup(func() { wtMultiProbe = oldProbe })
	cmd := m.wtMultiCreate(false)
	if cmd == nil || !f.busy {
		t.Fatal("Create must run in the background")
	}
	msg, ok := cmd().(wtMultiCreatedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("msg %+v", msg)
	}
	if len(got) != 1 || got[0].Repo.Name != "api" {
		t.Fatalf("made in %+v, want only api", got)
	}
}

func TestWTMultiCreateNeedsBranch(t *testing.T) {
	f := wtMultiTestForm(t)
	withConfigDir(t)
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.wt.multi = f
	m.connect = connectcard.New(wtMultiCardFor(f, "", "", nil))
	m.connectOpen = true
	if cmd := m.wtMultiCreate(false); cmd != nil || f.fail == "" {
		t.Fatalf("an empty branch must stay on the card (fail %q)", f.fail)
	}
}

func TestWTMultiCreatedTagsGroupAndFailStays(t *testing.T) {
	f := wtMultiTestForm(t)
	m := wtMultiModel(t, f, nil)
	f.busy, f.gen = true, 3
	_ = m.onWTMultiCreated(wtMultiCreatedMsg{gen: 3, err: errors.New("web: the branch feat/login already exists")})
	if !m.connectOpen || !strings.Contains(f.fail, "already exists") || f.busy {
		t.Fatalf("a failed Create keeps the card with git's words (fail %q)", f.fail)
	}
	items := multirepo.Plan(f.repos, "feat/login", "", f.home)
	f.busy, f.gen = true, 4
	_ = m.onWTMultiCreated(wtMultiCreatedMsg{gen: 4, branch: "feat/login", items: items})
	if m.connectOpen || m.wt.multi != nil {
		t.Fatal("the card closes once every worktree is made")
	}
	for _, it := range items {
		all, err := worktrees.Load(it.Repo.Common)
		if err != nil || all[it.Folder].Group != "feat/login" {
			t.Fatalf("%s: group %q err %v", it.Repo.Name, all[it.Folder].Group, err)
		}
	}
}
