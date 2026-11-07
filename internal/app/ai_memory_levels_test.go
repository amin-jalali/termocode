package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// CMP-73: memory levels. "on" uses and learns, "read" only uses the saved
// lessons (no extractor), "off" and undecided do neither.
func TestAIMemoryLevels(t *testing.T) {
	for _, c := range []struct {
		state       string
		uses, learn bool
	}{{"on", true, true}, {"read", true, false}, {"off", false, false}, {"", false, false}} {
		if aiMemoryUses(c.state) != c.uses || aiMemoryLearns(c.state) != c.learn {
			t.Errorf("%q: uses %v learns %v", c.state, aiMemoryUses(c.state), aiMemoryLearns(c.state))
		}
	}
}

// CMP-73: with memory on "use only", saved lessons still go into the
// prompt, and the kind shows in front of the lesson.
func TestAIMemoryUseOnlyStillInPrompt(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "")
	l := aiTestLesson("2026-11-07", "Not npm", "Use pnpm in this repo.")
	l.Kind = aimemory.KindCorrection
	seedAILessons(t, dir, l)
	setAIMemory("read")
	driveAIChat(t, m, m.sendAIChat("install deps"), nil)
	sys := rec.reqs[0].Messages[0].Content
	if !strings.Contains(sys, "- [correction] Not npm: Use pnpm in this repo.") {
		t.Fatalf("prompt:\n%s", sys)
	}
}

// CMP-73: AI: Memory: Learn New Lessons On/Off flips between "use and
// learn" and "use only"; from off it turns memory on in "use only".
func TestAIMemoryLearnToggle(t *testing.T) {
	m, _, _ := aiRulesModel(t, "")
	setAIMemory("on")
	m.dispatchAIPalette("ai-memory-learn")
	if loadSession().AIMemory != "read" || !strings.Contains(aiToastText(m), "use only") {
		t.Fatalf("state %q toast %q", loadSession().AIMemory, aiToastText(m))
	}
	m.dispatchAIPalette("ai-memory-learn")
	if loadSession().AIMemory != "on" || !strings.Contains(aiToastText(m), "use and learn") {
		t.Fatalf("state %q toast %q", loadSession().AIMemory, aiToastText(m))
	}
	setAIMemory("off")
	m.dispatchAIPalette("ai-memory-learn")
	if loadSession().AIMemory != "read" {
		t.Fatalf("from off: %q", loadSession().AIMemory)
	}
	// The plain toggle turns a "use only" memory off.
	m.dispatchAIPalette("ai-memory-toggle")
	if loadSession().AIMemory != "off" {
		t.Fatalf("toggle from read: %q", loadSession().AIMemory)
	}
	if p, ok := aiActionPolicies["ai-memory-learn"]; !ok || p.Gate != aiGateNever {
		t.Error("the AI may change the memory level")
	}
}

// CMP-73: the MCP server shares the lessons with outside agents as a
// second read-only resource, termocode://memory/lessons (Markdown,
// redacted); with memory off it holds only a note.
func TestMCPServeMemoryResource(t *testing.T) {
	m, _, dir := aiRulesModel(t, "")
	seedAILessons(t, dir,
		aiTestLesson("2026-11-07", "Run vet", "Run go vet before a commit."),
		aiTestLesson("2026-11-07", "Token", "use sk-proj-ABCDEFGHIJKLMNOP1234 for tests"))
	src, hub, audit := mcpServeTestSource(dir, nil)
	defer hub.stop()
	var rs mcp.ResourceSource = src
	found := false
	for _, r := range rs.Resources() {
		found = found || (r.URI == mcpServeMemoryURI && r.MimeType == "text/markdown")
	}
	if !found {
		t.Fatalf("resources = %+v", rs.Resources())
	}
	read := func() string {
		var got string
		mcpServeDrive(t, m, hub, func() (mcp.CallResult, error) {
			var err error
			got, err = src.ReadResource(mcpServeCtx("codex"), mcpServeMemoryURI)
			return mcp.CallResult{}, err
		})
		return got
	}
	setAIMemory("off")
	if got := read(); !strings.Contains(got, "Memory is off") || strings.Contains(got, "go vet") {
		t.Fatalf("off: %s", got)
	}
	setAIMemory("read")
	got := read()
	if !strings.Contains(got, "Run vet") || !strings.Contains(got, "Run go vet before a commit.") ||
		strings.Contains(got, "sk-proj-ABCDEFGHIJKLMNOP1234") {
		t.Fatalf("on: %s", got)
	}
	if !strings.Contains(audit.all(), "resources/read") {
		t.Fatalf("audit = %q", audit.all())
	}
	if _, err := src.ReadResource(context.Background(), "termocode://memory/other"); err == nil {
		t.Fatal("unknown memory uri read")
	}
}
