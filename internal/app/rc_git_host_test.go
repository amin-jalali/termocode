package app

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// rcGitAnswer answers the git reads of the Source Control snapshot as a
// container repo on branch remote-main with one new file would.
func rcGitAnswer(argv []string) (string, int) {
	if len(argv) == 0 || argv[0] != "git" {
		return "", 127
	}
	switch {
	case slices.Contains(argv, "status"):
		return "# branch.oid 1111111111111111111111111111111111111111\x00# branch.head remote-main\x00? remote.go\x00", 0
	case slices.Contains(argv, "log"):
		return "1111111\x1f1111111111111111111111111111111111111111\x1f\x1fcontainer commit\x1fdev\x1f1700000000\x1fHEAD -> refs/heads/remote-main\x1e", 0
	}
	return "", 128 // no upstream, no rebase
}

func rcGitWorkspace(t *testing.T) *rcHost {
	t.Helper()
	ws, h := newRCWorkspace(t, fstest.MapFS{"remote.go": {Data: []byte("package main\n")}})
	h.exec.answer = rcGitAnswer
	setProcWorkspace(t, ws)
	return h
}

func checkRCGitMsg(t *testing.T, msg GitMsg, h *rcHost) {
	t.Helper()
	if !msg.IsRepo {
		t.Fatal("the container repo was not seen (git ran on the local disk?)")
	}
	if msg.Branch.Name != "remote-main" {
		t.Errorf("branch %q, want remote-main", msg.Branch.Name)
	}
	if len(msg.Files) != 1 || msg.Files[0].Path != "remote.go" || msg.Files[0].Code != "??" {
		t.Errorf("files %+v", msg.Files)
	}
	if len(msg.Graph) != 1 || msg.Graph[0].Subject != "container commit" {
		t.Errorf("graph %+v", msg.Graph)
	}
	for _, argv := range h.exec.calls() {
		if argv[0] != "git" {
			t.Errorf("not a git command: %q", strings.Join(argv, " "))
		}
	}
}

// Spec (FIX-3): the Source Control poll of a container workspace runs git
// in the container through the Host Exec, never on the local disk.
func TestRCGitPollRunsInContainer(t *testing.T) {
	h := rcGitWorkspace(t)
	m := Model{ws: procWorkspace()}
	msg, ok := m.pollGitCmd()().(GitMsg)
	if !ok {
		t.Fatal("not a GitMsg")
	}
	checkRCGitMsg(t, msg, h)
}

// Spec (FIX-3): the first full git read (start, manual refresh) of a
// container workspace runs in the container too.
func TestRCGitFetchRunsInContainer(t *testing.T) {
	h := rcGitWorkspace(t)
	msg, ok := fetchGitCmd()().(GitMsg)
	if !ok {
		t.Fatal("not a GitMsg")
	}
	checkRCGitMsg(t, msg, h)
}
