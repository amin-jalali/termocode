package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/aimemory"
)

// OM-2: memory, lessons across sessions. Off by default; on per
// workspace; lessons go into later prompts of the same workspace.

// seedAILessons writes lessons.md in dir.
func seedAILessons(t *testing.T, dir string, ls ...aimemory.Lesson) {
	t.Helper()
	if err := aimemory.Save(dir, ls); err != nil {
		t.Fatal(err)
	}
}

func aiTestLesson(date, title, text string, paths ...string) aimemory.Lesson {
	return aimemory.Lesson{ID: aimemory.NewID(title, text), Date: date, Title: title, Text: text, Paths: paths}
}

func setAIMemory(state string) {
	sess := loadSession()
	sess.AIMemory = state
	saveSession(sess)
}

// Spec: memory is off by default: lessons.md in the repo is not read
// into the prompt and the chat shows no memory line.
func TestAIMemoryOffByDefaultNoPromptBlock(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "")
	seedAILessons(t, dir, aiTestLesson("2026-10-24", "Run go vet", "Always run go vet first."))
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if len(rec.reqs) != 1 {
		t.Fatalf("requests %d", len(rec.reqs))
	}
	if sys := rec.reqs[0].Messages[0].Content; strings.Contains(sys, "Lessons you wrote") || strings.Contains(sys, "go vet") {
		t.Errorf("lessons in the prompt while memory is off:\n%s", sys)
	}
	if got := aiTranscriptText(m, 100); strings.Contains(got, "memory:") {
		t.Errorf("memory line while off:\n%s", got)
	}
	if m.ai.conv.Messages[0].Lessons != 0 {
		t.Error("lessons counted while off")
	}
}

// Spec: with memory on, the lesson about the open file is in the system
// prompt (after the rules block); with two lessons both fit the newest-3
// rule, so the unrelated one is there too, after it. The user message
// says "memory: 2 lessons".
func TestAIMemoryLessonsInPrompt(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "Rule one.\n")
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	seedAILessons(t, dir,
		aiTestLesson("2026-10-23", "Unrelated note", "The docs build with make docs."),
		aiTestLesson("2026-10-24", "a.go is generated", "Edit gen.go instead of a.go.", "a.go"))
	setAIMemory("on")
	if err := m.editor.Open(filepath.Join(dir, "a.go")); err != nil {
		t.Fatal(err)
	}
	trustAIRules(t, m, "hello")
	sys := rec.reqs[0].Messages[0].Content
	i, j, r := strings.Index(sys, "a.go is generated: Edit gen.go instead of a.go."), strings.Index(sys, "Unrelated note"), strings.Index(sys, "Project rules")
	if i < 0 || j < i || r < 0 || i < r || !strings.Contains(sys, "Lessons you wrote in earlier sessions in this repository") {
		t.Fatalf("prompt order rules %d, path lesson %d, other %d:\n%s", r, i, j, sys)
	}
	if strings.Contains(lastUserContent(rec.reqs[0]), "gen.go") {
		t.Error("lessons leaked into the user message")
	}
	if got := m.ai.conv.Messages[0].Lessons; got != 2 {
		t.Errorf("lessons on the user message %d", got)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "rules: AGENTS.md (10 B) · memory: 2 lessons") {
		t.Errorf("transcript lacks the memory line:\n%s", got)
	}
}

// Spec: lessons are redacted on the way into the prompt (a hand-edited
// file may hold a secret).
func TestAIMemoryPromptRedacted(t *testing.T) {
	m, rec, dir := aiRulesModel(t, "")
	seedAILessons(t, dir, aiTestLesson("2026-10-24", "Token", "use sk-proj-ABCDEFGHIJKLMNOP1234 for tests"))
	setAIMemory("on")
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	sys := rec.reqs[0].Messages[0].Content
	if strings.Contains(sys, "sk-proj-ABCDEFGHIJKLMNOP1234") || !strings.Contains(sys, "[REDACTED]") {
		t.Errorf("prompt:\n%s", sys)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "memory: 1 lesson") {
		t.Errorf("transcript lacks the memory line:\n%s", got)
	}
}

// Spec: AI: Memory: Turn On/Off flips memory for this workspace. Turning
// it on adds .termocode/memory/ to .gitignore exactly once (the old lines
// stay byte-equal) and the toast says so; the AI can never run it.
func TestAIMemoryToggle(t *testing.T) {
	m, _, dir := aiRulesModel(t, "")
	old := "bin/\n*.log\n"
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(old), 0o644)
	m.dispatchAIPalette("ai-memory-toggle")
	if loadSession().AIMemory != "on" {
		t.Fatal("not on")
	}
	toast := aiToastText(m)
	if !strings.Contains(toast, "Memory is on") || !strings.Contains(toast, ".termocode/memory/lessons.md") || !strings.Contains(toast, "added .termocode/memory/ to .gitignore") {
		t.Errorf("toast %q", toast)
	}
	m.dispatchAIPalette("ai-memory-toggle")
	if loadSession().AIMemory != "off" || !strings.Contains(aiToastText(m), "Memory is off") {
		t.Fatalf("not off: %q", aiToastText(m))
	}
	m.dispatchAIPalette("ai-memory-toggle")
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != old+".termocode/memory/\n" {
		t.Errorf(".gitignore %q", b)
	}
	if !strings.Contains(aiToastText(m), "already in .gitignore") {
		t.Errorf("second turn-on toast %q", aiToastText(m))
	}
	for _, id := range []string{"ai-memory-toggle"} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v", id, p)
		}
	}
	// No .gitignore: it is made.
	m2, _, dir2 := aiRulesModel(t, "")
	m2.dispatchAIPalette("ai-memory-toggle")
	if b, _ := os.ReadFile(filepath.Join(dir2, ".gitignore")); string(b) != ".termocode/memory/\n" {
		t.Errorf("made .gitignore %q", b)
	}
	if !strings.Contains(aiToastText(m2), "made .gitignore") {
		t.Errorf("toast %q", aiToastText(m2))
	}
}
