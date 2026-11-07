package app

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

// checkToolView fails when b is nil or breaks a block rule (with the id
// the agent fills), then compares it with testdata/tool_views/name.json
// on the decoded JSON.
func checkToolView(t *testing.T, name string, b *blocks.Block) {
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

// MOB-11: one golden per tool view.
func TestWebSearchViewGolden(t *testing.T) {
	out := "1. Go docs\n   https://go.dev/doc/\n   The Go docs.\n2. Blog\n   http://www.example.com/blog?x=1"
	checkToolView(t, "web_search", webSearchView(map[string]any{"query": "go"}, out))
	if b := webSearchView(map[string]any{"query": "go"}, "No results."); b == nil || b.Kind != blocks.KindStatus {
		t.Errorf("no results = %+v", b)
	}
}

func TestWebFetchViewGolden(t *testing.T) {
	out := "Go docs\nhttps://go.dev/doc/ (text/html, cut)\n---\nHello\nworld"
	checkToolView(t, "web_fetch", webFetchView(nil, out))
	if webFetchView(nil, "not a page") != nil {
		t.Error("unreadable text gave a block")
	}
}

func TestWorkflowDraftViewGolden(t *testing.T) {
	out := "Created .termocode/workflows/ship.jsonc (valid: 3 step(s), 1 write(s); trusted for this version). " +
		"Nothing ran. workflow_dry_run shows what it would read and write."
	checkToolView(t, "workflow_draft", workflowDraftView(map[string]any{"name": "ship"}, out))
}

func TestRaceStartViewGolden(t *testing.T) {
	args := map[string]any{"task": "fix the bug", "agents": []any{"claude", "codex"}, "verify": "go test ./..."}
	checkToolView(t, "agent_race_start", raceStartView(args, "Race started with 2 lanes. The user follows it on the compare page."))
}

func TestTestsResultsViewGolden(t *testing.T) {
	out := "Test run go test ./... finished in 2s: 5 passed, 2 failed, 0 skipped.\n" +
		"2 failed:\n" +
		"- TestA (internal/x/a_test.go:12): want 1\n    got 2\n" +
		"- TestB/case (b_test.go:3)\n" +
		"Package errors (build failed or the suite did not load):\n" +
		"- internal/y (internal/y/y.go:4): undefined: z\n" +
		"Use open_file(path, line) to jump to a failure.\n" +
		"Passed: TestC, TestD"
	checkToolView(t, "tests_results", testsResultsView(nil, out))
}

// All green is a ✓ status line; a run still going is ◐.
func TestTestsResultsViewStatus(t *testing.T) {
	b := testsResultsView(nil, "Test run all finished in 1s: 3 passed, 0 failed, 1 skipped.\nPassed: A, B, C")
	if b == nil || b.Kind != blocks.KindStatus || b.State != blocks.ToneOK {
		t.Errorf("green = %+v", b)
	}
	b = testsResultsView(nil, "Tests are running (all); tests_results(wait_ms: 60000) waits for the end. So far: 1 passed, 0 failed, 0 skipped.")
	if b == nil || b.State != blocks.ToneBusy || !strings.HasPrefix(b.Title, "Tests running") {
		t.Errorf("running = %+v", b)
	}
	if testsResultsView(nil, "No test run yet. Start one with tests_run(scope: \"all\").") != nil {
		t.Error("no run gave a block")
	}
}

func TestDBQueryViewGolden(t *testing.T) {
	out := "2 rows on main (dev) · 4 ms\nid | name\n1 | a\\|b\n2 | NULL\n"
	checkToolView(t, "db_query", dbQueryView(nil, out))
}

// A statement without rows is a ✓ status line with its notices; a line
// that does not split into the header's cells drops the block.
func TestDBQueryViewStatusAndBadRow(t *testing.T) {
	b := dbQueryView(nil, "3 rows changed on main · 2 ms\nnotice: hi\n")
	if b == nil || b.Kind != blocks.KindStatus || b.ForYou != "notice: hi" {
		t.Errorf("change = %+v", b)
	}
	if dbQueryView(nil, "1 row on main · 1 ms\na | b\nonly-one\n") != nil {
		t.Error("a bad row gave a block")
	}
}

// Wide results keep 12 columns and say so; the cut is marked.
func TestDBQueryViewCapsColumns(t *testing.T) {
	var head, row []string
	for i := 0; i < 15; i++ {
		head = append(head, "c")
		row = append(row, "v")
	}
	out := "1 row on main · 1 ms\n" + strings.Join(head, " | ") + "\n" + strings.Join(row, " | ") + "\n… [truncated at 1 rows; use max_rows]\n"
	b := dbQueryView(nil, out)
	if b == nil || len(b.Columns) != blocks.MaxColumns || len(b.Rows[0]) != blocks.MaxColumns || !b.Truncated {
		t.Fatalf("wide = %+v", b)
	}
}

// The phone sees no more than the model: a password the db text hid
// stays hidden in the block.
func TestDBQueryViewKeepsHiddenCells(t *testing.T) {
	b := dbQueryView(nil, "1 row on main · 1 ms\nuser | pw\nbob | [hidden]\n")
	if b == nil || b.Rows[0][1] != "[hidden]" {
		t.Errorf("rows = %+v", b)
	}
}

// The tools carry their views.
func TestAppToolsHaveViews(t *testing.T) {
	root := t.TempDir()
	tools := append(aiTasksTools(root, nil), aiDBTools(root, nil)...)
	tools = append(tools, aiTodoTools(nil)...)
	tools = append(tools, aiWebFetchTool())
	for _, n := range []string{"tests_results", "db_query", "todo_read", "web_fetch"} {
		found := false
		for _, tl := range tools {
			if tl.Name == n {
				found = true
				if tl.View == nil {
					t.Errorf("%s has no View", n)
				}
			}
		}
		if !found {
			t.Errorf("%s not found", n)
		}
	}
}
