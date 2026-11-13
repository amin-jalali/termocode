package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// RC-6: a server of a container workspace gets the root of the file's
// own project (the nearest folder with one of its markers, looked up
// through the container FS), not always the workspace root: a monorepo
// /app with /app/svc/go.mod starts gopls on /app/svc.
func TestRemoteFilesRootFindsTheNearestMarker(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t, func(c *fakedocker.Container) {
		c.WriteFile(t, "/app/svc/go.mod", []byte("module svc\n"), 0o644)
		c.WriteFile(t, "/app/svc/cmd/main.go", []byte("package main\n"), 0o644)
		c.WriteFile(t, "/go.mod", []byte("module above\n"), 0o644)
	})
	r := newRemoteFiles(ws)
	for _, tc := range []struct {
		start   string
		markers []string
		want    string
	}{
		{"/app/svc/cmd", []string{"go.work", "go.mod", ".git"}, "/app/svc"},
		{"/app/svc", []string{"go.mod"}, "/app/svc"},
		{"/app/pkg", []string{"go.mod"}, "/app"},     // none below the root: the root
		{"/app/pkg", []string{"Cargo.toml"}, "/app"}, // never above the root (/go.mod)
		{"/etc", []string{"go.mod"}, "/app"},         // outside the root: the root
	} {
		if got := r.root(tc.start, tc.markers, "/app"); got != tc.want {
			t.Errorf("root(%s, %v) = %q, want %q", tc.start, tc.markers, got, tc.want)
		}
	}
}

// The Lua side asks for it (termocode_fs_root) and reuses a client only
// for the same root.
func TestLSPSetupLuaAsksTheContainerForTheRoot(t *testing.T) {
	for _, want := range []string{"'termocode_fs_root'", "client.config.root_dir == conf.root_dir"} {
		if !strings.Contains(lspSetupLua, want) {
			t.Errorf("lspSetupLua has no %s", want)
		}
	}
}

func TestRemoteBufferRootRequest(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t, func(c *fakedocker.Container) {
		c.WriteFile(t, "/app/svc/go.mod", []byte("module svc\n"), 0o644)
	})
	n := remoteNvimOn(t, ws)
	got := remoteEval(t, n, `return vim.rpcrequest(vim.g.termocode_channel, 'termocode_fs_root', '/app/svc/x', {'go.mod'}, '/app')`)
	if got != "/app/svc" {
		t.Fatalf("termocode_fs_root = %q", got)
	}
}
