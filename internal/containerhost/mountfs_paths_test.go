package containerhost

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// On a bind-mounted workspace, a path that leaves the root is refused
// before any disk or engine work, for every file operation.
func TestMountFSRefusesPathsOutsideTheRoot(t *testing.T) {
	h, d, c := mounted(t, true, bindApp)
	f, ctx := h.FS(), t.Context()
	if _, ok := f.(mountFS); !ok {
		t.Fatalf("FS = %T, want the mount FS", f)
	}
	c.WriteFile(t, "/etc/passwd", []byte("root:x:0:0"), 0o644)
	before := engineCalls(d)
	bad := "../etc/passwd"
	nul := "a\x00b"
	checks := map[string]error{}
	_, checks["stat"] = f.Stat(ctx, bad)
	_, checks["readdir"] = f.ReadDir(ctx, bad)
	_, checks["read"] = f.ReadFile(ctx, bad)
	checks["write"] = f.WriteFile(ctx, bad, []byte("x"), 0o644)
	checks["mkdir"] = f.Mkdir(ctx, "/etc/evil", 0o755)
	checks["rename from"] = f.Rename(ctx, bad, "ok.txt")
	checks["rename to"] = f.Rename(ctx, "ok.txt", bad)
	checks["remove"] = f.Remove(ctx, bad)
	checks["nul"] = f.Remove(ctx, nul)
	for op, err := range checks {
		if err == nil {
			t.Errorf("%s outside the root: no error", op)
		}
	}
	if !errors.Is(checks["read"], workspace.ErrOutsideRoot) {
		t.Errorf("read err = %v, want ErrOutsideRoot", checks["read"])
	}
	if n := engineCalls(d) - before; n != 0 {
		t.Fatalf("%d engine calls for refused paths", n)
	}
	if data, _ := os.ReadFile(c.Local("/etc/passwd")); string(data) != "root:x:0:0" {
		t.Fatalf("/etc/passwd changed: %q", data)
	}
}

// A folder in the workspace that links out of the mount (../data) is not
// the local disk's business: every operation under it goes to the engine
// and lands in the container's folder.
func TestMountFSFolderLinkedOutOfTheMountGoesThroughTheEngine(t *testing.T) {
	h, d, c := mounted(t, true, bindApp)
	c.Mkdir(t, "/data")
	c.WriteFile(t, "/data/old.txt", []byte("old"), 0o644)
	if err := os.Symlink("../data", c.Local("/app/data")); err != nil {
		t.Skip("no symlinks:", err)
	}
	f, ctx := h.FS(), t.Context()
	before := engineCalls(d)
	if err := f.WriteFile(ctx, "data/new.txt", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(c.Local("/data/new.txt")); err != nil || string(got) != "new" {
		t.Fatalf("container /data/new.txt = %q, %v", got, err)
	}
	if fi, err := f.Stat(ctx, "data/new.txt"); err != nil || fi.Size() != 3 {
		t.Fatalf("Stat = %v, %v", fi, err)
	}
	ents, err := f.ReadDir(ctx, "data")
	if err != nil || names(ents) != "new.txt|old.txt" {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	if s := fmt.Sprint(ents[0]); s != "- new.txt" {
		t.Fatalf("entry prints as %q", s)
	}
	if err := f.Mkdir(ctx, "data/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(c.Local("/data/sub")); err != nil || !fi.IsDir() {
		t.Fatalf("container /data/sub = %v, %v", fi, err)
	}
	if err := f.Rename(ctx, "data/old.txt", "data/older.txt"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove(ctx, "data/older.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.Local("/data/older.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("container /data/older.txt still there: %v", err)
	}
	if engineCalls(d) == before {
		t.Fatal("no engine call for a container-linked folder")
	}
}
