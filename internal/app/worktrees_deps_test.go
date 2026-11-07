package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

func wtDepEntry(path, branch string, md worktrees.Meta) worktrees.Entry {
	return worktrees.Entry{Worktree: git.Worktree{Path: path, Branch: branch}, Kind: worktrees.KindAgent, Meta: md}
}

// CMP-80: the graph card lists the waves; each row says done, ready or
// what it waits for.
func TestWTDepsRows(t *testing.T) {
	es := []worktrees.Entry{
		wtDepEntry("/a", "feat/a", worktrees.Meta{Checkpoint: worktrees.CheckDone}),
		wtDepEntry("/b", "feat/b", worktrees.Meta{BlockedBy: []string{"/a"}}),
		wtDepEntry("/c", "feat/c", worktrees.Meta{BlockedBy: []string{"/b"}}),
		wtDepEntry("/lone", "feat/lone", worktrees.Meta{}),
	}
	rows := wtDepsRows(es)
	var flat []string
	for _, r := range rows {
		flat = append(flat, strings.TrimSpace(r.Key+" "+r.What))
	}
	want := []string{
		"Wave 1",
		"done feat/a",
		"Wave 2",
		"ready feat/b · after feat/a",
		"Wave 3",
		"waits feat/c · waits for feat/b",
	}
	if !reflect.DeepEqual(flat, want) {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(flat, "\n"), strings.Join(want, "\n"))
	}
	if wtDepsRows(es[3:]) != nil {
		t.Fatal("no edges: no rows")
	}
}

// The control socket's worktree-list says what each worktree waits for
// (by title), so agents and the SDK see the graph too.
func TestWTCtlListBlockedBy(t *testing.T) {
	es := []worktrees.Entry{
		wtDepEntry("/a", "feat/a", worktrees.Meta{}),
		wtDepEntry("/b", "feat/b", worktrees.Meta{BlockedBy: []string{"/a", "/gone"}}),
	}
	list := wtCtlList(es, "/b")
	if !reflect.DeepEqual(list[1].BlockedBy, []string{"feat/a"}) || !list[1].Waits {
		t.Fatalf("b = %+v", list[1])
	}
	if list[0].Waits || len(list[0].BlockedBy) != 0 {
		t.Fatalf("a = %+v", list[0])
	}
}

func TestWTDepsPaletteRowAndPolicy(t *testing.T) {
	found := false
	for _, it := range worktreePaletteItems() {
		found = found || it.ID == wtDepsPaletteID
	}
	if !found {
		t.Fatal("no palette row for the dependency graph")
	}
	if _, ok := aiActionPolicies[wtDepsPaletteID]; !ok {
		t.Fatal("no AI policy for the dependency graph")
	}
}

// worktree_block records the edge in worktrees.json (auto: metadata
// only), refuses a self edge, and remove drops it; outside agents get it.
func TestWorktreeBlockTool(t *testing.T) {
	withConfigDir(t)
	root, feat, common := wtRepoFeat(t)
	tool := aiWorktreeBlockTool(root)
	if tool.Mutating {
		t.Fatal("worktree_block must not ask: it writes metadata only")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"worktree": "feat/x", "waits_for": root}); err != nil {
		t.Fatal(err)
	}
	md, _ := worktrees.Load(common)
	if len(md[feat].BlockedBy) != 1 {
		t.Fatalf("blocked by %v", md[feat].BlockedBy)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"worktree": "feat/x", "waits_for": "feat/x"}); err == nil {
		t.Error("a worktree waits for itself")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"worktree": "feat/x", "waits_for": "nowhere"}); err == nil {
		t.Error("an unknown blocker was taken")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"worktree": "feat/x", "waits_for": root, "remove": true}); err != nil {
		t.Fatal(err)
	}
	md, _ = worktrees.Load(common)
	if len(md[feat].BlockedBy) != 0 {
		t.Fatalf("still blocked by %v", md[feat].BlockedBy)
	}
	if _, ok := ai.FindTool(aiWorktreeTools(root), "worktree_block"); !ok {
		t.Error("worktree_block is not in the chat's worktree tools")
	}
	if _, ok := mcpServeRow("worktree_block"); !ok {
		t.Error("worktree_block not offered to outside agents")
	}
}
