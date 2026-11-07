package ai

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

var updateToolViews = flag.Bool("update-tool-views", false, "rewrite testdata/tool_views/*.json")

// checkToolViewGolden compares b (as JSON) with testdata/tool_views/name.json.
// The compare is on the decoded JSON, so spacing in the file does not count.
func checkToolViewGolden(t *testing.T, name string, b *blocks.Block) {
	t.Helper()
	got, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "tool_views", name+".json")
	if *updateToolViews {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run %s -update-tool-views writes it)", err, t.Name())
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s differs:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

// validView fails the test when b is nil or breaks a block rule (with
// the id the agent would fill).
func validView(t *testing.T, b *blocks.Block) {
	t.Helper()
	if b == nil {
		t.Fatal("no block")
	}
	c := *b
	c.ID = "call-1"
	if err := c.Validate(); err != nil {
		t.Fatalf("invalid block: %v", err)
	}
	if b.ID != "" || b.Source != "" {
		t.Errorf("a view leaves id and source to the agent: %q %q", b.ID, b.Source)
	}
}

// MOB-11: list_dir rows refer to their entry, folders carry a badge.
func TestListDirViewGolden(t *testing.T) {
	b := ListDirView(map[string]any{"path": "internal/ai/"}, "testdata/\ntools.go\ntodos.go\n")
	validView(t, b)
	checkToolViewGolden(t, "list_dir", b)
}

// An empty folder is an idle status line; a cut listing drops the
// partial last line and says it was cut.
func TestListDirViewEmptyAndCut(t *testing.T) {
	b := ListDirView(map[string]any{}, "")
	validView(t, b)
	if b.Kind != blocks.KindStatus || b.State != blocks.ToneIdle || b.Title != "The folder . is empty" {
		t.Errorf("empty = %+v", b)
	}
	b = ListDirView(nil, "a.go\nb.go\nc.g\n… [truncated, 99 more bytes]")
	validView(t, b)
	if len(b.Items) != 2 || !b.Truncated || !strings.HasSuffix(b.Title, "+") {
		t.Errorf("cut = %+v", b)
	}
}

// A path that leaves the workspace never becomes a ref.
func TestListDirViewNoRefOutsideWorkspace(t *testing.T) {
	b := ListDirView(map[string]any{"path": "../etc"}, "passwd\n")
	validView(t, b)
	if b.Items[0].Ref != nil {
		t.Errorf("ref = %+v", b.Items[0].Ref)
	}
	for _, p := range []string{"/etc/passwd", "../x", "a/../../b", `C:\x`, "https://x", "a\x00b"} {
		if got := ViewPath(p); got != "" {
			t.Errorf("ViewPath(%q) = %q", p, got)
		}
	}
}

// MOB-11: search rows are path:line with the line below, each a lines ref.
func TestSearchViewGolden(t *testing.T) {
	out := "internal/ai/tools.go:25: type Tool struct {\ncmd/x/main.go:7: tools := ai.Tool{}\n(showing 2 of 9)\n"
	b := SearchView(map[string]any{"query": "Tool"}, out)
	validView(t, b)
	checkToolViewGolden(t, "search", b)
}

func TestSearchViewNoMatches(t *testing.T) {
	b := SearchView(map[string]any{"query": "zzz"}, "no matches")
	validView(t, b)
	if b.Kind != blocks.KindStatus || b.State != blocks.ToneIdle {
		t.Errorf("no matches = %+v", b)
	}
	if SearchView(nil, "garbage without a line number") != nil {
		t.Error("unreadable text gave a block")
	}
}

// Over-long and control text is cut to the caps, never dropped.
func TestSearchViewCapsText(t *testing.T) {
	long := strings.Repeat("é", 500)
	b := SearchView(map[string]any{"query": long}, "a.go:1: "+long+"\x1b[31m\n")
	validView(t, b)
	if n := len([]rune(b.Items[0].Sub)); n > blocks.MaxSub {
		t.Errorf("sub has %d runes", n)
	}
}

// MOB-11: todo_read rows carry tone and shape word: done, doing, pending.
func TestTodosViewGolden(t *testing.T) {
	b := TodosTextView(nil, "[x] read the plan\n[~] write the views\n[ ] mark the task")
	validView(t, b)
	checkToolViewGolden(t, "todo_read", b)
	b = TodosTextView(nil, "the todo list is empty")
	validView(t, b)
	if b.Kind != blocks.KindStatus {
		t.Errorf("empty = %+v", b)
	}
}

// The workspace tools carry their views.
func TestWorkspaceToolsHaveViews(t *testing.T) {
	tools := WorkspaceTools(Workspace{Root: t.TempDir()})
	for _, n := range []string{"list_dir", "search"} {
		tl, ok := FindTool(tools, n)
		if !ok || tl.View == nil {
			t.Errorf("%s has no View", n)
		}
	}
}
