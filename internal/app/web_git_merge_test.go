package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestConflictRepo stops a merge on a conflict in a.txt: main changed
// line one to "ours", the branch "side" to "theirs".
func webTestConflictRepo(t *testing.T, m *Model) (dir string, run func(args ...string) string) {
	t.Helper()
	dir, run = webTestRepo(t, m)
	run("checkout", "-q", "-b", "side")
	webTestWrite(t, dir, "a.txt", "theirs\ntwo\n")
	run("commit", "-q", "-am", "side change")
	run("checkout", "-q", "main")
	webTestWrite(t, dir, "a.txt", "ours\ntwo\n")
	run("commit", "-q", "-am", "main change")
	cmd := exec.Command("git", "merge", "-q", "side")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Fatal("the merge did not stop on a conflict")
	}
	webTestGitLoad(m)
	return dir, run
}

// Spec (merge editor): git.resolve_side keeps one side of the whole file
// only after a laptop card that names the file and the side; then the
// laptop's own merge code checks it out and stages it.
func TestGitResolveSideNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestConflictRepo(t, &m)
	c := webTestSub(t, &m)

	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitResolveSide, `{"path":"b.txt","side":"ours"}`)); got != string(webproto.CodeNotFound) {
		t.Errorf("a file with no conflict: %q", got)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitResolveSide, `{"path":"a.txt","side":"theirs"}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "a.txt") || !strings.Contains(v, "incoming") {
		t.Errorf("card:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); !strings.Contains(string(b), "<<<<<<<") {
		t.Fatal("resolved before the laptop said so")
	}
	m.confirmOpen = false
	webDrain(t, &m, m.handleConfirmSelect("confirm"))
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "theirs\ntwo\n" {
		t.Errorf("file after keep theirs: %q", b)
	}
	if st := run("status", "--porcelain"); strings.Contains(st, "UU") {
		t.Errorf("still conflicted: %q", st)
	}
}
