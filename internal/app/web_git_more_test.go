package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestLaptopConfirmRemote confirms the open laptop card and runs the
// laptop's git command it starts until its result is shown and git's
// state is read again.
func webTestLaptopConfirmRemote(t *testing.T, m *Model) {
	t.Helper()
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("confirm")
	m.webAfterUpdate(nil)
	got := make(chan tea.Msg, 16)
	n := 0
	start := func(c tea.Cmd) {
		if c != nil {
			n++
			go func() { got <- c() }()
		}
	}
	start(cmd)
	done := false
	deadline := time.After(20 * time.Second)
	for n > 0 {
		select {
		case msg := <-got:
			n--
			switch msg := msg.(type) {
			case gitRemoteDoneMsg:
				done = true
				start(m.gitRemoteDone(msg))
			case GitMsg:
				// fetchGitCmd reads the process's folder; the test reads the
				// workspace's git state itself below.
				if done {
					webTestGitLoad(m)
					return
				}
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
			}
		case <-deadline:
			t.Fatal("the laptop's git command did not finish")
		}
	}
	if !done {
		t.Fatal("no git command ran")
	}
}

// webTestRemoteRepo is webTestRepo with a bare remote that main tracks.
func webTestRemoteRepo(t *testing.T, m *Model) (dir, remote string, run func(args ...string) string) {
	t.Helper()
	dir, run = webTestRepo(t, m)
	remote = filepath.Join(t.TempDir(), "remote.git")
	run("init", "-q", "--bare", remote)
	run("remote", "add", "origin", remote)
	run("push", "-q", "-u", "origin", "main")
	webTestGitLoad(m)
	return dir, remote, run
}

// Spec (WEB-42): git.push waits for a laptop card that names the branch
// and the commits; nothing is pushed before the laptop says yes; then
// the laptop's own Git: Push runs (its toasts, its credentials).
func TestGitPushNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, remote, run := webTestRemoteRepo(t, &m)
	webTestWrite(t, dir, "b.txt", "b\n")
	run("add", "b.txt")
	run("commit", "-q", "-m", "second")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitPush, `{}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q open=%v", ref.Card, m.confirmOpen)
	}
	if v := m.confirm.View(); !strings.Contains(v, "1 commit on main") {
		t.Errorf("card:\n%s", v)
	}
	remoteHead := func() string {
		return strings.TrimSpace(run("--git-dir", remote, "rev-parse", "main"))
	}
	local := strings.TrimSpace(run("rev-parse", "main"))
	if remoteHead() == local {
		t.Fatal("pushed before the laptop said so")
	}
	webTestLaptopConfirmRemote(t, &m)
	if remoteHead() != local {
		t.Fatal("not pushed after the laptop's yes")
	}
	if !strings.Contains(webTestToasts(m), "Pushed to origin") {
		t.Errorf("toasts: %s", webTestToasts(m))
	}
	if a := webTestAudit(&m); !strings.Contains(a, "git.push  main  by=web:phone  → confirmed on the laptop") {
		t.Errorf("audit:\n%s", a)
	}

	// Pull: confirmed, the remote's new commit arrives.
	other := filepath.Join(t.TempDir(), "other")
	run("clone", "-q", "-b", "main", remote, other)
	webTestWrite(t, other, "c.txt", "c\n")
	for _, args := range [][]string{{"-C", other, "-c", "user.email=t@e.x", "-c", "user.name=T", "-c", "commit.gpgsign=false", "add", "c.txt"},
		{"-C", other, "-c", "user.email=t@e.x", "-c", "user.name=T", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "third"},
		{"-C", other, "push", "-q"}} {
		run(args...)
	}
	pf := webTestCallBg(t, &m, c, webproto.OpGitPull, `{}`)
	webTestData(t, pf, &ref)
	if v := m.confirm.View(); !strings.Contains(v, "Pull into main?") {
		t.Fatalf("pull card %v kind=%v:\n%s", pf, m.confirmKind, v)
	}
	webTestLaptopConfirmRemote(t, &m)
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err != nil {
		t.Fatalf("not pulled after the laptop's yes: %s", webTestToasts(m))
	}
}

// Spec (WEB-42): git.branches lists the local branches; git.checkout
// takes only a listed one and waits for the laptop; the card says the
// changed files go along. A detached HEAD cannot push from the web.
func TestGitCheckoutNeedsListAndCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	run("branch", "feat/web")
	webTestWrite(t, dir, "a.txt", "changed\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, webproto.OpGitCheckout, `{"branch":"feat/web"}`); webTestErr(f) != "not_found" {
		t.Fatalf("checkout before the list: %v", f)
	}
	var br webproto.GitBranches
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitBranches, `{}`), &br)
	if br.Current != "main" || strings.Join(br.Branches, ",") != "feat/web,main" || br.Stashes != 0 {
		t.Fatalf("branches %+v", br)
	}
	if f := webTestCallBg(t, &m, c, webproto.OpGitCheckout, `{"branch":"nope"}`); webTestErr(f) != "not_found" {
		t.Fatalf("an unknown branch: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitCheckout, `{"branch":"feat/web"}`), &ref)
	if v := m.confirm.View(); !strings.Contains(v, "Switch to feat/web?") || !strings.Contains(v, "1 changed file go") {
		t.Errorf("card:\n%s", v)
	}
	webTestLaptopConfirmRemote(t, &m)
	if got := strings.TrimSpace(run("rev-parse", "--abbrev-ref", "HEAD")); got != "feat/web" {
		t.Fatalf("on %q after the laptop's yes", got)
	}

	run("checkout", "-q", "--detach")
	webTestGitLoad(&m)
	if f := webTestCallBg(t, &m, c, webproto.OpGitPush, `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("push on a detached HEAD: %v", f)
	}
}

// Spec (WEB-42): git.stash waits for the laptop and refuses a clean tree;
// git.stash_pop brings the changes back.
func TestGitStashAndPop(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, _ := webTestRepo(t, &m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, webproto.OpGitStash, `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("stash of a clean tree: %v", f)
	}
	webTestWrite(t, dir, "a.txt", "changed\n")
	webTestGitLoad(&m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitStash, `{"message":"from phone"}`), &ref)
	webTestLaptopConfirmRemote(t, &m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\ntwo\n" {
		t.Fatalf("not stashed: %q", b)
	}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitStashPop, `{}`), &ref)
	webTestLaptopConfirmRemote(t, &m)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "changed\n" {
		t.Fatalf("not popped: %q", b)
	}
}
