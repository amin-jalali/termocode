package app

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec: the AI's rename tool keeps a staged file staged: git shows a
// rename with the staged text, not a deleted file plus an untracked one.
// It works for a move into another folder too.
func TestAIRenameKeepsStagedFileStaged(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("rename", map[string]any{"from": "a.txt", "to": "b.txt"}),
		toolCall("rename", map[string]any{"from": "b.txt", "to": "docs/c.txt"}),
	}}
	body := strings.Repeat("a line that stays the same\n", 10)
	m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": body, "keep.txt": "k\n", "docs/x.txt": "x\n"})
	gitOut := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	gitOut("init", "-q", "-b", "main")
	gitOut("add", ".")
	gitOut("commit", "-q", "-m", "init")
	if err := os.WriteFile(dir+"/a.txt", []byte(body+"staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut("add", "a.txt")
	driveAIChat(t, m, m.sendAIChat("rename it"), nil)
	if got := strings.TrimSpace(gitOut("status", "--porcelain=v1")); got != "R  a.txt -> docs/c.txt" {
		t.Errorf("git status %q, want a staged rename", got)
	}
	if got := gitOut("show", ":docs/c.txt"); got != body+"staged\n" {
		t.Errorf("staged text %q", got)
	}
}
