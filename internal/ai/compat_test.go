package ai

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// Spec (OMX-11): agent files (.claude/agents/*.md: front matter name,
// description, tools, model + a system prompt) become one-step project
// profiles. A read-only tool list gives a read-only step; a small model
// runs on the quick category, a big one on deep. A name that is taken
// is skipped with a note.
func TestLoadAgentProfiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	writeSkill(t, dir, "reviewer.md", "---\nname: code-reviewer\ndescription: Reviews code for bugs\ntools: Read, Grep, Glob\nmodel: opus\n---\nYou review code.\n")
	writeSkill(t, dir, "writer.md", "---\ndescription: Writes docs\nmodel: haiku\n---\nYou write docs.\n")
	writeSkill(t, dir, "taken.md", "---\nname: default\ndescription: x\n---\nbody\n")
	writeSkill(t, dir, "bad.md", "no front matter\n")
	ps, notes := LoadAgentProfiles(root)
	if len(ps) != 2 {
		t.Fatalf("profiles %+v notes %v", ps, notes)
	}
	r, w := ps[0], ps[1]
	if r.Name != "code-reviewer" || r.Source != ProfileSourceProject || r.Title != "Reviews code for bugs" {
		t.Errorf("reviewer %+v", r)
	}
	if st := r.Steps[0]; st.System != "You review code." || st.Tools.Preset != agentdag.PresetReadOnly || st.Category != agentdag.CategoryDeep {
		t.Errorf("reviewer step %+v", st)
	}
	if st := w.Steps[0]; w.Name != "writer" || st.Tools.Preset != "" || st.Category != agentdag.CategoryQuick {
		t.Errorf("writer %+v", w)
	}
	if len(notes) != 2 || !strings.Contains(strings.Join(notes, "\n"), "taken.md") {
		t.Errorf("notes %v", notes)
	}
}

// Spec (OMX-11): command files (.claude/commands/*.md, front matter
// optional) are "/name" chat commands; $ARGUMENTS takes the rest of the
// message, else the rest is added at the end. Project commands carry a
// hash (they need trust); user ones do not.
func TestLoadChatCommands(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeSkill(t, filepath.Join(root, ".claude", "commands"), "fix-issue.md", "---\ndescription: Fix a GitHub issue\n---\nFix issue $ARGUMENTS now.\n")
	writeSkill(t, filepath.Join(home, "commands"), "explain.md", "Explain this code simply.\n")
	writeSkill(t, filepath.Join(home, "commands"), "fix-issue.md", "user wins\n")
	cs := LoadChatCommands(root, filepath.Join(home, "commands"))
	if len(cs.List) != 2 || cs.ProjectHash != "" {
		t.Fatalf("commands %+v", cs)
	}
	fi, ok := cs.Find("fix-issue")
	if !ok || fi.Source != SkillSourceUser || fi.Body != "user wins" {
		t.Errorf("user command must win: %+v", fi)
	}
	cs = LoadChatCommands(root)
	fi, _ = cs.Find("fix-issue")
	if cs.ProjectHash == "" || fi.Description != "Fix a GitHub issue" {
		t.Fatalf("project command %+v hash %q", fi, cs.ProjectHash)
	}
	if got := fi.Expand("42"); got != "Fix issue 42 now." {
		t.Errorf("expand = %q", got)
	}
	ex := ChatCommand{Body: "Explain this code simply."}
	if got := ex.Expand("main.go"); got != "Explain this code simply.\n\nmain.go" {
		t.Errorf("expand without $ARGUMENTS = %q", got)
	}
	for in, want := range map[string][2]string{"/fix-issue 42": {"fix-issue", "42"}, "/explain": {"explain", ""}, " /x": {"", ""}, "/": {"", ""}, "hello": {"", ""}, "/a/b": {"", ""}} {
		n, a, ok := SplitChatCommand(in)
		if (want[0] != "") != ok || n != want[0] || a != want[1] {
			t.Errorf("SplitChatCommand(%q) = %q %q %v", in, n, a, ok)
		}
	}
}
