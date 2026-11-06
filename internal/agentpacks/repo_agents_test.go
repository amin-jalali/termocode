package agentpacks

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoAgents are the pack pieces picked for this repo's own dev setup
// (OMX-47): Go review, security, silent failures, test first.
var repoAgents = []string{"go-reviewer", "security-reviewer", "silent-failure-hunter", "tdd-guide"}

// Spec (OMX-47): the repo's .claude/ holds exactly the picked agents,
// each with its name and a description, adapted to this repo's rules
// (targeted -race tests, never the whole suite; no JS tooling); they are
// tracked by git (not ignored) and together add under 400 tokens to
// every chat.
func TestRepoAgentsArePickedAndSmall(t *testing.T) {
	root := filepath.Join("..", "..", ".claude")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range p.Files {
		if f.Kind != KindAgent {
			t.Errorf("only agents are picked: %s", f.Rel)
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f.Rel), ".md")
		names = append(names, name)
		head, body := frontmatter(f.Data)
		if !strings.HasPrefix(head, name+" ") {
			t.Errorf("%s: front matter name/description %q", f.Rel, head)
		}
		text := string(body)
		if strings.Contains(text, "go test ./...") || strings.Contains(text, "go test -race ./...") || strings.Contains(text, "npm ") {
			t.Errorf("%s runs the whole suite or JS tooling", f.Rel)
		}
		if !strings.Contains(text, "-race") {
			t.Errorf("%s does not say to run targeted tests with -race", f.Rel)
		}
		if out, err := exec.Command("git", "check-ignore", "-q", filepath.Join(root, filepath.FromSlash(f.Rel))).CombinedOutput(); err == nil {
			t.Errorf("%s is ignored by git %s", f.Rel, out)
		}
	}
	sort.Strings(names)
	if strings.Join(names, " ") != strings.Join(repoAgents, " ") {
		t.Errorf("agents %v want %v", names, repoAgents)
	}
	if always, _ := p.Tokens(nil); always >= 400 {
		t.Errorf("the picked agents add %d tokens to every chat", always)
	}
}
