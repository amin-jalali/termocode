package app

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiGated is tl with its gate applied for args, as the chat agent and
// the MCP bridge see it before they ask.
func aiGated(tl ai.Tool, args map[string]any) ai.Tool {
	if tl.Gate != nil {
		tl.Mutating, tl.Class = tl.Gate(args)
	}
	return tl
}

// aiWantCancelFirst fails unless the card of tl for args opens with
// Cancel (focused, not red) and offers no "Always allow".
func aiWantCancelFirst(t *testing.T, tl ai.Tool, args map[string]any) {
	t.Helper()
	g := aiGated(tl, args)
	if !g.Mutating {
		t.Errorf("%s %v runs at once; want a card", tl.Name, args)
		return
	}
	bs := aiToolCardButtons(g)
	if bs[0].ID != "deny" || bs[0].Title != "Cancel" {
		t.Errorf("%s %v: first button %+v, want Cancel", tl.Name, args, bs[0])
	}
	for _, b := range bs {
		if b.ID == "always" {
			t.Errorf("%s %v: offers %q", tl.Name, args, b.Title)
		}
	}
}

// aiWantAlways fails unless the card of tl for args offers "Always
// allow class".
func aiWantAlways(t *testing.T, tl ai.Tool, args map[string]any, class string) {
	t.Helper()
	g := aiGated(tl, args)
	if !g.Mutating || g.Class != class {
		t.Errorf("%s %v: mutating %v class %q, want a card with Always allow %s", tl.Name, args, g.Mutating, g.Class, class)
	}
}

// aiChatTool is the chat's tool name on root (call runs on m).
func aiChatToolOn(t *testing.T, m *Model, root, name string) ai.Tool {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiChatTools(root, call), name)
	if !ok {
		t.Fatalf("no chat tool %s", name)
	}
	return tl
}

// Spec: the GUI asks every time before a push, a checkout and an amend
// (the web's git.push, git.checkout and git.amend cards); the AI asks the
// same way: a Cancel-first card with no "Always allow git". A plain
// commit (no card in the GUI) keeps "Always allow git".
func TestAIGitPushCheckoutAmendAskEveryTime(t *testing.T) {
	m, dir := covGitRepo(t)
	aiWantCancelFirst(t, aiChatToolOn(t, m, dir, "git_push"), map[string]any{})
	aiWantCancelFirst(t, aiChatToolOn(t, m, dir, "git_checkout"), map[string]any{"branch": "main"})
	aiWantCancelFirst(t, aiChatToolOn(t, m, dir, "git_commit"), map[string]any{"message": "x", "amend": true})
	aiWantAlways(t, aiChatToolOn(t, m, dir, "git_commit"), map[string]any{"message": "x"}, ai.ClassGit)
}

// Spec: git_commit amend=true replaces the last commit (staged files go
// in; an empty message keeps the old one), as Commit (Amend) does;
// signoff adds the Signed-off-by trailer.
func TestAIGitCommitAmend(t *testing.T) {
	m, dir := covGitRepo(t)
	tl := aiChatToolOn(t, m, dir, "git_commit")
	covWrite(t, dir, "a.txt", "one\ntwo\nthree\nfour\n")
	covGit(t, dir, "add", "a.txt")
	if _, err := tl.Run(context.Background(), map[string]any{"message": "first, with four", "amend": true, "signoff": true}); err != nil {
		t.Fatal(err)
	}
	if n := covGit(t, dir, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("%s commits, want the one amended", n)
	}
	if s := covGit(t, dir, "log", "-1", "--format=%B"); s != "first, with four\n\nSigned-off-by: Tess <tess@example.com>" {
		t.Errorf("message %q", s)
	}
	if st := covGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("status %q, want the change in the commit", st)
	}
	if _, err := tl.Run(context.Background(), map[string]any{"amend": true}); err != nil {
		t.Fatalf("amend with no message: %v", err)
	}
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "first, with four" {
		t.Errorf("subject %q, want the old message kept", s)
	}
}
