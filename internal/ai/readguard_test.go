package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guarded is the workspace tools of w with a fresh write guard.
func guarded(t *testing.T, w Workspace) []Tool {
	t.Helper()
	return GuardWrites(WorkspaceTools(w), w, NewFileLedger())
}

// gcall validates and runs one tool as the agent does: a Validate error
// is the result the model gets, and nothing runs.
func gcall(t *testing.T, tools []Tool, name string, args map[string]any) (string, error) {
	t.Helper()
	tl, ok := FindTool(tools, name)
	if !ok {
		t.Fatalf("no tool %q", name)
	}
	if tl.Validate != nil {
		if err := tl.Validate(args); err != nil {
			return "", err
		}
	}
	return tl.Run(context.Background(), args)
}

// write_file on a file that exists but was not read (or written) in
// this chat is refused before it asks the user: the model would replace
// code it never saw. It says what to do instead.
func TestWriteFileNeedsARead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	tools := guarded(t, Workspace{Root: dir})

	_, err := gcall(t, tools, "write_file", map[string]any{"path": "main.go", "content": "package main\n"})
	if err == nil || !strings.Contains(err.Error(), "read it first") || !strings.Contains(err.Error(), "edit_file") {
		t.Fatalf("unread write: err = %v, want a refusal naming read_file and edit_file", err)
	}
	if got := readBack(t, filepath.Join(dir, "main.go")); got != "package main\n\nfunc main() {}\n" {
		t.Fatalf("the file changed: %q", got)
	}

	if _, err := gcall(t, tools, "read_file", map[string]any{"path": "main.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "./main.go", "content": "package main\n"}); err != nil {
		t.Fatalf("after a read: %v", err)
	}
	if got := readBack(t, filepath.Join(dir, "main.go")); got != "package main\n" {
		t.Fatalf("not written: %q", got)
	}
}

// A new file needs no read, and a file this chat wrote or edited can be
// written again.
func TestWriteFileNewOrOwnFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "one\n")
	tools := guarded(t, Workspace{Root: dir})
	for i, c := range []struct{ tool, path string }{
		{"write_file", "new.txt"}, {"write_file", "new.txt"},
		{"create_file", "made.txt"}, {"write_file", "made.txt"},
	} {
		if _, err := gcall(t, tools, c.tool, map[string]any{"path": c.path, "content": "x\n"}); err != nil {
			t.Fatalf("%d %s %s: %v", i, c.tool, c.path, err)
		}
	}
	if _, err := gcall(t, tools, "edit_file", map[string]any{"path": "a.txt", "old_text": "one", "new_text": "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "a.txt", "content": "three\n"}); err != nil {
		t.Fatalf("after an edit: %v", err)
	}
}

// A failed read (a missing file) does not count as a read.
func TestWriteFileFailedReadDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	tools := guarded(t, Workspace{Root: dir})
	if _, err := gcall(t, tools, "read_file", map[string]any{"path": "late.txt"}); err == nil {
		t.Fatal("read of a missing file worked")
	}
	writeFile(t, filepath.Join(dir, "late.txt"), "made by the user\n")
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "late.txt", "content": "x"}); err == nil {
		t.Fatal("write of a file that appeared after a failed read was allowed")
	}
}

// The ledger belongs to one chat: a new ledger (a new chat) has read
// nothing.
func TestWriteFileLedgerIsPerChat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "one\n")
	w := Workspace{Root: dir}
	l := NewFileLedger()
	first := GuardWrites(WorkspaceTools(w), w, l)
	if _, err := gcall(t, first, "read_file", map[string]any{"path": "a.txt"}); err != nil {
		t.Fatal(err)
	}
	again := GuardWrites(WorkspaceTools(w), w, l) // the next turn of the same chat
	if _, err := gcall(t, again, "write_file", map[string]any{"path": "a.txt", "content": "x\n"}); err != nil {
		t.Fatalf("same chat, next turn: %v", err)
	}
	other := guarded(t, w)
	if _, err := gcall(t, other, "write_file", map[string]any{"path": "a.txt", "content": "y\n"}); err == nil {
		t.Fatal("another chat wrote a file it never read")
	}
}

// On a container workspace the guard asks the host whether the file is
// there.
func TestWriteFileNeedsAReadOnHost(t *testing.T) {
	w, f, _ := hostWorkspace(t)
	tools := guarded(t, w)
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "main.go", "content": "x"}); err == nil || !strings.Contains(err.Error(), "read it first") {
		t.Fatalf("unread host write: %v", err)
	}
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "new.go", "content": "package x\n"}); err != nil {
		t.Fatalf("new host file: %v", err)
	}
	if _, err := gcall(t, tools, "read_file", map[string]any{"path": "main.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gcall(t, tools, "write_file", map[string]any{"path": "main.go", "content": "x"}); err != nil {
		t.Fatalf("after a host read: %v", err)
	}
	if string(f.files["main.go"].Data) != "x" {
		t.Fatalf("host file = %q", f.files["main.go"].Data)
	}
}

func readBack(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
