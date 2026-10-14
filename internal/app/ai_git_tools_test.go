package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

func aiGitTool(t *testing.T, name string) ai.Tool {
	t.Helper()
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) { return "", nil }
	tl, ok := ai.FindTool(aiGitTools(t.TempDir(), call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// K4: read ops run at once; stage/unstage/stash push|apply|pop ask with
// class git; hunk discard and stash drop ask with class "" (no Always
// allow: they lose data).
func TestAIGitToolGates(t *testing.T) {
	type want struct {
		mutating bool
		class    string
	}
	cases := []struct {
		tool string
		args map[string]any
		want want
	}{
		{"git_hunk", map[string]any{"op": "stage"}, want{true, ai.ClassGit}},
		{"git_hunk", map[string]any{"op": "unstage"}, want{true, ai.ClassGit}},
		{"git_hunk", map[string]any{"op": "discard"}, want{true, ""}},
		{"git_stash", map[string]any{"op": "list"}, want{false, ""}},
		{"git_stash", map[string]any{"op": "push"}, want{true, ai.ClassGit}},
		{"git_stash", map[string]any{"op": "apply"}, want{true, ai.ClassGit}},
		{"git_stash", map[string]any{"op": "pop"}, want{true, ai.ClassGit}},
		{"git_stash", map[string]any{"op": "drop"}, want{true, ""}},
	}
	for _, c := range cases {
		m, cl := aiGitTool(t, c.tool).Gate(c.args)
		if (want{m, cl}) != c.want {
			t.Errorf("%s %v: got (%v, %q), want %+v", c.tool, c.args, m, cl, c.want)
		}
	}
	for name, want := range map[string]want{
		"git_log": {false, ""}, "git_blame": {false, ""},
		"git_unstage": {true, ai.ClassGit}, "git_fetch": {true, ai.ClassGit},
	} {
		tl := aiGitTool(t, name)
		if tl.Gate != nil || tl.Mutating != want.mutating || tl.Class != want.class {
			t.Errorf("%s: mutating %v class %q", name, tl.Mutating, tl.Class)
		}
	}
}

func TestAIGitToolValidate(t *testing.T) {
	bad := []struct {
		tool string
		args map[string]any
		msg  string
	}{
		{"git_hunk", map[string]any{"op": "revert", "path": "a", "line": 1}, "stage, unstage, discard"},
		{"git_hunk", map[string]any{"op": "stage", "path": "a", "line": 0}, "line"},
		{"git_hunk", map[string]any{"op": "stage", "path": "../x", "line": 1}, "outside"},
		{"git_stash", map[string]any{"op": "clear"}, "list, push, pop, apply, drop"},
		{"git_stash", map[string]any{"op": "drop", "index": -1}, "index"},
		{"git_unstage", map[string]any{"paths": []any{}}, "empty"},
	}
	for _, c := range bad {
		err := aiGitTool(t, c.tool).Validate(c.args)
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s %v: err %v, want %q", c.tool, c.args, err, c.msg)
		}
	}
	if err := aiGitTool(t, "git_stash").Validate(map[string]any{"op": "list"}); err != nil {
		t.Errorf("list: %v", err)
	}
}

// The unsaved check runs before the dialog: an error from the IDE side
// becomes a Validate error.
func TestAIGitDiscardPrecheckRefusesUnsaved(t *testing.T) {
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		return "", context.Canceled // stands in for "unsaved edits" from Update
	}
	tl, _ := ai.FindTool(aiGitTools(t.TempDir(), call), "git_hunk")
	if err := tl.Validate(map[string]any{"op": "discard", "path": "a.txt", "line": 1}); err == nil {
		t.Error("discard: precheck error was dropped")
	}
	if err := tl.Validate(map[string]any{"op": "stage", "path": "a.txt", "line": 1}); err != nil {
		t.Errorf("stage must not precheck: %v", err)
	}
}

func TestAIFormatBlameGroupsRuns(t *testing.T) {
	ls := []git.BlameRangeLine{
		{Line: 1, Hash: "aaaaaaa", Author: "Ann", Date: "2026-01-02", Subject: "one"},
		{Line: 2, Hash: "aaaaaaa", Author: "Ann", Date: "2026-01-02", Subject: "one"},
		{Line: 3, Hash: "uncommitted"},
		{Line: 4, Hash: "aaaaaaa", Author: "Ann", Date: "2026-01-02", Subject: "one"},
	}
	want := "1-2 aaaaaaa Ann 2026-01-02 one\n3 uncommitted (working tree)\n4 aaaaaaa Ann 2026-01-02 one"
	if got := aiFormatBlame(ls); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
