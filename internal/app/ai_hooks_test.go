package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

func writeHooksFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (OMX-12): a user hook runs after a matching tool call that
// worked, sees the call's path, and writes one audit line; a failed hook
// adds its output to the tool result; other tools and failed calls run
// no hook.
func TestAIHooksRunAfterTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh hooks")
	}
	m, _, root, _ := aiSkillsModel(t)
	m.output = newOutputStore()
	mark := filepath.Join(t.TempDir(), "ran")
	writeHooksFile(t, aiHooksUserFile(), `{"post_tool":[{"tools":["write_file"],"run":"echo \"$TERMOCODE_TOOL_PATH\" >> `+mark+`; echo lint: bad; exit 1"}]}`)
	ok := func(context.Context, map[string]any) (string, error) { return "wrote", nil }
	tools := aiWithHooks(root, []ai.Tool{{Name: "write_file", Run: ok}, {Name: "read_file", Run: ok}}, m.output)
	res, err := tools[0].Run(context.Background(), map[string]any{"path": "a.go"})
	if err != nil || !strings.Contains(res, "wrote") || !strings.Contains(res, "lint: bad") || !strings.Contains(res, "exit 1") {
		t.Fatalf("result %q %v", res, err)
	}
	if b, _ := os.ReadFile(mark); strings.TrimSpace(string(b)) != filepath.Join(root, "a.go") {
		t.Errorf("hook saw path %q", b)
	}
	if res, _ := tools[1].Run(context.Background(), nil); res != "wrote" {
		t.Errorf("read_file got a hook: %q", res)
	}
	audit := strings.Join(m.output.Lines(aiAuditChannel), "\n")
	if !strings.Contains(audit, "hook  after=write_file") || !strings.Contains(audit, "by=user") || !strings.Contains(audit, "exit 1") {
		t.Errorf("audit:\n%s", audit)
	}
	if n := strings.Count(audit, "hook  after="); n != 1 {
		t.Errorf("%d hook lines", n)
	}
}

// Spec (OMX-12): a project hooks file asks once (Deny first) before the
// first message; refused hooks never run; Esc puts the message back.
func TestAIHooksProjectTrust(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	writeHooksFile(t, filepath.Join(root, ".termocode", "hooks.json"), `{"post_tool":[{"tools":["write_file"],"run":"gofmt -w \"$TERMOCODE_TOOL_PATH\""}]}`)
	m.sendAIChat("hello")
	if !m.confirmOpen || m.confirmKind != confirmKindAIHooksTrust || m.confirm.SafeID() != "deny" {
		t.Fatal("no Deny-first hooks card before the first send")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Run this project's hooks?", "after write_file: gofmt -w", "Not in this project"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	driveAIChat(t, m, m.answerAIRulesForTest("deny"), nil)
	if len(rec.reqs) != 1 || len(aiActiveHooks(root)) != 0 {
		t.Fatalf("deny: %d reqs, %d hooks", len(rec.reqs), len(aiActiveHooks(root)))
	}
	m.openAIHooks()
	if m.confirmKind != confirmKindAIHooksTrust {
		t.Fatal("AI: Hooks… did not open the card")
	}
	m.answerAIRulesForTest("use")
	if len(aiActiveHooks(root)) != 1 {
		t.Fatal("trusted hook not active")
	}
	writeHooksFile(t, filepath.Join(root, ".termocode", "hooks.json"), `{"post_tool":[{"run":"rm -rf /"}]}`)
	if len(aiActiveHooks(root)) != 0 {
		t.Fatal("a changed hooks file runs without a new answer")
	}
	m.sendAIChat("again")
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if string(m.ai.input) != "again" {
		t.Errorf("Esc: input %q", string(m.ai.input))
	}
}
