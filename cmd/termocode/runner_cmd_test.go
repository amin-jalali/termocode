package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/runner"
)

// CMP-12: `termocode runner add|list|show|cancel` work on the queue
// folder; `runner serve` is the loop that takes the tasks.
func TestRunRunnerCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runner")
	folder := t.TempDir()
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runRunnerIn(args, strings.NewReader(stdin), &out, &errw, func() (string, error) { return folder, nil }, func() (string, error) { return dir, nil })
		return code, out.String(), errw.String()
	}
	if code, _, e := run(""); code != 2 || !strings.Contains(e, "runner serve") {
		t.Errorf("no args: %d %q", code, e)
	}
	code, out, e := run("", "add", "--agent", "codex", "--read-only", "explain", "the", "parser")
	if code != 0 || e != "" {
		t.Fatalf("add: %d %q", code, e)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatal("add printed no id")
	}
	if code, _, _ := run("fix it from stdin\n", "add", "-"); code != 0 {
		t.Errorf("add from stdin: %d", code)
	}
	if code, _, e := run("", "add", "--agent", "bash", "x"); code == 0 || !strings.Contains(e, "agent") {
		t.Errorf("bad agent: %d %q", code, e)
	}
	if code, _, e := run("", "add", "--folder", "rel", "x"); code == 0 || e == "" {
		t.Errorf("relative folder: %d %q", code, e)
	}

	code, out, _ = run("", "list", "--json")
	var tasks []runner.Task
	if code != 0 || json.Unmarshal([]byte(out), &tasks) != nil || len(tasks) != 2 {
		t.Fatalf("list --json: %d %q", code, out)
	}
	if tasks[0].ID != id || tasks[0].Agent != "codex" || !tasks[0].ReadOnly || tasks[0].Folder != folder || tasks[0].Prompt != "explain the parser" {
		t.Errorf("first task %+v", tasks[0])
	}
	if tasks[1].Prompt != "fix it from stdin" || tasks[1].Agent != "claude" || tasks[1].ReadOnly {
		t.Errorf("stdin task %+v", tasks[1])
	}
	if code, out, _ := run("", "list"); code != 0 || !strings.Contains(out, "STATE") || !strings.Contains(out, id) || !strings.Contains(out, "queued") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, out, _ := run("", "show", id); code != 0 || !strings.Contains(out, "explain the parser") || !strings.Contains(out, "read-only") {
		t.Errorf("show: %d %q", code, out)
	}
	if code, out, _ := run("", "cancel", id); code != 0 || !strings.Contains(out, "canceled") {
		t.Errorf("cancel: %d %q", code, out)
	}
	if code, _, e := run("", "show", "nope"); code != 1 || e == "" {
		t.Errorf("show unknown: %d %q", code, e)
	}
	if code, _, e := run("", "serve", "--pool", "x"); code != 2 || !strings.Contains(e, "pool") {
		t.Errorf("bad pool: %d %q", code, e)
	}
}

func TestRunnerIsNoTUICommand(t *testing.T) {
	if tuiCommand([]string{"runner", "list"}) {
		t.Error("termocode runner would hold the GC for a UI frame")
	}
}
