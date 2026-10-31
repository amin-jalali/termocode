package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OM-4: skills on demand. The model sees only "name — description" and
// loads a skill's text with skill_load. Project skills are trusted per
// content hash, like project rules; user skills are trusted.

func writeAISkill(t *testing.T, dir, file, name, desc, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// aiSkillsModel is a plain-chat model in a temp workspace with a temp
// config folder (user skills live there).
func aiSkillsModel(t *testing.T, replies ...ai.Chunk) (m *Model, rec *recordingAIProvider, root, userDir string) {
	t.Helper()
	rec = &recordingAIProvider{replies: replies}
	m, root = aiIDETestModel(t, rec) // a temp config folder (withConfigDir)
	return m, rec, root, aiSkillsUserDir()
}

func aiReqHasTool(req ai.Request, name string) bool {
	for _, t := range req.Tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func aiReqText(req ai.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content + "\n")
	}
	return b.String()
}

// Spec: built-in skills are available on first start, without a trust card.
func TestAISkillsBundledFirstStart(t *testing.T) {
	m, rec, _, _ := aiSkillsModel(t)
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if m.confirmOpen || len(rec.reqs) != 1 {
		t.Fatal("card or no request without skills")
	}
	if sys := rec.reqs[0].Messages[0].Content; !strings.Contains(sys, "- debugging —") {
		t.Errorf("skills list without skills:\n%s", sys)
	}
	if !aiReqHasTool(rec.reqs[0], "skill_load") {
		t.Error("skill_load offered without skills")
	}
}

// Spec: a user skill is offered at once (no card): its name and
// description are in the prompt, its body is not, and skill_load is a
// tool.
func TestAISkillsUserSkillInPrompt(t *testing.T) {
	m, rec, _, user := aiSkillsModel(t)
	writeAISkill(t, user, "go.md", "go-tests", "How to run the Go tests", "SECRET-BODY run go test -race")
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if m.confirmOpen || len(rec.reqs) != 1 {
		t.Fatal("a user skill asked for trust")
	}
	sys := rec.reqs[0].Messages[0].Content
	if !strings.Contains(sys, "Skills you can load") || !strings.Contains(sys, "- go-tests — How to run the Go tests") {
		t.Errorf("prompt lacks the skills list:\n%s", sys)
	}
	if strings.Contains(sys, "SECRET-BODY") {
		t.Error("the skill body is in the prompt")
	}
	if !aiReqHasTool(rec.reqs[0], "skill_load") {
		t.Error("no skill_load tool")
	}
}

// Spec: skill_load returns the body (secrets hidden); the chat shows
// "📘 loaded skill: <name>"; an unknown name is an error that lists the
// names.
func TestAISkillLoadTool(t *testing.T) {
	ai.RegisterSecret("om4-registered-secret-77")
	m, rec, root, user := aiSkillsModel(t,
		ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "skill_load", Args: map[string]any{"name": "go-tests"}}}},
		ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c2", Name: "skill_load", Args: map[string]any{"name": "nope"}}}},
		ai.Chunk{Delta: "ok"})
	writeAISkill(t, user, "go.md", "go-tests", "How to run the Go tests", "Run go test -race.\npassword om4-registered-secret-77")
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if len(rec.reqs) != 3 {
		t.Fatalf("requests %d", len(rec.reqs))
	}
	got := aiReqText(rec.reqs[1])
	if !strings.Contains(got, "Run go test -race.") || strings.Contains(got, "om4-registered-secret-77") {
		t.Errorf("tool result:\n%s", got)
	}
	got = aiReqText(rec.reqs[2])
	if !strings.Contains(got, `no skill "nope"`) || !strings.Contains(got, "go-tests") {
		t.Errorf("unknown-name result:\n%s", got)
	}
	tr := aiTranscriptText(m, 100)
	if !strings.Contains(tr, "📘 loaded skill: go-tests") {
		t.Errorf("transcript lacks the skill chip:\n%s", tr)
	}
	_ = root
}

// Spec: project skills ask once per content hash before the first send;
// until trusted they are neither listed nor loadable.
func TestAISkillsProjectTrust(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	pdir := filepath.Join(root, ".termocode", "skills")
	writeAISkill(t, pdir, "db.md", "db-migrate", "How to write a migration", "body")
	m.sendAIChat("hello")
	if !m.confirmOpen || m.confirmKind != confirmKindAISkillsTrust {
		t.Fatal("no skills trust card before the first send")
	}
	if got := m.confirm.SafeID(); got != "deny" {
		t.Errorf("safe answer %q, want deny", got)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Use this project's skills?", "db-migrate", "How to write a migration", "Not in this project"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	if len(rec.reqs) != 0 {
		t.Fatal("sent before the answer")
	}
	driveAIChat(t, m, m.answerAIRulesForTest("deny"), nil)
	if len(rec.reqs) != 1 {
		t.Fatalf("not sent after deny: %d requests", len(rec.reqs))
	}
	if sys := rec.reqs[0].Messages[0].Content; strings.Contains(sys, "db-migrate") {
		t.Errorf("refused project skill offered:\n%s", sys)
	}
	if out, err := aiSkillLoad(root, "db-migrate"); err == nil {
		t.Errorf("refused project skill loaded: %q", out)
	}

	// Use, through the palette's card: offered from the next turn.
	m.openAISkillsTrust()
	if !m.confirmOpen || m.confirmKind != confirmKindAISkillsTrust {
		t.Fatal("the trust row did not open the card")
	}
	m.answerAIRulesForTest("use")
	driveAIChat(t, m, m.sendAIChat("again"), nil)
	if len(rec.reqs) != 2 || !strings.Contains(rec.reqs[1].Messages[0].Content, "- db-migrate — How to write a migration") {
		t.Fatalf("trusted project skill not offered")
	}
	if out, err := aiSkillLoad(root, "db-migrate"); err != nil || !strings.Contains(out, "body") {
		t.Errorf("trusted load %q %v", out, err)
	}

	// A changed file asks again.
	writeAISkill(t, pdir, "db.md", "db-migrate", "How to write a migration", "new body")
	m.sendAIChat("third")
	if !m.confirmOpen || m.confirmKind != confirmKindAISkillsTrust {
		t.Fatal("a changed skill did not ask again")
	}
	// Esc puts the message back.
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if string(m.ai.input) != "third" {
		t.Errorf("Esc: input %q", string(m.ai.input))
	}
}

// Spec: AI: Skills… lists the skills with their source and description,
// Enter opens the file, "New skill…" writes a template and opens it; the
// AI can never run it.
func TestAISkillsPalette(t *testing.T) {
	m, _, root, user := aiSkillsModel(t)
	m.dispatchAIPalette("ai-skills")
	if !m.pickerOpen || m.pickerKind != pickerKindAISkills {
		t.Fatal("no picker without skills (it offers New skill…)")
	}
	up := writeAISkill(t, user, "go.md", "go-tests", "How to run the Go tests", "body")
	if err := os.MkdirAll(filepath.Join(root, ".termocode", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "skills", "bad.md"), []byte("no front matter"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.pickerOpen = false
	m.dispatchAIPalette("ai-skills")
	view := strings.ToLower(ansi.Strip(m.picker.View()))
	for _, want := range []string{"go-tests", "how to run the go tests", "user skills", "new project skill", "bad.md: no front matter"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	m.pickerOpen = false
	var opened []string
	old := aiOpenSkillFile
	aiOpenSkillFile = func(_ *Model, path string, _ int) { opened = append(opened, path) }
	t.Cleanup(func() { aiOpenSkillFile = old })
	m.onAISkillsPicked(aiSkillsOpenPrefix + up)
	if len(opened) != 1 || opened[0] != up {
		t.Errorf("Enter opened %v, want %s", opened, up)
	}
	m.onAISkillsPicked(aiSkillsOpenPrefix + "/etc/passwd")
	if len(opened) != 1 {
		t.Errorf("opened a file that is not a skill: %v", opened)
	}

	m.onAISkillsPicked(aiSkillsNewProjectID)
	p := filepath.Join(root, ".termocode", "skills", "new-skill.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	name, desc, _, err := ai.ParseSkill(string(b), "new-skill.md")
	if err != nil || name != "new-skill" || desc == "" {
		t.Errorf("template does not parse: %q %q %v", name, desc, err)
	}
	m.onAISkillsPicked(aiSkillsNewProjectID)
	if _, err := os.Stat(filepath.Join(root, ".termocode", "skills", "new-skill-2.md")); err != nil {
		t.Errorf("second new skill: %v", err)
	}
	m.onAISkillsPicked(aiSkillsNewUserID)
	if _, err := os.Stat(filepath.Join(user, "new-skill.md")); err != nil {
		t.Errorf("new user skill: %v", err)
	}
	if len(opened) != 4 || opened[1] != p {
		t.Errorf("new skills not opened: %v", opened)
	}

	if p, ok := aiActionPolicies["ai-skills"]; !ok || p.Gate != aiGateNever {
		t.Errorf("ai-skills policy %+v", p)
	}
}

func TestAISkillsBuiltinPreview(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	sk, _ := aiLoadSkills(root).Find("debugging")
	m.w, m.h = 120, 50
	m.openAISkills()
	if !strings.Contains(ansi.Strip(m.picker.View()), "BUILT-IN SKILLS") {
		t.Fatalf("built-ins missing from picker: %s", ansi.Strip(m.picker.View()))
	}
	m.onAISkillsPicked(aiSkillsOpenPrefix + sk.Path)
	if !m.confirmOpen || m.confirmKind != confirmKindBuiltinSkill || !strings.Contains(ansi.Strip(m.confirm.View()), "debugging") {
		t.Fatal("no preview")
	}
}

// Spec (OMX-11): SKILL.md folders in .claude/skills of the project go
// behind the same trust card (the card names the folder); the user's
// ~/.claude/skills are offered at once.
func TestAISkillsOtherAppFolders(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAISkill(t, filepath.Join(home, ".claude", "skills", "mine"), "SKILL.md", "mine", "My own habit", "hi")
	writeAISkill(t, filepath.Join(root, ".claude", "skills", "pdf"), "SKILL.md", "pdf", "Fill PDF forms", "body")
	m.sendAIChat("hello")
	if !m.confirmOpen || m.confirmKind != confirmKindAISkillsTrust {
		t.Fatal("no trust card for a .claude/skills folder")
	}
	box := ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, ".claude/skills · 1 skill") || !strings.Contains(box, "pdf — Fill PDF forms") || strings.Contains(box, "mine") {
		t.Errorf("card:\n%s", box)
	}
	driveAIChat(t, m, m.answerAIRulesForTest("deny"), nil)
	if len(rec.reqs) != 1 {
		t.Fatalf("requests %d", len(rec.reqs))
	}
	sys := rec.reqs[0].Messages[0].Content
	if !strings.Contains(sys, "- mine — My own habit") || strings.Contains(sys, "Fill PDF") {
		t.Errorf("prompt:\n%s", sys)
	}
}
