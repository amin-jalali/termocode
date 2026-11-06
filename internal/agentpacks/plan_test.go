package agentpacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (OMX-46): for Claude Code every part goes under ~/.claude/ at its
// pack path; the plan marks each file as added, changed (the user's own
// file is replaced) or the same (not written), changed files first.
func TestPlanInstallClaude(t *testing.T) {
	p, _ := Load(samplePack(t))
	home, state := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".claude", "agents", "go-reviewer.md"), "my own reviewer\n")
	write(t, filepath.Join(home, ".claude", "rules", "golang", "style.md"), "Use gofmt.\n")
	pl, err := PlanInstall(p, "claude", home, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Join(pl.Lines(), "\n")
	want := "~ ~/.claude/agents/go-reviewer.md   (replaces your file)\n" +
		"+ ~/.claude/commands/plan.md\n" +
		"+ ~/.claude/skills/tdd/SKILL.md\n" +
		"+ ~/.claude/skills/tdd/scripts/run.sh"
	if lines != want {
		t.Errorf("lines\n%s\nwant\n%s", lines, want)
	}
	if pl.Writes() != 4 || pl.Same() != 1 {
		t.Errorf("writes %d same %d", pl.Writes(), pl.Same())
	}
	if pl.Always <= 0 || pl.OnUse <= 0 {
		t.Errorf("tokens %d %d", pl.Always, pl.OnUse)
	}
}

// Spec (OMX-46): Codex gets skills under ~/.codex/skills/ and commands as
// prompts under ~/.codex/prompts/; it has no agent or rule files, so
// those are skipped and counted for the card.
func TestPlanInstallCodex(t *testing.T) {
	p, _ := Load(samplePack(t))
	pl, err := PlanInstall(p, "codex", t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Join(pl.Lines(), "\n")
	want := "+ ~/.codex/prompts/plan.md\n+ ~/.codex/skills/tdd/SKILL.md\n+ ~/.codex/skills/tdd/scripts/run.sh"
	if lines != want {
		t.Errorf("lines\n%s\nwant\n%s", lines, want)
	}
	if pl.Skipped[KindAgent] != 1 || pl.Skipped[KindRule] != 1 {
		t.Errorf("skipped %v", pl.Skipped)
	}
	if _, err := PlanInstall(p, "vim", t.TempDir(), t.TempDir(), nil); err == nil {
		t.Error("an unknown target must be refused")
	}
}

// Spec (OMX-46): the user can install only some parts; the cost then
// counts only those parts.
func TestPlanInstallOnlySomeParts(t *testing.T) {
	p, _ := Load(samplePack(t))
	pl, err := PlanInstall(p, "claude", t.TempDir(), t.TempDir(), []Kind{KindAgent})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pl.Lines(), "\n"); got != "+ ~/.claude/agents/go-reviewer.md" {
		t.Errorf("lines %s", got)
	}
	if a, u := p.Tokens([]Kind{KindAgent}); pl.Always != a || pl.OnUse != u {
		t.Errorf("tokens %d %d want %d %d", pl.Always, pl.OnUse, a, u)
	}
	if _, err := PlanInstall(p, "claude", t.TempDir(), t.TempDir(), []Kind{}); err == nil {
		t.Error("a plan with no parts must be refused")
	}
}

// Spec (OMX-46): Apply writes the files, keeps the user's old file, and
// Undo puts it back and removes the added files (and folders it made);
// a file the user changed after the install is kept, never lost.
func TestApplyAndUndo(t *testing.T) {
	p, _ := Load(samplePack(t))
	home, state := t.TempDir(), t.TempDir()
	mine := filepath.Join(home, ".claude", "agents", "go-reviewer.md")
	write(t, mine, "my own reviewer\n")
	pl, _ := PlanInstall(p, "claude", home, state, nil)
	rec, err := Apply(pl, state)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mine); !strings.Contains(string(b), "You review Go code") {
		t.Fatalf("not written: %s", b)
	}
	plan := filepath.Join(home, ".claude", "commands", "plan.md")
	write(t, plan, "I changed this after the install\n")

	list, err := Installed(state)
	if err != nil || len(list) != 1 || list[0].Name != "demo-pack" || list[0].Version != "1.2.0" || list[0].Target != "claude" || list[0].ID != rec.ID {
		t.Fatalf("installed %+v %v", list, err)
	}
	undo := strings.Join(list[0].UndoLines(), "\n")
	for _, w := range []string{"↺ ~/.claude/agents/go-reviewer.md", "- ~/.claude/skills/tdd/SKILL.md", "= ~/.claude/commands/plan.md   (you changed it: kept)"} {
		if !strings.Contains(undo, w) {
			t.Errorf("undo lines miss %q:\n%s", w, undo)
		}
	}
	res, err := Undo(state, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "my own reviewer\n" {
		t.Errorf("old file not back: %q", b)
	}
	if b, _ := os.ReadFile(plan); !strings.Contains(string(b), "I changed this") {
		t.Error("a file the user changed was lost")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Error("the folders the pack made are still there")
	}
	if res.Restored != 1 || res.Removed != 2 || len(res.Kept) != 1 {
		t.Errorf("result %+v", res)
	}
	if list, _ := Installed(state); len(list) != 0 {
		t.Errorf("record still there: %+v", list)
	}
}

// Spec (OMX-46): the installed version is pinned: a second install of
// the same pack into the same agent is refused until the first is
// undone; another agent is fine.
func TestInstalledPackIsPinned(t *testing.T) {
	p, _ := Load(samplePack(t))
	home, state := t.TempDir(), t.TempDir()
	pl, _ := PlanInstall(p, "claude", home, state, nil)
	if _, err := Apply(pl, state); err != nil {
		t.Fatal(err)
	}
	p.Version = "2.0.0"
	if _, err := PlanInstall(p, "claude", home, state, nil); err == nil || !strings.Contains(err.Error(), "1.2.0") || !strings.Contains(err.Error(), "undo") {
		t.Errorf("second install: %v", err)
	}
	if _, err := PlanInstall(p, "codex", home, state, nil); err != nil {
		t.Errorf("other agent: %v", err)
	}
}

// Spec (OMX-46): when a file changed between the preview and Apply,
// nothing is written (the user saw something else).
func TestApplyRefusesAStalePreview(t *testing.T) {
	p, _ := Load(samplePack(t))
	home, state := t.TempDir(), t.TempDir()
	pl, _ := PlanInstall(p, "claude", home, state, nil)
	write(t, filepath.Join(home, ".claude", "commands", "plan.md"), "new since the preview\n")
	if _, err := Apply(pl, state); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("stale: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "go-reviewer.md")); !os.IsNotExist(err) {
		t.Error("a stale plan wrote files")
	}
}
