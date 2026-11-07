package app

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/amin-jalali/termocode/internal/ai"
)

// RC-5 4e: the agent's tools in a container workspace read the
// container's files through the Host, never the local decoys.
func TestAIToolsInContainerUseTheHost(t *testing.T) {
	ws, h := newRCWorkspace(t, fstest.MapFS{"remote.go": {Data: []byte("package main // in the container\n")}})
	m := newTestModel(t)
	m.ws = ws
	aws := m.aiWorkspaceAt(ws.Root)
	if aws.FS == nil || aws.Exec == nil || aws.HostLabel != "api-1" {
		t.Fatalf("workspace = %+v", aws)
	}
	read, _ := ai.FindTool(ai.WorkspaceTools(aws), "read_file")
	out, err := read.Run(context.Background(), map[string]any{"path": "remote.go"})
	if err != nil || !strings.Contains(out, "in the container") {
		t.Fatalf("read_file = %q %v", out, err)
	}
	if _, err := read.Run(context.Background(), map[string]any{"path": "local.go"}); err == nil {
		t.Fatal("read_file read the local decoy")
	}
	if len(h.calls()) == 0 {
		t.Fatal("the Host FS was never asked")
	}
}

// A worktree or another local folder keeps the local tools.
func TestAIWorkspaceAtOtherRootStaysLocal(t *testing.T) {
	ws, _ := newRCWorkspace(t, fstest.MapFS{})
	m := newTestModel(t)
	m.ws = ws
	if aws := m.aiWorkspaceAt(t.TempDir()); aws.FS != nil {
		t.Fatal("a local folder got the container host")
	}
	local := newTestModel(t)
	if aws := local.aiWorkspaceAt(t.TempDir()); aws.FS != nil {
		t.Fatal("a local workspace got a host")
	}
}

func TestPathUnder(t *testing.T) {
	for _, c := range []struct {
		root, p string
		want    bool
	}{{"/app", "/app", true}, {"/app", "/app/x", true}, {"/app", "/apple", false}, {"/app", "/", false}, {"/", "/x", true}, {"", "/x", false}} {
		if got := pathUnder(c.root, c.p); got != c.want {
			t.Errorf("pathUnder(%q, %q) = %v", c.root, c.p, got)
		}
	}
}

// A file with unsaved edits in the editor is never overwritten by the
// agent in a container either.
func TestAISyncHostToolsRefuseUnsaved(t *testing.T) {
	ran := false
	tools := []ai.Tool{{Name: "write_file", Run: func(context.Context, map[string]any) (string, error) { ran = true; return "wrote", nil }}}
	refuse := func(context.Context, aiIDEFunc) (string, error) {
		return "", errUnsavedForTest
	}
	w := ai.Workspace{Root: "/app", FS: rcFS{}}
	got := aiSyncHostTools(tools, w, refuse)
	if _, err := got[0].Run(context.Background(), map[string]any{"path": "a.go", "content": "x"}); err != errUnsavedForTest || ran {
		t.Fatalf("err = %v, ran = %v", err, ran)
	}
}

var errUnsavedForTest = &unsavedErr{}

type unsavedErr struct{}

func (*unsavedErr) Error() string { return "unsaved" }
