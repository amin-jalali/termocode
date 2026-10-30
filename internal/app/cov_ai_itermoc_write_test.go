package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// The Itermoc write tools' checks and card texts
// (ai_itermoc_write_tools.go): arguments in every JSON shape, the
// refusals and what the cards say.

func TestCovAIDraftArgs(t *testing.T) {
	if got := aiDraftStrings([]string{"a", "b"}); strings.Join(got, ",") != "a,b" {
		t.Errorf("[]string = %v", got)
	}
	if got := aiDraftStrings([]any{" a ", 3, ""}); strings.Join(got, ",") != "a" {
		t.Errorf("[]any = %v", got)
	}
	for in, want := range map[any]float64{2: 2, "$1.5": 1.5, 0.25: 0.25} {
		if got, ok := aiDraftNumber(in); !ok || got != want {
			t.Errorf("number %v = %v %v", in, got, ok)
		}
	}
	if _, ok := aiDraftNumber("lots"); ok {
		t.Error("lots is a number")
	}
	if _, ok := aiDraftNumber(true); ok {
		t.Error("true is a number")
	}
	if _, err := aiDraftProfile(map[string]any{"name": "x", "budget_usd": "much"}); err == nil || !strings.Contains(err.Error(), "budget_usd") {
		t.Errorf("bad budget: %v", err)
	}
	if _, err := aiDraftProfile(map[string]any{"name": "x"}); err == nil || !strings.Contains(err.Error(), "steps is required") {
		t.Errorf("no steps: %v", err)
	}
	if _, err := aiDraftProfile(map[string]any{"name": "x", "steps": []any{"text"}}); err == nil || !strings.Contains(err.Error(), "step 1 is not an object") {
		t.Errorf("bad step: %v", err)
	}
	p, err := aiDraftProfile(map[string]any{"name": "x", "budget_usd": 2, "categories": map[string]any{"quick": "fast", "deep": " ", "x": 3},
		"steps": []any{map[string]any{"id": "plan", "prompt": "Plan it.\nMore.", "planner": true}, map[string]any{"title": "Do", "after": []any{"plan"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Categories) != 1 || p.Categories["quick"] != "fast" || !p.BudgetSet {
		t.Errorf("profile = %+v", p)
	}
	lines := strings.Join(aiDraftLines(p), "\n")
	covAIMustContain(t, "draft lines", lines, "budget: $2", "quick steps use fast", "1 · plan · ", " · planner", "    Plan it.", "2 · Do · ", " · after plan")
}

func TestCovAIProfileSwitchChecks(t *testing.T) {
	sn := aiItermocWriteSnap{profiles: map[string]string{"review": ai.ProfileSourceBuiltin, "house": ai.ProfileSourceProject}, active: "review"}
	for args, want := range map[string]string{"": "name is required", "nope": `there is no profile "nope"`, "review": "already the active profile"} {
		if err := sn.checkSwitch(map[string]any{"name": args}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", args, err)
		}
	}
	if err := sn.checkSwitch(map[string]any{"name": "house"}); err != nil {
		t.Errorf("house: %v", err)
	}
	if got := sn.describeSwitch(map[string]any{"name": "house"}); !strings.Contains(got, "asks you to trust it first") {
		t.Errorf("project card = %q", got)
	}
	if got := sn.describeSwitch(map[string]any{"name": "x", "note": "sk-ant-api03-" + strings.Repeat("a", 40)}); got != aiSecretCardText {
		t.Errorf("secret card = %q", got)
	}
	m := aiTestModel(t)
	if _, _, err := m.aiToolProfileSwitch("no-such"); err == nil {
		t.Error("switch to a missing profile")
	}
}

func TestCovAISlotTexts(t *testing.T) {
	for kind, want := range map[string]string{ai.KindOllama: "needs no key", ai.KindCompatible: "If the server needs a key",
		ai.KindClaudeOAuth: "Claude sign-in", ai.KindOpenAI: "an OpenAI key"} {
		if got := aiSlotKeyText(kind); !strings.Contains(got, want) {
			t.Errorf("%s: %q", kind, got)
		}
	}
	if got := aiSlotWhat(ai.ModelSlot{Provider: ai.KindOpenAI}); !strings.Contains(got, "(default)") {
		t.Errorf("default model = %q", got)
	}
	if got := aiSlotWhat(ai.ModelSlot{Provider: ai.KindCompatible, Model: "m", BaseURL: "http://127.0.0.1:8080/v1"}); !strings.Contains(got, "127.0.0.1:8080") {
		t.Errorf("host = %q", got)
	}
	sn := aiItermocWriteSnap{slots: map[string]bool{}, userSlots: map[string]ai.ModelSlot{"fast": {Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}}
	if err := sn.checkSlot(map[string]any{"name": "fast", "provider": ai.KindCompatible, "model": "m", "base_url": "http://127.0.0.1:1/v1?key=x"}); err == nil || !strings.Contains(err.Error(), "query or fragment") {
		t.Errorf("query in base_url: %v", err)
	}
	if got := sn.describeSlot(map[string]any{"name": "fast", "provider": ai.KindOpenAI, "model": "gpt-5"}); !strings.Contains(got, "Change model slot fast") || !strings.Contains(got, "- openai · gpt-5-mini") {
		t.Errorf("change card = %q", got)
	}
	if got := sn.describeSlot(map[string]any{"name": "main", "provider": ai.KindOpenAI}); !strings.HasPrefix(got, "Model slot draft (invalid: ") {
		t.Errorf("main card = %q", got)
	}
	m := aiTestModel(t)
	m.connectOpen = true
	if _, _, err := m.aiToolSlotDraft("fast", ai.ModelSlot{Provider: ai.KindOpenAI}); err == nil {
		t.Error("a slot draft over an open card")
	}
}

func TestCovAIServiceAndAgentChecks(t *testing.T) {
	aiTestModel(t)
	if err := checkServiceConnect(map[string]any{}); err == nil || err.Error() != "service is required" {
		t.Errorf("no service: %v", err)
	}
	if err := checkServiceConnect(map[string]any{"service": "nope"}); err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Errorf("unknown: %v", err)
	}
	if got := describeServiceConnect(map[string]any{"service": "github"}); !strings.Contains(got, "GitHub") {
		t.Errorf("connect card = %q", got)
	}
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly"},"plain":{"type":"http","url":"https://x.example/mcp"}}}`)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "github"}, "writes is required"},
		{map[string]any{"writes": true}, "name is required"},
		{map[string]any{"name": "plain", "writes": true}, "has no read-only switch"},
		{map[string]any{"name": "github", "writes": false}, "is already read only"},
	} {
		if err := checkServiceAccess(c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.args, err)
		}
	}
	if got := describeServiceAccess(map[string]any{"name": "github", "writes": false}); !strings.HasPrefix(got, "Make github read only?") {
		t.Errorf("read only card = %q", got)
	}
	if err := checkAgentStart(map[string]any{"agent": "nope"}); err == nil || !strings.Contains(err.Error(), "agent must be one of") {
		t.Errorf("unknown agent: %v", err)
	}
	if got := describeAgentStart(map[string]any{"agent": "nope"}); got != "Start an outside agent (invalid)" {
		t.Errorf("invalid agent card = %q", got)
	}
	if _, err := aiStartOutsideAgent(&Model{}, agents.Registry[0]); err == nil {
		t.Error("an agent started without a terminal")
	}
}
