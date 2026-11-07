package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The git_ai_attribution setting: who is named on a commit the AI makes.
// user (the default) adds nothing; co-author adds a Co-authored-by
// trailer; ai makes the AI the author and keeps the user as committer.
func TestAICommitAttributionModes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if v, ok := gitxSettingValue(aiAttributionRowID); !ok || v != "user" {
		t.Fatalf("default value %q %v", v, ok)
	}
	if msg, author := aiCommitAttributed("fix: x"); msg != "fix: x" || author != "" {
		t.Errorf("user: %q %q", msg, author)
	}
	m := newTestModel(t)
	if _, ok := m.applyGitxSetting(aiAttributionRowID, "co-author"); !ok {
		t.Fatal("the row is not handled")
	}
	msg, author := aiCommitAttributed("fix: x")
	if author != "" || !strings.HasSuffix(msg, "\n\nCo-authored-by: "+aiCommitIdentity) {
		t.Errorf("co-author: %q %q", msg, author)
	}
	m.applyGitxSetting(aiAttributionRowID, "ai")
	if msg, author := aiCommitAttributed("fix: x"); msg != "fix: x" || author != aiCommitIdentity {
		t.Errorf("ai: %q %q", msg, author)
	}
	m.applyGitxSetting(aiAttributionRowID, "robot")
	if v, _ := gitxSettingValue(aiAttributionRowID); v != "ai" {
		t.Errorf("a bad value was saved: %q", v)
	}
	if c := settingsCategories()[aiAttributionRowID]; c != "Git" {
		t.Errorf("category %q", c)
	}
	if got := settingChoices(aiAttributionRowID); strings.Join(got, ",") != "user,co-author,ai" {
		t.Errorf("choices %q", got)
	}
	if p, ok := aiSettingPolicies["git_ai_attribution"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v", p)
	}
}

// The chat's git_commit names the AI as the setting says, and its card
// says so before the user allows it.
func TestAIGitCommitToolUsesAttribution(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := saveHTTPSetting("git_ai_attribution", "ai"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "Me")
	run("config", "user.email", "me@example.com")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	tools := aiAttributionTools(ai.WorkspaceTools(ai.Workspace{Root: dir}), dir)
	commit, ok := ai.FindTool(tools, "git_commit")
	if !ok {
		t.Fatal("no git_commit")
	}
	if h := commit.Describe(map[string]any{"message": "add a"}); !strings.Contains(h, "author: termocode AI") {
		t.Errorf("card text %q", h)
	}
	if _, err := commit.Run(context.Background(), map[string]any{"message": "add a"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(run("log", "-1", "--format=%an <%ae>|%cn")); got != aiCommitIdentity+"|Me" {
		t.Errorf("author|committer = %q", got)
	}
}
