package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aihooks"
)

func hooksOf(t *testing.T, text, source string) []aihooks.Hook {
	t.Helper()
	hs, err := aihooks.Parse([]byte(text), source)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

// Spec (CMP-59): pre_tool hooks run in order for matching tools (MCP
// globs too); exit 2 blocks with the reason; a changed argument set
// goes to the next hook; "allow" counts only from the user's own hooks;
// each run is an audit line.
func TestAIPreToolHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh hooks")
	}
	root := t.TempDir()
	out := newOutputStore()
	hs := hooksOf(t, `{"pre_tool":[
		{"tools":["mcp__github__*"],"run":"echo no github writes; exit 2"},
		{"tools":["run_command"],"run":"echo '{\"decision\":\"allow\",\"args\":{\"command\":\"make test\"}}'"},
		{"tools":["run_command"],"run":"cat > `+filepath.Join(root, "seen")+`"}]}`, aihooks.SourceUser)
	pre := aiPreToolHooks(root, hs, out)
	if pre == nil {
		t.Fatal("no PreTool")
	}
	v := pre(context.Background(), ai.Tool{Name: "mcp__github__create_issue"}, map[string]any{})
	if v.Block != "no github writes" {
		t.Fatalf("MCP block %+v", v)
	}
	v = pre(context.Background(), ai.Tool{Name: "run_command"}, map[string]any{"command": "make"})
	if v.Block != "" || !v.Allow || v.Args["command"] != "make test" {
		t.Fatalf("allow+change %+v", v)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "seen")); !strings.Contains(string(b), `"command":"make test"`) {
		t.Errorf("the next hook saw %s", b)
	}
	if v := pre(context.Background(), ai.Tool{Name: "read_file"}, nil); v.Block != "" || v.Allow || v.Args != nil {
		t.Errorf("no match decided %+v", v)
	}
	audit := strings.Join(out.Lines(aiAuditChannel), "\n")
	if !strings.Contains(audit, "hook  before=mcp__github__create_issue") || !strings.Contains(audit, "blocked: no github writes") || !strings.Contains(audit, "changed args, allow") {
		t.Errorf("audit:\n%s", audit)
	}
	proj := aiPreToolHooks(root, hooksOf(t, `{"pre_tool":[{"run":"echo '{\"decision\":\"allow\"}'"}]}`, aihooks.SourceProject), out)
	if v := proj(context.Background(), ai.Tool{Name: "run_command"}, nil); v.Allow {
		t.Error("a project hook approved a call")
	}
	if aiPreToolHooks(root, hooksOf(t, `{"post_tool":[{"run":"x"}]}`, aihooks.SourceUser), out) != nil {
		t.Error("PreTool without pre_tool hooks")
	}
}

// Spec (CMP-59): a blocking pre_compact hook keeps the full chat (no
// summary call); post_compact hooks get the summary; stop hooks return
// the reason to go on; a stopped run is not pushed past aiStopHookMax.
func TestAICompactAndStopHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh hooks")
	}
	root := t.TempDir()
	out := newOutputStore()
	calls := 0
	compact := func(context.Context, []ai.Message) (string, error) { calls++; return "SUM", nil }
	if aiWithCompactHooks(root, nil, out, nil) != nil {
		t.Error("nil compact must stay nil")
	}
	blocked := aiWithCompactHooks(root, hooksOf(t, `{"pre_compact":[{"run":"echo keep it; exit 2"}]}`, aihooks.SourceUser), out, compact)
	if s, err := blocked(context.Background(), nil); err == nil || s != "" || calls != 0 || !strings.Contains(err.Error(), "keep it") {
		t.Fatalf("blocked compact %q %v calls=%d", s, err, calls)
	}
	mark := filepath.Join(root, "sum")
	post := aiWithCompactHooks(root, hooksOf(t, `{"post_compact":[{"run":"cat > `+mark+`"}]}`, aihooks.SourceUser), out, compact)
	if s, err := post(context.Background(), nil); s != "SUM" || err != nil || calls != 1 {
		t.Fatalf("compact %q %v", s, err)
	}
	if b, _ := os.ReadFile(mark); !strings.Contains(string(b), `"summary":"SUM"`) {
		t.Errorf("post_compact saw %s", b)
	}
	stop := hooksOf(t, `{"stop":[{"run":"exit 0"},{"run":"echo tests still fail; exit 2"}]}`, aihooks.SourceUser)
	if why := aiRunStopHooks(root, stop, out); why != "tests still fail" {
		t.Errorf("stop reason %q", why)
	}
	if why := aiRunStopHooks(root, hooksOf(t, `{"stop":[{"run":"exit 1"}]}`, aihooks.SourceUser), out); why != "" {
		t.Errorf("a failed stop hook pushed: %q", why)
	}
	if txt := aiStopHookText("x"); !strings.Contains(txt, "stop hook") || !strings.Contains(txt, "x") {
		t.Errorf("text %q", txt)
	}
	audit := strings.Join(out.Lines(aiAuditChannel), "\n")
	for _, want := range []string{"on=pre_compact", "on=post_compact", "on=stop"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit misses %s:\n%s", want, audit)
		}
	}
}

// Spec (CMP-59): file_save hooks match the workspace path or the base
// name; other events and files do not match.
func TestAIFileSaveHookFiles(t *testing.T) {
	root := t.TempDir()
	hs := hooksOf(t, `{"file_save":[{"files":["*.go"],"run":"vet"},{"files":["docs/*"],"run":"lint"},{"run":"all"}],"post_tool":[{"run":"x"}]}`, aihooks.SourceUser)
	runs := func(p string) string {
		var r []string
		for _, h := range aiFileSaveHookFiles(root, p, hs) {
			r = append(r, h.Run)
		}
		return strings.Join(r, ",")
	}
	if got := runs(filepath.Join(root, "internal", "a.go")); got != "vet,all" {
		t.Errorf("go file: %s", got)
	}
	if got := runs(filepath.Join(root, "docs", "x.md")); got != "lint,all" {
		t.Errorf("doc: %s", got)
	}
	if got := aiHookWhen(hs[0]); got != "on save *.go" {
		t.Errorf("when %q", got)
	}
}
