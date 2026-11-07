package app

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/amin-jalali/termocode/internal/git"
)

// rcStashRepo is the stash list of the fake container repo: a push
// adds one, a drop or pop removes one.
type rcStashRepo struct {
	mu   sync.Mutex
	msgs []string // stash@{0} first
}

func (s *rcStashRepo) answer(argv []string) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := strings.Join(argv, " ")
	switch {
	case strings.HasPrefix(line, "git stash list"):
		var b strings.Builder
		for i, m := range s.msgs {
			b.WriteString("stash@{" + string(rune('0'+i)) + "}|" + m + "\n")
		}
		return b.String(), 0
	case strings.HasPrefix(line, "git rev-parse --verify -q stash@{"):
		if len(s.msgs) == 0 {
			return "", 1
		}
		return strings.Repeat(string(rune('a'+len(s.msgs))), 40) + "\n", 0
	case strings.HasPrefix(line, "git stash push"):
		s.msgs = append([]string{"On remote-main: box wip"}, s.msgs...)
		return "", 0
	case strings.HasPrefix(line, "git stash drop"), strings.HasPrefix(line, "git stash pop"):
		if len(s.msgs) > 0 {
			s.msgs = s.msgs[1:]
		}
		return "", 0
	case strings.HasPrefix(line, "git stash show"):
		// A real diff: the web view reads + lines only inside a hunk.
		return "diff --git a/box.go b/box.go\n--- a/box.go\n+++ b/box.go\n@@ -1,0 +1,1 @@\n+in the container\n", 0
	case strings.HasPrefix(line, "git stash apply"), strings.HasPrefix(line, "git stash branch"):
		return "", 0
	}
	return rcGitAnswer(argv)
}

// rcStashWorkspace is a container workspace whose repo has one stash.
func rcStashWorkspace(t *testing.T) (*Model, *rcHost) {
	t.Helper()
	ws, h := newRCWorkspace(t, fstest.MapFS{"remote.go": {Data: []byte("package main\n")}})
	s := &rcStashRepo{msgs: []string{"On remote-main: old wip"}}
	h.exec.answer = s.answer
	setProcWorkspace(t, ws)
	m := &Model{ws: ws}
	if !m.gitEnsureLoaded() {
		t.Fatal("the container repo was not seen")
	}
	return m, h
}

func rcRan(h *rcHost, want string) bool {
	for _, argv := range h.exec.calls() {
		if strings.Join(argv, " ") == want {
			return true
		}
	}
	return false
}

// Spec: the STASHES section of a container workspace lists the
// container's stashes, read through the Host Exec.
func TestRCGitStashSectionRunsInContainer(t *testing.T) {
	m, h := rcStashWorkspace(t)
	want := []git.StashEntry{{Ref: "stash@{0}", Message: "On remote-main: old wip"}}
	if !slices.Equal(m.gitStashes, want) {
		t.Errorf("stashes %+v, want %+v", m.gitStashes, want)
	}
	msg, ok := m.pollGitCmd()().(GitMsg)
	if !ok || !slices.Equal(msg.Stashes, want) {
		t.Errorf("poll stashes %+v", msg.Stashes)
	}
	if !rcRan(h, "git stash list --pretty=%gd|%s") {
		t.Errorf("stash list not run in the container: %q", h.exec.calls())
	}
}

// Spec: the stash actions (Git: Stashes… and the STASHES rows) of a
// container workspace run git in the container: apply, pop, view, drop
// and branch.
func TestRCGitStashActionsRunInContainer(t *testing.T) {
	m, h := rcStashWorkspace(t)
	m.openStashActionsPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindStashPick {
		t.Fatal("no stash list picker for the container's stash")
	}
	m.onStashRow(0)
	if m.gitStashOps.ref != "stash@{0}" {
		t.Fatalf("ref %q", m.gitStashOps.ref)
	}
	m.onStashActionPicked("sa-apply")
	m.onStashActionPicked("sa-diff")
	if !strings.Contains(m.gitStashOps.page, "in the container") {
		t.Errorf("View Changes page %q, want the container's diff", m.gitStashOps.page)
	}
	m.onStashActionPicked("sa-drop")
	if !m.confirmOpen || m.confirmKind != confirmKindStashDrop {
		t.Fatal("no drop card")
	}
	m.onGitOpsConfirm(confirmKindStashDrop, "drop")

	m.gitStashOps.ref = "stash@{0}"
	m.promptKind = promptKindStashBranch
	m.handleGitOpsPromptSubmit("topic")
	m.onStashActionPicked("sa-pop")

	for _, want := range []string{
		"git stash apply stash@{0}",
		"git stash show --no-color -p --include-untracked stash@{0}",
		"git stash drop stash@{0}",
		"git stash branch topic stash@{0}",
	} {
		if !rcRan(h, want) {
			t.Errorf("%q not run in the container: %q", want, h.exec.calls())
		}
	}
}

// Spec: the stash options (new files, staged only, one file, a message)
// of a container workspace go to `git stash push` in the container; the
// older Stash, Pop Stash and Stash List commands do too.
func TestRCGitStashOptionsRunInContainer(t *testing.T) {
	m, h := rcStashWorkspace(t)
	m.openStashModePicker()
	if !m.pickerOpen || m.pickerKind != pickerKindStashMode {
		t.Fatal("no Stash what? picker")
	}
	m.onStashModePicked("stash-untracked")
	if !m.promptOpen || m.promptKind != promptKindStashMessage {
		t.Fatal("no message prompt")
	}
	m.handleGitOpsPromptSubmit("box wip")
	m.gitStashOps.opts = git.StashOptions{StagedOnly: true}
	m.promptKind = promptKindStashMessage
	m.handleGitOpsPromptSubmit("")

	m.gitStash()
	m.gitStashPop()
	m.openStashPicker()
	if !m.pickerOpen || m.pickerKind != pickerKindStash {
		t.Fatal("no Stash List picker for the container's stashes")
	}
	m.applyStashSelected("stash@{1}")

	for _, want := range []string{
		"git stash push -m box wip --include-untracked",
		"git stash push --staged",
		"git stash push",
		"git stash pop",
		"git stash apply stash@{1}",
	} {
		if !rcRan(h, want) {
			t.Errorf("%q not run in the container: %q", want, h.exec.calls())
		}
	}
	for _, argv := range h.exec.calls() {
		if argv[0] != "git" {
			t.Errorf("not a git command: %q", argv)
		}
	}
}

// Spec: a container workspace whose host cannot run commands shows no
// stashes and its stash actions fail with a toast; nothing runs on the
// local disk.
func TestRCGitStashNoHostExec(t *testing.T) {
	ws, _ := newRCWorkspace(t, nil)
	ws.Host = nil
	m := &Model{ws: ws, gitIsRepo: true, gitLoaded: true}
	rd, err := gitReaderOf(ws)
	if err != nil {
		t.Fatal(err)
	}
	if es := rd.stashes(); es != nil {
		t.Errorf("stashes %+v, want none", es)
	}
	m.openStashActionsPicker()
	if m.pickerOpen {
		t.Error("a stash picker opened with no Host Exec")
	}
}
