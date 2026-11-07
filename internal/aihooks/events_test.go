package aihooks

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Spec (CMP-59): one file holds hooks of every event; each hook knows
// its event; post_tool stays apart; an unknown event or a bad pattern
// is an error; "$schema" style keys are skipped.
func TestParseEvents(t *testing.T) {
	hs, err := Parse([]byte(`{"$schema":"x","pre_tool":[{"tools":["mcp__github__*"],"run":"guard"}],
		"post_tool":[{"run":"fmt"}],"file_save":[{"files":["*.go"],"run":"vet"}],
		"stop":[{"run":"check"}],"pre_compact":[{"run":"a"}],"post_compact":[{"run":"b"}]}`), SourceUser)
	if err != nil || len(hs) != 6 {
		t.Fatalf("parse %+v %v", hs, err)
	}
	for i, ev := range Events {
		if hs[i].Event != ev || hs[i].Source != SourceUser {
			t.Errorf("hook %d: %+v, want event %s", i, hs[i], ev)
		}
	}
	if !hs[0].Matches("mcp__github__create_issue") || hs[0].Matches("mcp__slack__post") || hs[0].Matches("run_command") {
		t.Error("MCP glob matching")
	}
	fs := Of(hs, EventFileSave)[0]
	if !fs.MatchesFile("internal/app/a.go") || !fs.MatchesFile("a.go") || fs.MatchesFile("README.md") {
		t.Error("file globs")
	}
	if _, err := Parse([]byte(`{"pre_save":[{"run":"x"}]}`), SourceUser); err == nil || !strings.Contains(err.Error(), "unknown event") {
		t.Errorf("unknown event: %v", err)
	}
	if _, err := Parse([]byte(`{"pre_tool":[{"tools":["[a"],"run":"x"}]}`), SourceUser); err == nil {
		t.Error("bad pattern accepted")
	}
	root, cfg := t.TempDir(), t.TempDir()
	user := filepath.Join(cfg, "hooks.json")
	writeFile(t, user, `{"post_tool":[{"run":"a"}],"stop":[{"run":"b"}]}`)
	writeFile(t, filepath.Join(root, ".termocode", "hooks.json"), `{"pre_tool":[{"run":"c"}]}`)
	s := Load(root, user)
	if len(s.PostTool) != 1 || len(s.Other) != 2 || len(s.All()) != 3 || len(s.Project()) != 1 || s.ProjectHash == "" {
		t.Fatalf("set %+v", s)
	}
}

// Spec (CMP-59): exit 2 blocks with the output as reason; exit 0 with
// a JSON object denies, allows or changes the arguments; any other exit
// or a start error decides nothing.
func TestDecide(t *testing.T) {
	if d := Decide(Result{Exit: 2, Output: "no rm -rf"}); !d.Block || d.Reason != "no rm -rf" {
		t.Errorf("exit 2: %+v", d)
	}
	if d := Decide(Result{Exit: 2}); !d.Block || d.Reason == "" {
		t.Errorf("exit 2 without output: %+v", d)
	}
	if d := Decide(Result{Exit: 1, raw: `{"decision":"deny"}`}); d.Block || d.Allow {
		t.Errorf("a failed hook decided: %+v", d)
	}
	if d := Decide(Result{Exit: 0, raw: `{"decision":"deny","reason":"not on main","args":{"x":1}}`}); !d.Block || d.Reason != "not on main" || d.Args != nil {
		t.Errorf("deny: %+v", d)
	}
	if d := Decide(Result{Exit: 0, raw: `{"decision":"allow","args":{"command":"ls -la"}}`}); !d.Allow || d.Block || d.Args["command"] != "ls -la" {
		t.Errorf("allow + change: %+v", d)
	}
	if d := Decide(Result{Exit: 0, raw: "formatted 3 files"}); d.Block || d.Allow || d.Args != nil {
		t.Errorf("plain output decided: %+v", d)
	}
	if d := Decide(Result{Exit: -1, Err: context.DeadlineExceeded}); d.Block {
		t.Errorf("timeout blocked: %+v", d)
	}
}

// Spec (CMP-59): a hook reads the event as JSON on stdin and its event
// in TERMOCODE_HOOK_EVENT; Decide sees the raw (not redacted) answer.
func TestRunInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	root := t.TempDir()
	h := Hook{Run: `echo "$TERMOCODE_HOOK_EVENT" >&2; cat`, Event: EventPreTool}
	r := Run(context.Background(), h, Call{Tool: "run_command", Root: root, Args: map[string]any{"command": "make"}})
	if r.Exit != 0 || !strings.Contains(r.Output, "pre_tool") || !strings.Contains(r.Output, `"event":"pre_tool"`) ||
		!strings.Contains(r.Output, `"tool":"run_command"`) || !strings.Contains(r.Output, `"command":"make"`) {
		t.Fatalf("input %+v", r)
	}
	h = Hook{Run: `echo '{"decision":"allow","args":{"command":"make test"}}'`, Event: EventPreTool}
	if d := Decide(Run(context.Background(), h, Call{Root: root})); !d.Allow || d.Args["command"] != "make test" {
		t.Errorf("decision from a run: %+v", d)
	}
	r = Run(context.Background(), Hook{Run: `cat`, Event: EventPostCompact}, Call{Root: root, Summary: "the summary"})
	if !strings.Contains(r.Output, `"summary":"the summary"`) || !strings.Contains(r.Output, `"event":"post_compact"`) {
		t.Errorf("post_compact input %q", r.Output)
	}
}
