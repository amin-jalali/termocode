package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

func webTreeList(t *testing.T, r *webFilesRig, rel string) webproto.TreeListRes {
	t.Helper()
	var res webproto.TreeListRes
	webResData(t, r.call(webproto.OpTreeList, &webproto.PathArgs{Path: rel}), &res)
	return res
}

func webTreeNames(es []webproto.TreeEntry) string {
	var out []string
	for _, e := range es {
		s := e.Name
		if e.Dir {
			s += "/"
		}
		if e.Git != "" {
			s += "[" + e.Git + "]"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// Spec (WEB-16, T40): tree.list lists folders inside the root only:
// "..", an absolute path and a symlink that points out are refused; a
// symlink to a folder inside is fine.
func TestTreeListInsideRootOnly(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("in/a.txt", "a")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(r.root, "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.root, "in"), filepath.Join(r.root, "inlink")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"..", "../x", "in/../../x", "/etc", outside, "out", "out/sub"} {
		f := r.call(webproto.OpTreeList, &webproto.PathArgs{Path: p})
		if code := webErrCode(f); code != "forbidden" && code != "not_found" {
			t.Errorf("tree.list %q: %v", p, f)
		}
		if strings.Contains(fmt.Sprint(f), outside) {
			t.Errorf("tree.list %q leaks the outside path: %v", p, f)
		}
	}
	if got := webTreeNames(webTreeList(t, r, "inlink").Entries); got != "a.txt" {
		t.Errorf("inlink: %q", got)
	}
	if got := webTreeNames(webTreeList(t, r, "").Entries); got != "in/ inlink out" {
		t.Errorf("root: %q", got)
	}
}

// Spec (WEB-16): a folder lists at most 2000 entries, folders first;
// more sets truncated.
func TestTreeTruncated2000(t *testing.T) {
	r := newWebFilesRig(t)
	for i := 0; i < webproto.MaxTreeEntries+5; i++ {
		if err := os.WriteFile(filepath.Join(r.root, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Mkdir(filepath.Join(r.root, "zdir"), 0o755)
	res := webTreeList(t, r, "")
	if len(res.Entries) != webproto.MaxTreeEntries || !res.Truncated {
		t.Fatalf("%d entries, truncated %v", len(res.Entries), res.Truncated)
	}
	if !res.Entries[0].Dir || res.Entries[0].Name != "zdir" {
		t.Errorf("folders first: %+v", res.Entries[0])
	}
}

// Spec (WEB-16): files carry the explorer's git letter (untracked "?"),
// a folder with a change inside is "M", build artefacts are ignored.
func TestTreeGitBadges(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("a.go", "")
	r.write("b.go", "")
	r.write("new.txt", "")
	r.write("sub/x.go", "")
	r.write("node_modules/y.js", "")
	r.write("fresh/z", "")
	r.m.gitIsRepo = true
	r.m.gitFiles = []git.FileStatus{{Code: "M ", Path: "a.go"}, {Code: "??", Path: "new.txt"},
		{Code: " M", Path: "sub/x.go"}, {Code: "??", Path: "fresh/"}}
	res := webTreeList(t, r, "")
	if got := webTreeNames(res.Entries); got != "fresh/[?] node_modules/ sub/[M] a.go[M] b.go new.txt[?]" {
		t.Errorf("badges: %q", got)
	}
	for _, e := range res.Entries {
		if (e.Name == "node_modules") != e.Ignored {
			t.Errorf("%s ignored=%v", e.Name, e.Ignored)
		}
	}
	if got := webTreeNames(webTreeList(t, r, "sub").Entries); got != "x.go[M]" {
		t.Errorf("sub: %q", got)
	}
}

// Spec (WEB-16): dotfiles follow the laptop's toggle; flipping it sends
// a hidden delta and the listed folders again.
func TestTreeHiddenFollowsLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	r.write(".env", "X=1")
	r.write("main.go", "")
	if snap := r.sub("tree"); snap["hidden"] != false {
		t.Fatalf("snap: %v", snap)
	}
	if got := webTreeNames(webTreeList(t, r, "").Entries); got != "main.go" {
		t.Fatalf("hidden off: %q", got)
	}
	r.deltas("tree")
	r.m.explorer.ToggleHidden()
	r.track()
	ds := r.deltas("tree")
	if len(ds) != 2 || ds[0]["kind"] != "hidden" || ds[0]["on"] != true || ds[1]["kind"] != "dir" {
		t.Fatalf("deltas: %v", ds)
	}
	es := ds[1]["entries"].([]any)
	if len(es) != 2 || es[0].(map[string]any)["name"] != ".env" || es[0].(map[string]any)["hidden"] != true {
		t.Errorf("entries: %v", es)
	}
}

// Spec (WEB-16): a git poll lists the watched folders again and sends
// the ones that changed; unlist sends remove.
func TestTreeRefreshAndUnlist(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("src/a.go", "")
	r.sub("tree")
	webTreeList(t, r, "")
	webTreeList(t, r, "src")
	r.deltas("tree")
	webDrain(t, r.m, r.m.webTreeRefresh())
	if ds := r.deltas("tree"); len(ds) != 0 {
		t.Fatalf("nothing changed, yet %v", ds)
	}
	r.write("src/b.go", "")
	webDrain(t, r.m, r.m.webTreeRefresh())
	ds := r.deltas("tree")
	if len(ds) != 1 || ds[0]["path"] != "src" {
		t.Fatalf("deltas: %v", ds)
	}
	if f := r.call(webproto.OpTreeUnlist, &webproto.PathArgs{Path: "src"}); f["t"] != "res" {
		t.Fatal(f)
	}
	if ds := r.deltas("tree"); len(ds) != 1 || ds[0]["kind"] != "remove" || ds[0]["path"] != "src" {
		t.Fatalf("unlist: %v", ds)
	}
	if err := os.RemoveAll(filepath.Join(r.root, "src")); err != nil {
		t.Fatal(err)
	}
	webTreeList(t, r, "")
	r.deltas("tree")
}

// Spec (WEB-16, T48): one connection lists at most 64 folders.
func TestTreeDirCap(t *testing.T) {
	r := newWebFilesRig(t)
	for i := 0; i <= webproto.MaxTreeDirs; i++ {
		_ = os.MkdirAll(filepath.Join(r.root, fmt.Sprintf("d%02d", i)), 0o755)
	}
	for i := 0; i < webproto.MaxTreeDirs; i++ {
		webTreeList(t, r, fmt.Sprintf("d%02d", i))
	}
	if f := r.call(webproto.OpTreeList, &webproto.PathArgs{Path: fmt.Sprintf("d%02d", webproto.MaxTreeDirs)}); webErrCode(f) != "limit" {
		t.Fatalf("65th folder: %v", f)
	}
	webTreeList(t, r, "d00") // listed already: fine
}

// webMapHost is a container host over an in-memory FS (ADR 0018).
type webMapHost struct{ fs webMapFS }

func (h webMapHost) FS() workspace.FS                 { return h.fs }
func (webMapHost) Exec() services.Exec                { return nil }
func (webMapHost) TermArgv(string, []string) []string { return nil }
func (webMapHost) Caps() workspace.Caps               { return workspace.Caps{} }
func (webMapHost) Online() bool                       { return true }
func (webMapHost) Label() string                      { return "api-1" }
func (f webMapFS) WriteFile(context.Context, string, []byte, fs.FileMode) error {
	return fs.ErrPermission
}
func (f webMapFS) Mkdir(context.Context, string, fs.FileMode) error { return fs.ErrPermission }
func (f webMapFS) Rename(context.Context, string, string) error     { return fs.ErrPermission }
func (f webMapFS) Remove(context.Context, string) error             { return fs.ErrPermission }

type webMapFS struct{ m fstest.MapFS }

func (f webMapFS) name(p string) string { return strings.TrimPrefix(path.Clean(p), "/") }
func (f webMapFS) Stat(_ context.Context, p string) (fs.FileInfo, error) {
	return fs.Stat(f.m, f.name(p))
}
func (f webMapFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	return fs.ReadDir(f.m, f.name(p))
}
func (f webMapFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	return fs.ReadFile(f.m, f.name(p))
}

// Spec (WEB-16): a container workspace lists through its Host FS.
func TestTreeContainerFS(t *testing.T) {
	r := newWebFilesRig(t)
	host := webMapHost{webMapFS{fstest.MapFS{
		"app/main.go":    {Data: []byte("package main")},
		"app/cmd/x.go":   {Data: []byte("")},
		"app/.dockerenv": {Data: []byte("")},
	}}}
	r.m.ws = workspace.Workspace{ID: "container:dev/api-1:/app", Kind: workspace.KindContainer, Root: "/app", Host: host}
	if got := webTreeNames(webTreeList(t, r, "").Entries); got != "cmd/ main.go" {
		t.Errorf("root: %q", got)
	}
	if got := webTreeNames(webTreeList(t, r, "cmd").Entries); got != "x.go" {
		t.Errorf("cmd: %q", got)
	}
	if f := r.call(webproto.OpTreeList, &webproto.PathArgs{Path: "../etc"}); webErrCode(f) != "forbidden" {
		t.Errorf("../etc: %v", f)
	}
}

// Spec (WEB-16, WEB-7): the tree replays: list, a change seen by the
// git poll, a badge, unlist.
func TestTreeReplayGolden(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("README.md", "hi")
	r.write("src/a.go", "package a")
	c := webReplayRecord(t, r.m, r.h, "tree", func() {
		webTreeList(t, r, "")
		webTreeList(t, r, "src")
		r.write("src/b.go", "package a")
		r.m.gitIsRepo = true
		r.m.gitFiles = []git.FileStatus{{Code: "??", Path: "src/b.go"}}
		webDrain(t, r.m, r.m.webTreeRefresh())
		r.call(webproto.OpTreeUnlist, &webproto.PathArgs{Path: "src"})
	})
	if len(c.Deltas) != 5 { // root, src, root again (src has a change), src, remove
		t.Fatalf("%d deltas: %s", len(c.Deltas), c.Deltas)
	}
	webReplayCheck(t, webTopics["tree"], c)
	webReplayGolden(t, webReplayPath("tree", "list_refresh_unlist"), c)
}
