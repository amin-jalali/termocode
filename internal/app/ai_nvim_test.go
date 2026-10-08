package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"termocode/internal/debounce"
	"termocode/internal/nvim"
)

// TestAIGhostTextAndProposalNvim drives the Lua half of Group A against a
// real nvim: ghost text shows, Tab-accept rewrites the typed word without
// duplicating it (plus the import), and an AI proposal applies as one undo
// step.
func TestAIGhostTextAndProposalNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	for _, meth := range []string{"termocode_ai_changed", "termocode_ai_cancel"} {
		_ = c.RegisterNotify(meth, nil)
	}
	if err := c.ExecLua(aiLua); err != nil {
		t.Fatalf("aiLua: %v", err)
	}
	if err := c.ExecLua(snippetsLua); err != nil {
		t.Fatalf("snippetsLua: %v", err)
	}
	path := filepath.Join(t.TempDir(), "main.go")
	_ = os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tprint\n}\n"), 0o644)
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	_ = c.Command("call cursor(4, 1)")
	_ = c.Input("A") // append at end of "\tprint" → insert mode
	mode := ""
	for i := 0; i < 50 && !strings.HasPrefix(mode, "i"); i++ {
		mode, _ = c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.HasPrefix(mode, "i") {
		t.Fatalf("not in insert mode: %q", mode)
	}
	m := Model{w: 100, h: 30, nvim: c, debounce: debounce.New(), ai: newAIState()}
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || snap.LineBefore != "\tprint" || snap.Row != 3 {
		t.Fatalf("snapshot %+v ok=%v", snap, ok)
	}
	gen := m.debounce.Next(aiInlineReqID)
	m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: snap, raw: "IMPORT: import \"fmt\"\nfmt.Println(\"hi\")"})
	if has, _ := c.EvalLuaString(`return _G._termocode_ai.has() and 'y' or ''`); has != "y" {
		t.Fatal("ghost text not shown")
	}
	if ok, _ := c.EvalLuaString(`return _G._termocode_ai.accept() and 'y' or ''`); ok != "y" {
		t.Fatal("accept failed")
	}
	lines, _ := c.CurrentBufLines()
	want := []string{"package main", "", `import "fmt"`, "", "func main() {", "\tfmt.Println(\"hi\")", "}"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("after accept:\n%s", strings.Join(lines, "\n"))
	}
	// Stale ghost (buffer changed) must not show.
	m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: snap, raw: "x"})
	if has, _ := c.EvalLuaString(`return _G._termocode_ai.has() and 'y' or ''`); has == "y" {
		t.Fatal("stale ghost shown")
	}

	// Proposal: replace the Println line, apply, undo once.
	_ = c.Input("<Esc>")
	tgt, ok := m.aiCaptureTarget(false)
	if !ok {
		t.Fatal("capture target")
	}
	tgt.start, tgt.end = 5, 6
	m.openAIProposal(aiProposal{target: tgt, repl: []string{"\tfmt.Println(\"hello\")", "\treturn"}, title: "test"})
	wins, _ := c.EvalLuaString(`return tostring(#vim.api.nvim_list_wins())`)
	if wins != "2" {
		t.Fatalf("diff not opened, wins=%s", wins)
	}
	m.confirmOpen = false
	m.applyAIProposal()
	wins, _ = c.EvalLuaString(`return tostring(#vim.api.nvim_list_wins())`)
	lines, _ = c.CurrentBufLines()
	if wins != "1" || lines[5] != "\tfmt.Println(\"hello\")" || lines[6] != "\treturn" {
		t.Fatalf("apply: wins=%s lines=%q", wins, lines)
	}
	_ = c.Command("silent! undo")
	lines, _ = c.CurrentBufLines()
	if lines[5] != "\tfmt.Println(\"hi\")" || len(lines) != 7 {
		t.Fatalf("one undo should restore: %q", lines)
	}
}
