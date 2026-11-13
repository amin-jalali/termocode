//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (Git: Show Tags… from the web): git.tags lists the tags, newest
// first; git.checkout_tag takes only a listed one and waits for a laptop
// card that says HEAD ends up detached; then the laptop checks it out.
func TestGitTagsListAndCheckout(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	run("tag", "v1")
	webTestWrite(t, dir, "a.txt", "later\n")
	run("commit", "-q", "-am", "later")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, "git.checkout_tag", `{"tag":"v1"}`); webTestErr(f) != "not_found" {
		t.Fatalf("checkout before the list: %v", f)
	}
	var tags struct {
		Tags []string `json:"tags"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.tags", `{}`), &tags)
	if strings.Join(tags.Tags, ",") != "v1" {
		t.Fatalf("tags %+v", tags)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.checkout_tag", `{"tag":"v1"}`), &ref)
	if ref.Card == "" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "v1") || !strings.Contains(v, "detached") {
		t.Errorf("card:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "later\n" {
		t.Fatal("checked out before the laptop said so")
	}
	webTestLaptopConfirmRemote(t, &m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\n" {
		t.Fatalf("not on v1 after the laptop's yes: %q", b)
	}
}

// Spec (Git: Show Stash List… from the web): git.stash_list lists the
// stash entries; git.stash_apply takes only a listed one and waits for a
// laptop card; then the laptop applies it (the entry stays).
func TestGitStashListAndApply(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "stashed\n")
	run("stash", "push", "-q", "-m", "from test")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, "git.stash_apply", `{"ref":"stash@{0}"}`); webTestErr(f) != "not_found" {
		t.Fatalf("apply before the list: %v", f)
	}
	var list struct {
		Entries []struct {
			Ref     string `json:"ref"`
			Message string `json:"message"`
		} `json:"entries"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_list", `{}`), &list)
	if len(list.Entries) != 1 || list.Entries[0].Ref != "stash@{0}" || !strings.Contains(list.Entries[0].Message, "from test") {
		t.Fatalf("list %+v", list)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_apply", `{"ref":"stash@{0}"}`), &ref)
	if ref.Card == "" || m.confirm.SafeID() != "cancel" || !strings.Contains(m.confirm.View(), "stash@{0}") {
		t.Fatalf("card %+v:\n%s", ref, m.confirm.View())
	}
	webTestLaptopConfirmRemote(t, &m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "stashed\n" {
		t.Fatalf("not applied after the laptop's yes: %q", b)
	}
	if got := strings.TrimSpace(run("stash", "list")); !strings.Contains(got, "from test") {
		t.Errorf("apply dropped the entry: %q", got)
	}
}

// Spec (GITX-10 web, the STASHES section): the git topic lists the stash
// as the laptop's STASHES section has it; git.stash_show sends one
// entry's changes; git.stash_act takes only an entry on show, waits for
// a laptop card (Cancel first), then drops, pops or branches there.
func TestGitStashPanelFromWeb(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "first stash\n")
	run("stash", "push", "-q", "-m", "first")
	webTestWrite(t, dir, "a.txt", "second stash\n")
	run("stash", "push", "-q", "-m", "second")
	webTestGitLoad(&m)
	snap := m.webGitBuild()
	if len(snap.Stashes) != 2 || snap.Stashes[0].Ref != "stash@{0}" || !strings.Contains(snap.Stashes[0].Message, "second") ||
		!strings.Contains(snap.Stashes[1].Message, "first") {
		t.Fatalf("stashes in the topic %+v", snap.Stashes)
	}
	c := webTestSub(t, &m)
	var d struct {
		Rows []struct {
			K    string `json:"k"`
			Text string `json:"text"`
		} `json:"rows"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_show", `{"ref":"stash@{1}"}`), &d)
	found := false
	for _, r := range d.Rows {
		found = found || (r.K == "add" && r.Text == "first stash")
	}
	if !found {
		t.Fatalf("show rows %+v", d.Rows)
	}
	if f := webTestCallBg(t, &m, c, "git.stash_act", `{"ref":"stash@{5}","do":"drop"}`); webTestErr(f) != "not_found" {
		t.Fatalf("an entry not on show: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_act", `{"ref":"stash@{1}","do":"drop"}`), &ref)
	if v := m.confirm.View(); ref.Card == "" || m.confirm.SafeID() != "cancel" || !strings.Contains(v, "stash@{1}") || !strings.Contains(v, "first") {
		t.Fatalf("drop card %+v:\n%s", ref, v)
	}
	if got := run("stash", "list"); !strings.Contains(got, "first") {
		t.Fatal("dropped before the laptop said so")
	}
	webTestLaptopConfirmRemote(t, &m)
	if got := run("stash", "list"); strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("after drop: %q", got)
	}
	webTestGitLoad(&m)
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_act", `{"ref":"stash@{0}","do":"branch","branch":"from-stash"}`), &ref)
	if v := m.confirm.View(); m.confirm.SafeID() != "cancel" || !strings.Contains(v, "from-stash") {
		t.Fatalf("branch card:\n%s", v)
	}
	webTestLaptopConfirmRemote(t, &m)
	if b := strings.TrimSpace(run("branch", "--show-current")); b != "from-stash" {
		t.Errorf("branch %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "second stash\n" {
		t.Errorf("a.txt %q", b)
	}
	if got := strings.TrimSpace(run("stash", "list")); got != "" {
		t.Errorf("stash after branch: %q", got)
	}
}

// Spec (GITX-10 web): a pop names the entry on the card, then applies it
// and drops it on the laptop.
func TestGitStashPanelPopFromWeb(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "popped\n")
	run("stash", "push", "-q", "-m", "to pop")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.stash_act", `{"ref":"stash@{0}","do":"pop"}`), &ref)
	if v := m.confirm.View(); m.confirm.SafeID() != "cancel" || !strings.Contains(v, "to pop") {
		t.Fatalf("pop card:\n%s", v)
	}
	webTestLaptopConfirmRemote(t, &m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "popped\n" {
		t.Errorf("a.txt %q", b)
	}
	if got := strings.TrimSpace(run("stash", "list")); got != "" {
		t.Errorf("stash after pop: %q", got)
	}
}
