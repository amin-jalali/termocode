package ai

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentFilesWithProblemsAreSkippedWithNotes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeSkill(t, dir, "badname.md", "---\nname: Bad Name!\ndescription: x\n---\nbody\n")
	writeSkill(t, dir, "empty.md", "---\nname: empty-one\ndescription: x\n---\n")
	writeSkill(t, dir, "long.md", "---\nname: long-one\ndescription: "+strings.Repeat("d", SkillDescMaxRunes+20)+"\ntools: Read, Edit\n---\nbody\n")
	writeSkill(t, dir, "plain.md", "---\nname: plain-one\n---\nbody\n")
	writeSkill(t, dir, "big.md", "---\nname: big-one\n---\n"+strings.Repeat("x", SkillMaxBytes+1))
	writeSkill(t, dir, ".hidden.md", "---\nname: hidden\n---\nbody\n")
	writeSkill(t, dir, "notes.txt", "not markdown")
	ps, notes := LoadAgentProfiles(root)
	all := strings.Join(notes, "\n")
	for _, want := range []string{"bad name", "empty body", "over 32 KB"} {
		if !strings.Contains(all, want) {
			t.Errorf("notes lack %q:\n%s", want, all)
		}
	}
	byName := map[string]Profile{}
	for _, p := range ps {
		byName[p.Name] = p
	}
	if len(byName) != 2 {
		t.Fatalf("profiles %v", byName)
	}
	if got := []rune(byName["long-one"].Title); len(got) != SkillDescMaxRunes || got[len(got)-1] != '…' {
		t.Errorf("a long description is cut to %d runes with a mark, got %d", SkillDescMaxRunes, len(got))
	}
	if byName["long-one"].Steps[0].Tools.Preset != "" {
		t.Error("an agent that may edit is not read-only")
	}
	if byName["plain-one"].Title != "plain-one" {
		t.Errorf("no description: the name is the title, got %q", byName["plain-one"].Title)
	}
}

func TestChatCommandFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "commands")
	writeSkill(t, dir, "empty.md", "---\ndescription: d\n---\n")
	writeSkill(t, dir, "plain.md", "Just a prompt\n")
	writeSkill(t, dir, "bad.md", "---\nname: x\n")
	c := LoadChatCommands(root)
	if _, ok := c.Find("plain"); !ok {
		t.Errorf("a file without front matter is a prompt: %+v", c)
	}
	if !strings.Contains(strings.Join(c.Notes, "\n"), "empty") {
		t.Errorf("notes %v", c.Notes)
	}
	if got := (ChatCommand{Body: "do it"}).Expand(""); got != "do it" {
		t.Errorf("Expand with no args = %q", got)
	}
	if got := (ChatCommand{Body: "do $ARGUMENTS now"}).Expand("x"); got != "do x now" {
		t.Errorf("Expand %q", got)
	}
	if _, ok := c.Find("nope"); ok {
		t.Error("an unknown command")
	}
	if _, _, err := splitFrontMatter("---\nname: x\n"); err == nil {
		t.Error("an unclosed front matter")
	}
	m, body, err := splitFrontMatter("---\nname: 'quoted'\nnote without colon\nTitle: \"T\"\n---\nbody")
	if err != nil || m["name"] != "quoted" || m["title"] != "T" || body != "body" {
		t.Errorf("front matter %v %q %v", m, body, err)
	}
	// a missing folder
	if c := LoadChatCommands(filepath.Join(root, "none"), filepath.Join(root, "nodir")); len(c.List) != 0 {
		t.Error("missing folders give no commands")
	}
}

// Tool texts the approval card shows, and the rules for asking again.
func TestWorkspaceToolDescriptionsAndGates(t *testing.T) {
	w := Workspace{Root: t.TempDir()}
	get := func(name string) Tool {
		tool, ok := FindTool(WorkspaceTools(w), name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		return tool
	}
	if d := get("search").Describe(map[string]any{"query": "q", "regex": true, "glob": "*.go"}); d != `Search: "q" (regex) in *.go` {
		t.Errorf("search describe %q", d)
	}
	for name, want := range map[string]string{"git_status": "Git status", "git_branch": "Git branches", "git_push": "Git push"} {
		if got := get(name).Describe(nil); got != want {
			t.Errorf("%s describe %q", name, got)
		}
	}
	if got := get("git_stage").Describe(map[string]any{"path": "a.go"}); got != "Stage a.go" {
		t.Errorf("stage describe %q", got)
	}
	commit := get("git_commit")
	if ask, class := commit.Gate(map[string]any{"amend": true}); !ask || class != "" {
		t.Errorf("an amend asks every time: %v %q", ask, class)
	}
	if ask, class := commit.Gate(map[string]any{}); !ask || class != ClassGit {
		t.Errorf("a commit: %v %q", ask, class)
	}
	if err := commit.Validate(map[string]any{"message": " "}); err == nil {
		t.Error("an empty commit message")
	}
	if err := commit.Validate(map[string]any{"amend": true}); err != nil {
		t.Errorf("an amend may keep its message: %v", err)
	}
	if d := commit.Describe(map[string]any{"amend": true}); !strings.Contains(d, "its message kept") {
		t.Errorf("amend without message %q", d)
	}
	if d := commit.Describe(map[string]any{"amend": true, "message": "new\nmore"}); d != `Amend the last commit: "new"` {
		t.Errorf("amend with message %q", d)
	}
	for _, name := range []string{"git_checkout", "git_push"} {
		if ask, class := get(name).Gate(nil); !ask || class != "" {
			t.Errorf("%s asks every time: %v %q", name, ask, class)
		}
	}
	// search with nothing found, glob with a cut list
	if out, err := get("search").Run(context.Background(), map[string]any{"query": "zzz-not-here"}); err != nil || out != "no matches" {
		t.Errorf("search %q %v", out, err)
	}
	if _, err := get("glob").Run(context.Background(), map[string]any{"pattern": " "}); err == nil {
		t.Error("an empty glob pattern")
	}
}
