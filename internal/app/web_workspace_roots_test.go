//go:build unix

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/update"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// WEBP-8: a folder added to the workspace (on the laptop or from the
// web, after the laptop card) shows on the web as its own root, as the
// laptop's explorer shows it. The page names its files ":<folder>/…";
// the absolute path never goes out, and the same checks as the
// workspace's own folder hold (no "..", no symlink out).

// webExtraRoot makes a folder named name outside the workspace, adds it
// to the explorer and returns its path.
func webExtraRoot(t *testing.T, r *webFilesRig, name string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"x.go": "package x\n", "sub/y.go": "package y\n"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.m.explorer.AddRoot(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWebTreeShowsAddedFolders(t *testing.T) {
	r, _ := webWorkspaceRig(t)
	r.write("a.txt", "a")
	lib := webExtraRoot(t, r, "lib")

	snap, err := webTreeSnapshot(r.m, "")
	if err != nil {
		t.Fatal(err)
	}
	roots := snap.(webproto.TreeSnap).Roots
	want := []webproto.TreeRoot{{Path: "", Label: "ws-root"}, {Path: ":lib", Label: "lib"}}
	if len(roots) != 2 || roots[0] != want[0] || roots[1] != want[1] {
		t.Fatalf("roots %+v, want %+v", roots, want)
	}
	if b, _ := json.Marshal(snap); strings.Contains(string(b), lib) {
		t.Errorf("the folder's path went out: %s", b)
	}
	if got := webTreeNames(webTreeList(t, r, ":lib").Entries); got != "sub/ x.go" {
		t.Errorf(":lib lists %q", got)
	}
	if got := webTreeNames(webTreeList(t, r, ":lib/sub").Entries); got != "y.go" {
		t.Errorf(":lib/sub lists %q", got)
	}
	if abs, err := r.m.webResolve(":lib/sub/y.go"); err != nil || abs != filepath.Join(lib, "sub", "y.go") {
		t.Errorf("resolve: %q %v", abs, err)
	}
	if rel, ok := r.m.webRel(filepath.Join(lib, "x.go")); !ok || rel != ":lib/x.go" {
		t.Errorf("rel of the added folder's file: %q %v", rel, ok)
	}
	if rel, ok := r.m.webRel(filepath.Join(r.root, "a.txt")); !ok || rel != "a.txt" {
		t.Errorf("rel of the workspace's file: %q %v", rel, ok)
	}

	var files webproto.FilesListRes
	webResData(t, r.call(webproto.OpFilesList, &webproto.EmptyArgs{}), &files)
	if got := strings.Join(files.Paths, " "); got != ":lib/sub/y.go :lib/x.go a.txt" {
		t.Errorf("files.list %q", got)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(lib, "out")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{":lib/out", ":lib/../x", ":nope", ":nope/x.go"} {
		f := r.call(webproto.OpTreeList, &webproto.PathArgs{Path: p})
		if code := webErrCode(f); code != "forbidden" && code != "not_found" {
			t.Errorf("tree.list %q: %v", p, f)
		}
	}

}

// Two added folders with one name get their own names; removing one
// tells the open pages (a roots delta), and its folders leave the tree.
func TestWebTreeRootsFollowTheLaptop(t *testing.T) {
	r, _ := webWorkspaceRig(t)
	r.sub("tree")
	first := webExtraRoot(t, r, "lib")
	webExtraRoot(t, r, "lib")
	r.track()
	ds := r.deltas("tree")
	if len(ds) != 1 || ds[0]["kind"] != "roots" {
		t.Fatalf("deltas after the add %v", ds)
	}
	b, _ := json.Marshal(ds[0]["roots"])
	if string(b) != `[{"label":"ws-root","path":""},{"label":"lib","path":":lib"},{"label":"lib","path":":lib~2"}]` {
		t.Errorf("roots %s", b)
	}
	if got := webTreeNames(webTreeList(t, r, ":lib~2").Entries); got != "sub/ x.go" {
		t.Errorf(":lib~2 lists %q", got)
	}
	r.deltas("tree")
	r.track()
	if ds := r.deltas("tree"); len(ds) != 0 {
		t.Errorf("no change, yet %v", ds)
	}
	if !r.m.explorer.RemoveRoot(first) {
		t.Fatal("remove")
	}
	r.track()
	ds = r.deltas("tree")
	var kinds []string
	for _, d := range ds {
		kinds = append(kinds, d["kind"].(string))
	}
	if strings.Join(kinds, " ") != "remove roots" || ds[0]["path"] != ":lib~2" {
		t.Errorf("deltas after the remove %v", ds)
	}
}

// The update chip: the status topic names a newer release, as the
// laptop's status bar chip does; none once installed or skipped.
func TestStatusNamesANewerRelease(t *testing.T) {
	m := aiTestModel(t)
	old := termocodeVersion
	termocodeVersion = "v0.1.0"
	t.Cleanup(func() { termocodeVersion = old })
	p := &webStatusPub{}
	if s := m.webStatus(p); s.Update != "" {
		t.Errorf("no check yet: %q", s.Update)
	}
	m.updW().state = update.State{Latest: "v0.2.0"}
	if s := m.webStatus(p); s.Update != "v0.2.0" {
		t.Errorf("newer release: %q", s.Update)
	}
	a := m.webStatus(p)
	b := a
	b.Update = ""
	if webStatusSame(a, b) {
		t.Error("the chip is not part of the compare")
	}
	m.upd.installed = "v0.2.0"
	if s := m.webStatus(p); s.Update != "" {
		t.Errorf("installed: %q", s.Update)
	}
	m.upd.installed = ""
	m.upd.state.Skipped = "v0.2.0"
	if s := m.webStatus(p); s.Update != "" {
		t.Errorf("skipped: %q", s.Update)
	}
}
