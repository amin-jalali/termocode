package containerhost

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestWorkspaceFSOps(t *testing.T) {
	h, _, _ := newHost(t)
	ws := h.Workspace()
	if ws.Kind != workspace.KindContainer || ws.Root != "/app" || ws.ID != "container:default/api-1:/app" || ws.Base() != "app" {
		t.Fatalf("ws = %+v", ws)
	}
	f, ctx := ws.Host.FS(), t.Context()
	if err := f.WriteFile(ctx, "a/b/c.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := f.ReadFile(ctx, "/app/a/b/c.txt"); err != nil || string(data) != "hi" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if fi, err := f.Stat(ctx, "a/b/c.txt"); err != nil || fi.Size() != 2 {
		t.Fatalf("Stat = %v, %v", fi, err)
	}
	if err := f.Mkdir(ctx, "d/e", 0o755); err != nil {
		t.Fatal(err)
	}
	if ents, err := f.ReadDir(ctx, "."); err != nil || names(ents) != "a/|d/" {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	if err := f.Rename(ctx, "a/b/c.txt", "d/e/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if err := f.Remove(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	if ents, _ := f.ReadDir(ctx, "."); names(ents) != "a/" {
		t.Fatalf("after remove = %v", names(ents))
	}
}
