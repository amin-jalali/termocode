package app

import (
	"os"
	"strings"
	"testing"
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
