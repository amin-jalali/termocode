package app

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// rcWebStashWorkspace is a container workspace whose repo has two
// stashes, with a web hub and one browser on it.
func rcWebStashWorkspace(t *testing.T) (*Model, *rcHost, *webConn) {
	t.Helper()
	ws, h := newRCWorkspace(t, fstest.MapFS{"remote.go": {Data: []byte("package main\n")}})
	s := &rcStashRepo{msgs: []string{"On remote-main: new wip", "On remote-main: old wip"}}
	h.exec.answer = s.answer
	setProcWorkspace(t, ws)
	mm := aiTestModel(t)
	m := &mm
	m.ws = ws
	m.output = newOutputStore()
	webTestHub(t, m)
	webTestGitLoad(m)
	if !m.gitIsRepo {
		t.Fatal("the container repo was not seen")
	}
	return m, h, webTestSub(t, m)
}

// Spec: the web STASHES section of a container workspace lists the
// container's stash; View reads the entry's changes in the container.
func TestRCWebGitStashListAndShowRunInContainer(t *testing.T) {
	m, h, c := rcWebStashWorkspace(t)
	snap := m.webGitBuild()
	if len(snap.Stashes) != 2 || snap.Stashes[1].Ref != "stash@{1}" || !strings.Contains(snap.Stashes[1].Message, "old wip") {
		t.Fatalf("stashes in the topic %+v", snap.Stashes)
	}
	var d struct {
		Rows []struct {
			Text string `json:"text"`
		} `json:"rows"`
	}
	webTestData(t, webTestCallBg(t, m, c, "git.stash_show", `{"ref":"stash@{1}"}`), &d)
	found := false
	for _, r := range d.Rows {
		found = found || strings.Contains(r.Text, "in the container")
	}
	if !found {
		t.Errorf("show rows %+v, want the container's diff", d.Rows)
	}
	if !rcRan(h, "git stash show --no-color -p --include-untracked stash@{1}") {
		t.Errorf("stash show not run in the container: %q", h.exec.calls())
	}
}

// Spec: the web stash actions (Apply, Pop, Drop, Create Branch) of a
// container workspace wait for the laptop card, then check the entry
// and run git in the container; nothing runs on the local disk.
func TestRCWebGitStashActionsRunInContainer(t *testing.T) {
	m, h, c := rcWebStashWorkspace(t)
	for _, a := range []struct{ args, want string }{
		{`{"ref":"stash@{1}","do":"apply"}`, "git stash apply stash@{1}"},
		{`{"ref":"stash@{1}","do":"drop"}`, "git stash drop stash@{1}"},
		{`{"ref":"stash@{0}","do":"branch","branch":"topic"}`, "git stash branch topic stash@{0}"},
		{`{"ref":"stash@{0}","do":"pop"}`, "git stash apply stash@{0}"},
	} {
		webTestGitLoad(m)
		var ref webproto.AskRef
		webTestData(t, webTestCallBg(t, m, c, "git.stash_act", a.args), &ref)
		if ref.Card == "" || m.confirm.SafeID() != "cancel" {
			t.Fatalf("%s: no safe-first card %+v", a.args, ref)
		}
		if rcRan(h, a.want) {
			t.Fatalf("%s: ran before the laptop said so", a.args)
		}
		webTestLaptopConfirmRemote(t, m)
		if !rcRan(h, a.want) {
			t.Errorf("%q not run in the container: %q", a.want, h.exec.calls())
		}
	}
	for _, argv := range h.exec.calls() {
		if argv[0] != "git" {
			t.Errorf("not a git command: %q", argv)
		}
	}
}
