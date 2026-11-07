package powers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CMP-78): every catalog power has a unique id, a valid skill
// name, a one-line description and a body; its skill file parses as a
// skill with that name and description.
func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	if len(Catalog()) < 3 {
		t.Fatalf("catalog %d", len(Catalog()))
	}
	for _, p := range Catalog() {
		if seen[p.ID] || !ai.ValidSkillName(p.SkillName()) || p.Title == "" || p.Description == "" || p.Body == "" {
			t.Errorf("bad power %+v", p)
		}
		seen[p.ID] = true
		name, desc, body, err := ai.ParseSkill(p.SkillText(), "SKILL.md")
		if err != nil || name != p.SkillName() || desc != p.Description || body == "" {
			t.Errorf("%s: %q %q %v", p.ID, name, desc, err)
		}
		if got, ok := ByID(p.ID); !ok || got.ID != p.ID {
			t.Errorf("ByID %s", p.ID)
		}
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID found an unknown power")
	}
}

// Spec (CMP-78): Install writes <skills>/<skill name>/SKILL.md and never
// replaces a file that is there; Remove deletes only an unchanged file
// (a file the user edited stays: ErrChanged).
func TestInstallRemove(t *testing.T) {
	dir := t.TempDir()
	p := Catalog()[0]
	if Installed(dir, p) {
		t.Fatal("installed before Install")
	}
	if err := Install(dir, p); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, p.SkillName(), "SKILL.md")
	if b, err := os.ReadFile(path); err != nil || string(b) != p.SkillText() {
		t.Fatalf("file %q %v", b, err)
	}
	if !Installed(dir, p) {
		t.Error("not installed after Install")
	}
	if err := Install(dir, p); !errors.Is(err, os.ErrExist) {
		t.Errorf("second Install %v", err)
	}
	if err := os.WriteFile(path, []byte(p.SkillText()+"\nmy note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, p); !errors.Is(err, ErrChanged) {
		t.Errorf("Remove of an edited file %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the edited file went")
	}
	if err := os.WriteFile(path, []byte(p.SkillText()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, p.SkillName())); !os.IsNotExist(err) {
		t.Errorf("folder left: %v", err)
	}
}
