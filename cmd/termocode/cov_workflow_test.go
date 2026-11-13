package main

import (
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowCLIUsageAndArgumentErrors(t *testing.T) {
	ws := cliWorkspace(t)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 2, "usage"},
		{[]string{"show"}, 2, "usage: termocode workflow show"},
		{[]string{"show", "ghost"}, 1, "no workflow"},
		{[]string{"run"}, 2, "usage: termocode workflow run"},
		{[]string{"run", "docs"}, 3, "start it in the IDE"},
		{[]string{"run", "ghost", "--dry-run"}, 1, "no workflow"},
		{[]string{"logs"}, 2, "usage: termocode workflow logs"},
		{[]string{"logs", "no-such-run"}, 1, "termocode:"},
		{[]string{"again", "docs"}, 2, "usage: termocode workflow again"},
		{[]string{"stop", "x"}, 3, "live run"},
		{[]string{"pause", "x"}, 3, "live run"},
		{[]string{"bogus"}, 2, "usage"},
		{[]string{"list", "--bogus"}, 2, ""},
		{[]string{"run", "docs", "--dry-run", "--var", "novalue"}, 2, ""},
	} {
		code, _, errw := cliRun(ws, c.args...)
		if code != c.code || !strings.Contains(errw, c.want) {
			t.Errorf("%v: code %d err %q", c.args, code, errw)
		}
	}
}

func TestWorkflowCLIListShowJSONAndBroken(t *testing.T) {
	ws := cliWorkspace(t)
	if code, out, _ := cliRun(ws, "list", "--json"); code != 0 || !strings.Contains(out, `"docs"`) {
		t.Errorf("list json: %d %q", code, out)
	}
	if code, out, _ := cliRun(ws, "show", "docs", "--json"); code != 0 || !strings.Contains(out, `"steps"`) {
		t.Errorf("show json: %d %q", code, out)
	}
	if code, out, _ := cliRun(ws, "show", "docs"); code != 0 || !strings.Contains(out, "docs") {
		t.Errorf("show: %d %q", code, out)
	}
	if code, _, errw := cliRun(ws, "show", "bad"); code != 1 || errw == "" {
		t.Errorf("show broken: %d %q", code, errw)
	}
	if code, _, errw := cliRun(ws, "run", "bad", "--dry-run"); code != 1 || errw == "" {
		t.Errorf("run broken: %d %q", code, errw)
	}
	if code, _, _ := cliRun(ws, "validate"); code != 1 {
		t.Errorf("validate with a broken file: %d", code)
	}
	if code, _, errw := cliRun(ws, "validate", filepath.Join(ws, "missing.jsonc")); code != 1 || errw == "" {
		t.Errorf("validate missing: %d %q", code, errw)
	}
}

func TestWorkflowCLIRunsScoresAndAgain(t *testing.T) {
	ws := cliWorkspace(t)
	root, _ := ledger.Root(ws)
	e, _ := wire.Find(wire.Load(ws, "", root), "docs")
	if err := wire.Trust(root, e); err != nil {
		t.Fatal(err)
	}
	if code, _, errw := cliRun(ws, "run", "docs", "--dry-run", "--var", "a=1"); code != 0 {
		t.Fatalf("dry run: %d %q", code, errw)
	}
	if code, out, _ := cliRun(ws, "runs", "--json"); code != 0 || !strings.Contains(out, "[") {
		t.Errorf("runs json: %d %q", code, out)
	}
	if code, out, _ := cliRun(ws, "runs", "docs", "--limit", "1"); code != 0 || out == "" {
		t.Errorf("runs: %d %q", code, out)
	}
	if code, _, _ := cliRun(ws, "scores", "--json"); code != 0 {
		t.Errorf("scores json: %d", code)
	}
	if code, _, _ := cliRun(ws, "scores", "docs"); code != 0 {
		t.Errorf("scores: %d", code)
	}
	if code, _, errw := cliRun(ws, "again", "ghost", "k"); code != 1 || errw == "" {
		t.Errorf("again on a missing workflow: %d %q", code, errw)
	}
	if code, _, _ := cliRun(ws, "again", "docs", "item-1"); code != 0 && code != 1 {
		t.Errorf("again: %d", code)
	}
	if code, _, _ := cliRun(t.TempDir(), "list"); code != 0 {
		t.Errorf("list in an empty project: %d", code)
	}
}

func TestWorkflowPrivateDirAndKVFlags(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	if workflowPrivateDir() != filepath.Join("/x/cfg", "termocode", "workflows") {
		t.Fatal("xdg")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if workflowPrivateDir() != filepath.Join("/h", ".config", "termocode", "workflows") {
		t.Fatal("home")
	}
	t.Setenv("HOME", "")
	if _, err := os.UserHomeDir(); err == nil && workflowPrivateDir() != "" {
		t.Fatal("no home")
	}
	k := kvFlags{}
	if err := k.Set("novalue"); err == nil {
		t.Fatal("want k=v")
	}
	if err := k.Set("=v"); err == nil {
		t.Fatal("empty key")
	}
	if err := k.Set("a=1"); err != nil || k["a"] == nil || k.String() != "" {
		t.Fatal("set")
	}
}

func TestWfWhatPerStepKind(t *testing.T) {
	for _, c := range []struct {
		s    workflow.Step
		want string
	}{
		{workflow.Step{Source: &workflow.SourceSpec{From: "files", Glob: "*.md"}}, "files *.md"},
		{workflow.Step{Write: &workflow.WriteSpec{To: "file", Path: "a.md"}}, "file a.md"},
		{workflow.Step{Write: &workflow.WriteSpec{To: "mcp", Server: "s", Tool: "t"}}, "s t"},
		{workflow.Step{Write: &workflow.WriteSpec{To: "status", Server: "s", Tool: "t"}}, "s t"},
		{workflow.Step{Write: &workflow.WriteSpec{To: "clipboard"}}, "clipboard"},
		{workflow.Step{Each: &workflow.EachSpec{Items: "steps.a.items"}}, "steps.a.items"},
		{workflow.Step{Filter: &workflow.FilterSpec{}, When: "x > 1"}, "x > 1"},
		{workflow.Step{Loop: &workflow.LoopSpec{Max: 3, Until: "done"}}, "max 3 done"},
		{workflow.Step{Branch: &workflow.BranchSpec{Join: "all", Branches: []workflow.Branch{{Name: "a"}, {Name: "b"}}}}, "join all: a, b"},
		{workflow.Step{Wait: &workflow.WaitSpec{For: "merged", PR: "7"}}, "merged 7"},
		{workflow.Step{}, ""},
	} {
		if got := wfWhat(&c.s); got != c.want {
			t.Errorf("%+v: %q, want %q", c.s, got, c.want)
		}
	}
}

func TestWfStateNames(t *testing.T) {
	ws := cliWorkspace(t)
	root, _ := ledger.Root(ws)
	docs, _ := wire.Find(wire.Load(ws, "", root), "docs")
	bad, _ := wire.Find(wire.Load(ws, "", root), "bad")
	if wfState(bad) != "file error" || wfState(docs) != "not trusted" {
		t.Fatalf("%q %q", wfState(bad), wfState(docs))
	}
	if err := wire.Trust(root, docs); err != nil {
		t.Fatal(err)
	}
	docs, _ = wire.Find(wire.Load(ws, "", root), "docs")
	if got := wfState(docs); got != "on" && got != "off" {
		t.Fatalf("trusted state %q", got)
	}
}
