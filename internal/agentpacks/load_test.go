package agentpacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// samplePack lays out a small pack like the open packs do: agents/*.md,
// skills/<name>/..., rules/<group>/*.md and commands/*.md.
func samplePack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "package.json"), `{"name":"demo-pack","version":"1.2.0"}`)
	write(t, filepath.Join(dir, "agents", "go-reviewer.md"), "---\nname: go-reviewer\ndescription: Reviews Go code.\n---\nYou review Go code carefully, one file at a time.\n")
	write(t, filepath.Join(dir, "skills", "tdd", "SKILL.md"), "---\nname: tdd\ndescription: Test first.\n---\nWrite the failing test first.\n")
	write(t, filepath.Join(dir, "skills", "tdd", "scripts", "run.sh"), "echo hi\n")
	write(t, filepath.Join(dir, "rules", "golang", "style.md"), "Use gofmt.\n")
	write(t, filepath.Join(dir, "commands", "plan.md"), "---\ndescription: Make a plan.\n---\nPlan the work.\n")
	write(t, filepath.Join(dir, "README.md"), "not part of the pack\n")
	write(t, filepath.Join(dir, "agents", ".hidden.md"), "skipped\n")
	write(t, filepath.Join(dir, "agents", "notes.txt"), "only .md agents\n")
	return dir
}

// Spec (OMX-46): Load reads the pack's name and version (pack.json, else
// package.json, else the folder name and "local"), and only the four
// known parts: agents/*.md, skills/** , rules/**.md, commands/*.md.
// Hidden files, other files and other folders are not part of the pack.
func TestLoadReadsTheFourParts(t *testing.T) {
	p, err := Load(samplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo-pack" || p.Version != "1.2.0" {
		t.Errorf("name %q version %q", p.Name, p.Version)
	}
	var got []string
	for _, f := range p.Files {
		got = append(got, string(f.Kind)+":"+f.Rel)
	}
	want := "agent:agents/go-reviewer.md command:commands/plan.md rule:rules/golang/style.md skill:skills/tdd/SKILL.md skill:skills/tdd/scripts/run.sh"
	if strings.Join(got, " ") != want {
		t.Errorf("files\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if c := p.Count(); c[KindAgent] != 1 || c[KindSkill] != 1 || c[KindRule] != 1 || c[KindCommand] != 1 {
		t.Errorf("count %v (a skill is one folder)", c)
	}
}

// Spec (OMX-46): pack.json wins over package.json; without both the
// folder name and "local" are used.
func TestLoadNameAndVersion(t *testing.T) {
	dir := samplePack(t)
	write(t, filepath.Join(dir, "pack.json"), `{"name":"own","version":"9"}`)
	if p, _ := Load(dir); p.Name != "own" || p.Version != "9" {
		t.Errorf("pack.json: %q %q", p.Name, p.Version)
	}
	bare := filepath.Join(t.TempDir(), "my-pack")
	write(t, filepath.Join(bare, "agents", "a.md"), "x")
	if p, _ := Load(bare); p.Name != "my-pack" || p.Version != "local" {
		t.Errorf("bare: %q %q", p.Name, p.Version)
	}
}

// Spec (OMX-46): a pack is untrusted data: a symlink, a file that is too
// big, or an empty pack is refused with a plain reason; nothing outside
// the pack folder is read.
func TestLoadRefusesUnsafePacks(t *testing.T) {
	empty := t.TempDir()
	write(t, filepath.Join(empty, "README.md"), "x")
	if _, err := Load(empty); err == nil || !strings.Contains(err.Error(), "no agents, skills, rules or commands") {
		t.Errorf("empty pack: %v", err)
	}

	link := samplePack(t)
	secret := filepath.Join(t.TempDir(), "secret.md")
	write(t, secret, "outside")
	if err := os.Symlink(secret, filepath.Join(link, "agents", "evil.md")); err != nil {
		t.Skip("no symlinks here")
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("symlink: %v", err)
	}

	big := samplePack(t)
	write(t, filepath.Join(big, "rules", "huge.md"), strings.Repeat("x", MaxFileBytes+1))
	if _, err := Load(big); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("big file: %v", err)
	}
}

// Spec (OMX-46): the always-loaded token cost counts rules in full and
// only the name + description of agents, skills and commands; the
// on-use cost is the rest of their text.
func TestTokenCost(t *testing.T) {
	p, err := Load(samplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	always, onUse := p.Tokens(nil)
	if always <= 0 || onUse <= 0 {
		t.Fatalf("always %d onUse %d", always, onUse)
	}
	ra, ru := p.Tokens([]Kind{KindRule})
	if ra != Tokens([]byte("Use gofmt.\n")) || ru != 0 {
		t.Errorf("rules: always %d onUse %d", ra, ru)
	}
	aa, au := p.Tokens([]Kind{KindAgent})
	if aa != Tokens([]byte("go-reviewer Reviews Go code.")) || au != Tokens([]byte("You review Go code carefully, one file at a time.\n")) {
		t.Errorf("agents: always %d onUse %d", aa, au)
	}
	if Tokens(make([]byte, 400)) != 100 {
		t.Errorf("about 4 bytes per token: %d", Tokens(make([]byte, 400)))
	}
}
