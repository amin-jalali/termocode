package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestIndex is a file as the index holds it.
func webTestIndex(run func(args ...string) string, name string) string {
	return run("show", ":"+name)
}

// Spec (Git: Stage Line / Unstage Line from the web): git.stage_lines
// stages the changes on rows from..to of the saved working file and
// nothing else; git.unstage_lines takes rows from..to of the index's
// text (as the staged diff shows them) out again. Only a file the Git
// tab lists; no laptop card (both are undone with the other one).
func TestGitStageLinesFromWeb(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "ONE\nTWO\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, "git.stage_lines", `{"path":"b.txt","from":1,"to":1}`); webTestErr(f) != "not_found" {
		t.Fatalf("a file with no changes: %v", f)
	}
	if f := webTestCallBg(t, &m, c, "git.stage_lines", `{"path":"a.txt","from":2,"to":2}`); webTestErr(f) != "" {
		t.Fatalf("stage lines: %v", f)
	}
	if got := webTestIndex(run, "a.txt"); got != "one\nTWO\n" {
		t.Fatalf("index after staging row 2: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "ONE\nTWO\n" {
		t.Fatalf("the working file changed: %q", b)
	}
	if f := webTestCallBg(t, &m, c, "git.unstage_lines", `{"path":"a.txt","from":2,"to":2}`); webTestErr(f) != "" {
		t.Fatalf("unstage lines: %v", f)
	}
	if got := webTestIndex(run, "a.txt"); got != "one\ntwo\n" {
		t.Fatalf("index after unstaging row 2: %q", got)
	}
	if f := webTestCallBg(t, &m, c, "git.unstage_lines", `{"path":"a.txt","from":1,"to":1}`); webTestErr(f) != "conflict" {
		t.Fatalf("unstage with nothing staged: %v", f)
	}
	if a := webTestAudit(&m); !strings.Contains(a, "git.stage_lines  a.txt 2-2") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (Git: Discard Line from the web): git.discard_lines waits for a
// laptop card (Cancel first) that names the file and the rows; nothing
// changes before the laptop says yes; then only those rows go back.
func TestGitDiscardLinesNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, _ := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "ONE\nTWO\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.discard_lines", `{"path":"a.txt","from":1,"to":1}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "line 1 of a.txt") {
		t.Errorf("card:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "ONE\nTWO\n" {
		t.Fatalf("discarded before the laptop said so: %q", b)
	}
	webTestLaptopSelect(t, &m, "confirm")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\nTWO\n" {
		t.Fatalf("after the laptop's yes: %q", b)
	}
}

// Spec (Git: Discard Hunk from the web): git.discard_hunk names the
// change by its exact hunk header and waits for a laptop card; then
// only that change goes back. A header the diff no longer has changes
// nothing and the laptop says so.
func TestGitDiscardHunkNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	var lines []string
	for i := 0; i < 14; i++ {
		lines = append(lines, "row"+string(rune('a'+i)))
	}
	webTestWrite(t, dir, "a.txt", strings.Join(lines, "\n")+"\n")
	run("commit", "-q", "-am", "long")
	ch := append([]string(nil), lines...)
	ch[1], ch[12] = "TWO", "THIRTEEN"
	webTestWrite(t, dir, "a.txt", strings.Join(ch, "\n")+"\n")
	webTestGitLoad(&m)
	var heads []string
	for _, l := range strings.Split(run("diff", "--no-color", "--", "a.txt"), "\n") {
		if strings.HasPrefix(l, "@@ ") {
			heads = append(heads, l)
		}
	}
	if len(heads) != 2 {
		t.Fatalf("hunks %q", heads)
	}
	c := webTestSub(t, &m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.discard_hunk", `{"path":"a.txt","hunk":"`+heads[1]+`"}`), &ref)
	if ref.Card == "" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "a.txt") {
		t.Errorf("card:\n%s", v)
	}
	webTestLaptopSelect(t, &m, "confirm")
	want := append([]string(nil), ch...)
	want[12] = lines[12]
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != strings.Join(want, "\n")+"\n" {
		t.Fatalf("after the laptop's yes: %q", b)
	}
	// The same header again: that change is gone, nothing changes.
	webTestGitLoad(&m)
	webTestData(t, webTestCallBg(t, &m, c, "git.discard_hunk", `{"path":"a.txt","hunk":"`+heads[1]+`"}`), &ref)
	webTestLaptopSelect(t, &m, "confirm")
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != strings.Join(want, "\n")+"\n" {
		t.Fatalf("a gone change discarded something: %q", b)
	}
	if !strings.Contains(webTestToasts(m), "moved") {
		t.Errorf("toasts: %s", webTestToasts(m))
	}
}
