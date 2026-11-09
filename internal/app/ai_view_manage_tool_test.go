package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// AI parity (saved views): view_manage does the phone's saved-view
// actions: list, rename, pin and unpin at once (no card on the page) and
// delete after a card (the page asks Cancel first).
func TestAIViewManageTool(t *testing.T) {
	m := aiTestModel(t)
	if err := saveSavedViews([]webproto.SavedView{
		{ID: "v1", Name: "Open PRs", Spec: webproto.ViewSpec{View: "ci.prs"}, Saved: 1},
		{ID: "v2", Name: "Failing", Spec: webproto.ViewSpec{View: "ci.prs"}, Saved: 2},
	}); err != nil {
		t.Fatal(err)
	}
	tl := aiToolOf(t, &m, "view_manage")
	for op, ask := range map[string]bool{"list": false, "rename": false, "pin": false, "unpin": false, "delete": true} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s: ask=%v class=%q, want ask=%v and no Always allow", op, got, class, ask)
		}
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "list"}); err != nil || !strings.Contains(out, `v1 · "Open PRs" · ci.prs`) {
		t.Fatalf("list: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "rename", "id": "v1", "name": "Failing"}); err == nil {
		t.Error("a rename took another view's name")
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "rename", "id": "v1", "name": "My PRs"}); err != nil || !strings.Contains(out, "My PRs") {
		t.Fatalf("rename: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "pin", "id": "v2"}); err != nil {
		t.Fatal(err)
	}
	vs, _ := loadSavedViews()
	if i := savedViewIndex(vs, "v2"); i < 0 || !vs[i].Pinned {
		t.Fatalf("v2 not pinned: %+v", vs)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "unpin", "id": "v2"}); err != nil {
		t.Fatal(err)
	}
	if card := tl.Describe(map[string]any{"op": "delete", "id": "v1"}); !strings.Contains(card, `Delete the saved view "My PRs"?`) {
		t.Errorf("delete card: %s", card)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "delete", "id": "v1"}); err != nil {
		t.Fatal(err)
	}
	vs, _ = loadSavedViews()
	if len(vs) != 1 || vs[0].ID != "v2" || vs[0].Pinned {
		t.Errorf("after the delete: %+v", vs)
	}
}
