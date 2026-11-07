package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// aiStashGated is git_stash with its gate applied for op, as the chat
// agent and the MCP bridge see it before they ask.
func aiStashGated(t *testing.T, tl ai.Tool, op string) (ai.Tool, map[string]any) {
	t.Helper()
	args := map[string]any{"op": op, "index": 0}
	tl.Mutating, tl.Class = tl.Gate(args)
	return tl, args
}

// Spec: the stash tools run where the workspace lives. In a container
// workspace list, save, apply, pop and drop run git in the container
// through the Host Exec; nothing runs on the local disk.
func TestAIGitStashToolsRunInContainer(t *testing.T) {
	m, h := rcStashWorkspace(t)
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(aiGitTools(m.workspace().Root, call), "git_stash")
	if !ok {
		t.Fatal("no git_stash")
	}
	run := func(args map[string]any) string {
		t.Helper()
		if err := tl.Validate(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		out, err := tl.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	if out := run(map[string]any{"op": "list"}); !strings.Contains(out, "stash@{0}: On remote-main: old wip") {
		t.Errorf("list %q, want the container's stash", out)
	}
	if out := run(map[string]any{"op": "save", "message": "box wip", "untracked": true}); !strings.Contains(out, "stash@{0}") {
		t.Errorf("save %q", out)
	}
	run(map[string]any{"op": "apply", "index": 1})
	run(map[string]any{"op": "pop", "index": 0})
	run(map[string]any{"op": "drop", "index": 0})
	for _, want := range []string{
		"git stash list --pretty=%gd|%s",
		"git stash push -m box wip --include-untracked",
		"git stash apply stash@{1}",
		"git stash apply stash@{0}",
		"git stash drop stash@{0}",
	} {
		if !rcRan(h, want) {
			t.Errorf("%q not run in the container: %q", want, h.exec.calls())
		}
	}
}

// Spec: stash drop and pop ask on a Cancel-first card with no Always
// allow (drop loses the entry; pop removes it from the list). save and
// apply keep the plain Deny-first card with the git Always allow.
func TestAIGitStashLossCardsAreCancelFirst(t *testing.T) {
	tl := aiGitTool(t, "git_stash")
	for _, op := range []string{"drop", "pop"} {
		g, _ := aiStashGated(t, tl, op)
		bs := aiToolCardButtons(g)
		if bs[0].ID != "deny" || bs[0].Title != "Cancel" {
			t.Errorf("%s: first button %+v, want Cancel", op, bs[0])
		}
		for _, b := range bs {
			if b.ID == "always" {
				t.Errorf("%s: offers %q", op, b.Title)
			}
		}
	}
	for _, op := range []string{"save", "apply"} {
		g, _ := aiStashGated(t, tl, op)
		bs := aiToolCardButtons(g)
		if bs[0].ID != "deny" || bs[0].Title != "Deny" {
			t.Errorf("%s: first button %+v, want Deny", op, bs[0])
		}
		if bs[len(bs)-1].ID != "always" {
			t.Errorf("%s: no Always allow git", op)
		}
	}
}

// Spec: outside agents get git_stash over MCP: list runs at once; save
// and apply ask; drop and pop ask on a Cancel-first card that never
// offers Always allow.
func TestMCPServeStashTools(t *testing.T) {
	aiTestModel(t)
	tl, ok := ai.FindTool(mcpServeCatalog(), "git_stash")
	if !ok {
		t.Fatal("git_stash is not exposed over MCP")
	}
	if g, asks := mcpServeGate(tl, map[string]any{"op": "list"}); asks {
		t.Errorf("list asks (class %q)", g.Class)
	}
	for _, op := range []string{"save", "apply", "pop", "drop"} {
		if _, asks := mcpServeGate(tl, map[string]any{"op": op}); !asks {
			t.Errorf("%s runs without asking", op)
		}
	}
	for _, op := range []string{"drop", "pop"} {
		g, args := aiStashGated(t, tl, op)
		if k := mcpServeAlwaysKey(g, args); k != "" {
			t.Errorf("%s: Always allow key %q, want none", op, k)
		}
		m := &Model{mcpServe: &mcpServeState{queue: []*mcpServeAsk{{
			ctx: context.Background(), client: "claude", tool: g, human: g.Describe(args), reply: make(chan bool, 1),
		}}}}
		m.mcpServePump()
		if !m.confirmOpen || m.confirmKind != confirmKindMCPServe {
			t.Fatalf("%s: no card", op)
		}
		bs := m.confirm.Buttons()
		if bs[0] != (confirm.Button{ID: "deny", Title: "Cancel"}) {
			t.Errorf("%s: first button %+v, want a plain Cancel", op, bs[0])
		}
		for _, b := range bs {
			if b.ID == "always" {
				t.Errorf("%s: offers Always allow", op)
			}
		}
	}
}
