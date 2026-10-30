package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
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

// Spec (merge editor): git.conflict gives the saved file with its
// markers, its version, the three sides from git's index, and each
// conflict's exact text with the text each choice puts in its place.
func TestGitConflictRead(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, _ := webTestConflictRepo(t, &m)
	c := webTestSub(t, &m)

	var g webproto.GitConflict
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitConflict, `{"path":"a.txt"}`), &g)
	saved, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if g.Path != "a.txt" || g.Text != string(saved) || g.Version != webSHA256(saved) {
		t.Errorf("text %q version %q", g.Text, g.Version)
	}
	if g.Ours.Text != "ours\ntwo\n" || g.Theirs.Text != "theirs\ntwo\n" || g.Base.Text != "one\ntwo\n" || !g.Ours.Present || !g.Base.Present {
		t.Errorf("sides %+v %+v %+v", g.Ours, g.Theirs, g.Base)
	}
	if len(g.Blocks) != 1 {
		t.Fatalf("blocks %+v", g.Blocks)
	}
	b := g.Blocks[0]
	if b.Line != 1 || b.Raw != "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> side\n" || b.OursLabel != "HEAD" || b.TheirsLabel != "side" ||
		b.Ours != "ours\n" || b.Theirs != "theirs\n" || b.Both != "ours\ntheirs\n" || !strings.Contains(g.Text, b.Raw) {
		t.Errorf("block %+v", b)
	}
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitConflict, `{"path":"b.txt"}`)); got != string(webproto.CodeNotFound) {
		t.Errorf("no conflict: %q", got)
	}
}

// Spec (merge editor): git.resolve saves the merged text and stages the
// file, only if the saved file is still the version the page read, no
// conflict markers are left and the laptop has no unsaved edits to it.
// The file's CRLF line ends come back.
func TestGitResolveSavesAndStages(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestConflictRepo(t, &m)
	c := webTestSub(t, &m)
	saved, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	v := webSHA256(saved)
	call := func(text, version string) map[string]any {
		return webTestCallBg(t, &m, c, webproto.OpGitResolve, `{"path":"a.txt","text":`+webJSONString(text)+`,"version":"`+version+`"}`)
	}
	if got := webTestErr(call("merged\ntwo\n", strings.Repeat("0", 64))); got != string(webproto.CodeConflict) {
		t.Errorf("an old version: %q", got)
	}
	if got := webTestErr(call(string(saved), v)); got != string(webproto.CodeConflict) {
		t.Errorf("markers left: %q", got)
	}
	m.bufs = append(m.bufs, nvimBufferInfo(filepath.Join(dir, "a.txt"), true))
	if got := webTestErr(call("merged\ntwo\n", v)); got != string(webproto.CodeConflict) {
		t.Errorf("unsaved on the laptop: %q", got)
	}
	m.bufs = nil
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != string(saved) {
		t.Fatal("written although refused")
	}
	var r webproto.GitResolved
	webTestData(t, call("merged\ntwo\n", v), &r)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "merged\ntwo\n" {
		t.Errorf("saved %q", b)
	}
	if r.Path != "a.txt" || r.Left != 0 {
		t.Errorf("result %+v", r)
	}
	if st := run("status", "--porcelain"); !strings.Contains(st, "M  a.txt") {
		t.Errorf("not staged: %q", st)
	}
}

// Spec: a CRLF file keeps its line ends when the page's merged text
// (always "\n") is saved.
func TestGitResolveKeepsCRLF(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, _ := webTestConflictRepo(t, &m)
	crlf := []byte("<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> side\r\ntwo\r\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), crlf, 0o644); err != nil {
		t.Fatal(err)
	}
	c := webTestSub(t, &m)
	var g webproto.GitConflict
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitConflict, `{"path":"a.txt"}`), &g)
	if strings.Contains(g.Text, "\r") || len(g.Blocks) != 1 || strings.Contains(g.Blocks[0].Raw, "\r") {
		t.Fatalf("the page gets \\n line ends: %q %+v", g.Text, g.Blocks)
	}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitResolve, `{"path":"a.txt","text":"ours\ntwo\n","version":"`+g.Version+`"}`), &webproto.GitResolved{})
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "ours\r\ntwo\r\n" {
		t.Errorf("saved %q", b)
	}
}

func webJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func nvimBufferInfo(path string, modified bool) nvim.BufferInfo {
	return nvim.BufferInfo{ID: 9, Path: path, Modified: modified}
}
