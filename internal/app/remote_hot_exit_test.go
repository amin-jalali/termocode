package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Unsaved text of a remote buffer is backed up locally (never in the
// container) and comes back into the remote buffer on the next start.
func TestHotExitRestoresRemoteBuffers(t *testing.T) {
	for _, keep := range []bool{false, true} {
		ws, _, _, c := fakeContainerWS(t)
		n := remoteNvimOn(t, ws)
		dir := t.TempDir()
		if err := n.ExecLuaArgs(hotExitLua, dir); err != nil {
			t.Fatal(err)
		}
		_ = n.Command("edit /app/main.go")
		_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'unsaved', 'work'})`)
		if err := n.ExecLua(hotExitFlushLua); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
		if len(files) != 1 {
			t.Fatalf("backups = %v", files)
		}
		data, _ := os.ReadFile(files[0])
		b, ok := parseHotBackup(data)
		if !ok || b.Path != "/app/main.go" {
			t.Fatalf("backup = %q", data)
		}

		// The next run: a fresh nvim on the same container.
		n2 := remoteNvimOn(t, ws)
		m := Model{nvim: n2, ws: ws}
		if err := m.restoreHotBackups([]hotBackup{b}, keep); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		var got string
		for time.Now().Before(deadline) {
			got = remoteEval(t, n2, `local b = vim.fn.bufnr('/app/main.go')
return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n') .. '|' .. tostring(vim.bo[b].modified)`)
			if got == "unsaved\nwork|true" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if got != "unsaved\nwork|true" {
			t.Fatalf("keep %v: restored buffer = %q", keep, got)
		}
		if data, _ := os.ReadFile(c.Local("/app/main.go")); string(data) != "package main\n\nfunc main() {}\n" {
			t.Fatalf("restore changed the container file: %q", data)
		}
	}
}

// Backups of a container workspace live in the local state folder, under
// the key of the container ID (not of any local folder).
func TestHotExitRootOfAContainerIsLocalAndOwnKey(t *testing.T) {
	ws, _, _, _ := fakeContainerWS(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root, err := hotExitRootFor(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	local, _ := hotExitRoot("/app")
	if root == local || !strings.HasPrefix(root, os.Getenv("XDG_STATE_HOME")) {
		t.Fatalf("backup root = %s (local /app = %s)", root, local)
	}
}
