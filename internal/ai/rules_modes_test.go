package ai

import (
	"strings"
	"testing"
)

// modeRulesRoot is a workspace with a manual, an auto, an auto rule
// without a description and a glob rule in .claude/rules.
func modeRulesRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // the Go walker
	root := t.TempDir()
	writeRule(t, root, ".claude/rules/release.md", "---\ninclusion: manual\n---\nrelease rule\n")
	writeRule(t, root, ".claude/rules/db.md", "---\ninclusion: auto\ndescription: database migrations and SQL\n---\ndb rule\n")
	writeRule(t, root, ".claude/rules/nodesc.md", "---\ninclusion: auto\n---\nnodesc rule\n")
	writeRule(t, root, ".claude/rules/Named Rule.md", "---\ninclusion: manual\nname: deploy\n---\ndeploy rule\n")
	writeRule(t, root, ".claude/rules/go.md", "---\nglobs: \"*.go\"\n---\ngo rule\n")
	return root
}

// Spec (CMP-82): a .claude/rules file with "inclusion: manual" is used
// only when the user names it (#name); "inclusion: auto" with a
// description is offered to the model by that description. Neither is
// in the prompt text nor added to read_file results; both are in the
// trust hash and in Files. An auto rule without a description is
// manual. The name is the front matter name or the file name.
func TestRulesManualAndAutoModes(t *testing.T) {
	root := modeRulesRoot(t)
	r := LoadProjectRules(root)
	for _, bad := range []string{"release rule", "db rule", "nodesc rule", "deploy rule", "inclusion"} {
		if strings.Contains(r.Text, bad) {
			t.Errorf("prompt text holds %q:\n%s", bad, r.Text)
		}
	}
	names := func(rs []ScopedRule) string {
		var out []string
		for _, s := range rs {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(r.Manual()); got != "deploy,nodesc,release" {
		t.Errorf("manual %q", got)
	}
	auto := r.Auto()
	if names(auto) != "db" || auto[0].Description != "database migrations and SQL" || auto[0].Mode != RuleModeAuto {
		t.Errorf("auto %+v", auto)
	}
	if got := r.ScopedFor("main.go"); len(got) != 1 || got[0].File != ".claude/rules/go.md" {
		t.Errorf("ScopedFor(main.go) %+v", got)
	}
	if !strings.Contains(strings.Join(r.Files, " "), ".claude/rules/release.md") {
		t.Errorf("files %v", r.Files)
	}
	h := r.Hash
	writeRule(t, root, ".claude/rules/db.md", "---\ninclusion: auto\ndescription: only SQL\n---\ndb rule\n")
	if LoadProjectRules(root).Hash == h {
		t.Error("a changed description kept the trust hash")
	}
}

// Spec (CMP-82): "#name" in the user's message names a manual or auto
// rule (a word start only, not "issue#name"; unknown names are skipped).
func TestRuleMentions(t *testing.T) {
	r := LoadProjectRules(modeRulesRoot(t))
	var got []string
	for _, s := range r.Mentioned("follow #release and (#db), not issue#deploy or #nope; #release again") {
		got = append(got, s.Name)
	}
	if strings.Join(got, ",") != "release,db" {
		t.Errorf("mentions %v", got)
	}
	if len(r.Mentioned("no tags here")) != 0 {
		t.Error("mentions without #")
	}
}

// Spec (CMP-82): a named rule is added to the turn once per chat; the
// model loads an auto rule by name (never a manual one), once per chat.
func TestRulesInjectorMentionAndLoad(t *testing.T) {
	r := LoadProjectRules(modeRulesRoot(t))
	var j RulesInjector
	first := j.Mention(r, "use #release")
	if !strings.Contains(first, "release rule") || !strings.Contains(first, ".claude/rules/release.md") {
		t.Fatalf("mention %q", first)
	}
	if again := j.Mention(r, "#release"); again != "" {
		t.Errorf("mention again %q", again)
	}
	text, err := j.Load(r, "db")
	if err != nil || !strings.Contains(text, "db rule") {
		t.Fatalf("load %q %v", text, err)
	}
	if again, err := j.Load(r, "db"); err != nil || strings.Contains(again, "db rule") {
		t.Errorf("load again %q %v", again, err)
	}
	if _, err := j.Load(r, "release"); err == nil {
		t.Error("the model loaded a manual rule")
	}
	if _, err := j.Load(r, "nope"); err == nil || !strings.Contains(err.Error(), "db") {
		t.Errorf("unknown rule error %v", err)
	}
}
