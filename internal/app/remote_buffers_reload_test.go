package app

import (
	"os"
	"strings"
	"testing"
)

// RC-5: checktime never rereads a BufReadCmd buffer, so a remote buffer
// kept the old text after a discard (whole file or one hunk), a git op or
// an AI write in the container. The explicit reload after those reads
// the container file again; a buffer with unsaved edits keeps them.
func TestRemoteBufferReloadsAfterAChangeInTheContainer(t *testing.T) {
	n, c := remoteNvim(t)
	_ = n.Command("edit /app/main.go")
	if err := os.WriteFile(c.Local("/app/main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := n.ExecLua(checkTimeAllLua); err != nil {
		t.Fatal(err)
	}
	if got := remoteBufText(t, n); got != "package main\n\nfunc main() { println(1) }" {
		t.Fatalf("buffer after reload = %q", got)
	}
	if got := remoteEval(t, n, `return tostring(vim.bo.modified)`); got != "false" {
		t.Fatalf("modified after reload = %s", got)
	}
}

func TestRemoteBufferReloadKeepsUnsavedEdits(t *testing.T) {
	n, c := remoteNvim(t)
	_ = n.Command("edit /app/main.go")
	_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'mine'})`)
	if err := os.WriteFile(c.Local("/app/main.go"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := n.ExecLua(checkTimeAllLua); err != nil {
		t.Fatal(err)
	}
	if got := remoteBufText(t, n); got != "mine" {
		t.Fatalf("unsaved edits lost: %q", got)
	}
	if got := remoteEval(t, n, `return tostring(vim.b.termocode_disk_warned ~= nil)`); got != "true" {
		t.Fatal("a kept buffer must be marked as warned (the AI reports it as kept)")
	}
}

// The AI's sync (aiCheckTimeLua) reports a reloaded remote buffer.
func TestAICheckTimeReportsAReloadedRemoteBuffer(t *testing.T) {
	n, c := remoteNvim(t)
	_ = n.Command("edit /app/main.go")
	if err := os.WriteFile(c.Local("/app/main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := remoteEval(t, n, aiCheckTimeLua)
	if !strings.Contains(out, "/app/main.go") || !strings.Contains(out, "reloaded") {
		t.Fatalf("aiCheckTime = %s", out)
	}
	if got := remoteBufText(t, n); got != "package main" {
		t.Fatalf("buffer = %q", got)
	}
}
