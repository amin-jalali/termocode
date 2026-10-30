package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-41): tree.new_file makes an empty file and tree.new_dir a
// folder, inside a folder that resolves inside the workspace; an existing
// name is never replaced (conflict), a missing folder is not_found, a
// folder through a link that leads out is forbidden; each call leaves an
// audit line; a viewer may not.
func TestTreeNewFileAndFolder(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.write("src/a.go", "package a\n")
	if f := r.call(webproto.OpTreeNewFile, &webproto.ItemArgs{Path: "src/new.go"}); f["t"] != "res" {
		t.Fatalf("new file: %v", f)
	}
	if b, err := os.ReadFile(filepath.Join(r.root, "src", "new.go")); err != nil || len(b) != 0 {
		t.Fatalf("new file %q %v", b, err)
	}
	if f := r.call(webproto.OpTreeNewDir, &webproto.ItemArgs{Path: "src/pkg"}); f["t"] != "res" {
		t.Fatalf("new folder: %v", f)
	}
	if fi, err := os.Stat(filepath.Join(r.root, "src", "pkg")); err != nil || !fi.IsDir() {
		t.Fatalf("new folder %v", err)
	}
	if f := r.call(webproto.OpTreeNewFile, &webproto.ItemArgs{Path: "src/a.go"}); webErrCode(f) != "conflict" {
		t.Errorf("over an existing file: %v", f)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "src", "a.go")); string(b) != "package a\n" {
		t.Fatalf("an existing file changed: %q", b)
	}
	if f := r.call(webproto.OpTreeNewDir, &webproto.ItemArgs{Path: "src/pkg"}); webErrCode(f) != "conflict" {
		t.Errorf("over an existing folder: %v", f)
	}
	if f := r.call(webproto.OpTreeNewFile, &webproto.ItemArgs{Path: "nope/x.go"}); webErrCode(f) != "not_found" {
		t.Errorf("in a missing folder: %v", f)
	}
	outside := filepath.Join(filepath.Dir(r.root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.root, "out")); err != nil {
		t.Fatal(err)
	}
	if f := r.call(webproto.OpTreeNewFile, &webproto.ItemArgs{Path: "out/x.go"}); webErrCode(f) != "forbidden" {
		t.Errorf("through a link out: %v", f)
	}
	if _, err := os.Stat(filepath.Join(outside, "x.go")); err == nil {
		t.Fatal("a file was made outside the workspace")
	}
	v := r.conn("c-v", webproto.RoleViewer)
	if webproto.Allowed(v.role, webproto.OpTreeNewFile) {
		t.Error("a viewer may make files")
	}
	a := webTestAudit(r.m)
	if !strings.Contains(a, "tree.new_file  src/new.go  by=web:") || !strings.Contains(a, "tree.new_dir  src/pkg  by=web:") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEB-41): tree.rename gives a file a new name in its folder (off
// the UI thread); an existing name is never replaced; the workspace
// folder cannot be renamed; the laptop gets the explorer's Undo toast,
// which puts the old name back; the audit line names both.
func TestTreeRename(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.write("src/a.go", "package a\n")
	r.write("src/b.go", "package b\n")
	if f := r.call(webproto.OpTreeRename, &webproto.RenameArgs{Path: "src/a.go", Name: "b.go"}); webErrCode(f) != "conflict" {
		t.Fatalf("over b.go: %v", f)
	}
	f := r.call(webproto.OpTreeRename, &webproto.RenameArgs{Path: "src/a.go", Name: "c.go"})
	var ch webproto.TreeChanged
	webTestData(t, f, &ch)
	if f["t"] != "res" || ch.Path != "src/c.go" {
		t.Fatalf("rename: %v", f)
	}
	if _, err := os.Stat(filepath.Join(r.root, "src", "c.go")); err != nil {
		t.Fatal("c.go missing")
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "src", "b.go")); string(b) != "package b\n" {
		t.Fatalf("b.go changed: %q", b)
	}
	if !strings.Contains(webTestToasts(*r.m), "Renamed on web · Undo") {
		t.Errorf("toasts: %s", webTestToasts(*r.m))
	}
	if a := webTestAudit(r.m); !strings.Contains(a, "tree.rename  src/a.go → c.go  by=web:") {
		t.Errorf("audit:\n%s", a)
	}
	webDrain(t, r.m, r.m.undoBulkRename(r.m.bulkRename.serial))
	if _, err := os.Stat(filepath.Join(r.root, "src", "a.go")); err != nil {
		t.Fatal("the laptop's undo did not bring a.go back")
	}
	r.write("dir/x.txt", "x")
	if f := r.call(webproto.OpTreeRename, &webproto.RenameArgs{Path: "dir", Name: "dir2"}); f["t"] != "res" {
		t.Fatalf("folder rename: %v", f)
	}
	if _, err := os.Stat(filepath.Join(r.root, "dir2", "x.txt")); err != nil {
		t.Fatal("folder not renamed")
	}
}
