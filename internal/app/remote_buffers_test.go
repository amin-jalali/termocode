package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

func TestRemoteFilesReadsAndWritesTheContainer(t *testing.T) {
	ws, _, _, c := fakeContainerWS(t)
	r := newRemoteFiles(ws)
	got := r.read("/app/main.go")
	if got["ok"] != true || got["text"] != "package main\n\nfunc main() {}\n" || got["new"] != false {
		t.Fatalf("read = %v", got)
	}
	got = r.read("/app/missing.go")
	if got["ok"] != true || got["text"] != "" || got["new"] != true {
		t.Fatalf("read of a new file = %v", got)
	}
	if res := r.write("/app/main.go", "package main\n"); res["ok"] != true {
		t.Fatalf("write = %v", res)
	}
	if b, _ := os.ReadFile(c.Local("/app/main.go")); string(b) != "package main\n" {
		t.Fatalf("container file = %q", b)
	}
}

func TestRemoteFilesGoneContainerSaysSo(t *testing.T) {
	ws, _, _, c := fakeContainerWS(t)
	r := newRemoteFiles(ws)
	c.Remove()
	if got := r.read("/app/main.go"); got["ok"] != false || got["err"] != "api-1 is not running." {
		t.Fatalf("read = %v", got)
	}
	res := r.write("/app/main.go", "lost?")
	if res["ok"] != false || res["err"] != "api-1 is not running." {
		t.Fatalf("write = %v", res)
	}
	if b, _ := os.ReadFile(c.Local("/app/main.go")); strings.Contains(string(b), "lost?") {
		t.Fatal("a removed container got the write")
	}
}

func TestRemoteFilesRefusesPathsOutsideTheRoot(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t)
	r := newRemoteFiles(ws)
	if got := r.read("/etc/passwd"); got["ok"] != false || got["err"] != "The path is outside the workspace." {
		t.Fatalf("read = %v", got)
	}
}

// Plan decision 1: a new file written as root is accepted, with one
// warning per session.
func TestRemoteFilesWarnsOnceAboutRootOwnedNewFiles(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t, func(c *fakedocker.Container) { c.NoShell() }) // runs as root
	r := newRemoteFiles(ws)
	res := r.write("/app/new.go", "package main\n")
	if res["ok"] != true || !strings.Contains(res["warn"].(string), "root") {
		t.Fatalf("first new file = %v", res)
	}
	if res := r.write("/app/other.go", "x\n"); res["ok"] != true || res["warn"] != "" {
		t.Fatalf("second new file warned again: %v", res)
	}
	if res := r.write("/app/main.go", "x\n"); res["warn"] != "" {
		t.Fatalf("existing file warned: %v", res)
	}
}

func TestRemoteFilesNoRootWarningForAnotherUser(t *testing.T) {
	ws, h, _, _ := fakeContainerWS(t)
	if uid, _, ok := h.Owner(); !ok || uid == 0 {
		t.Skip("the test runs as root")
	}
	if res := newRemoteFiles(ws).write("/app/new.go", "x\n"); res["ok"] != true || res["warn"] != "" {
		t.Fatalf("write = %v", res)
	}
}
