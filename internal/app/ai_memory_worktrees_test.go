package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/aimemory"
)

func TestAIMemoryWorktreePromptAndReadOnlyPage(t *testing.T) {
	m, _, child, _ := aiSkillsModel(t)
	main := t.TempDir()
	admin := filepath.Join(main, ".git", "worktrees", "agent")
	if err := os.MkdirAll(admin, 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(child, ".git"):        "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"):   "../..\n",
		filepath.Join(admin, "gitdir"):      filepath.Join(child, ".git") + "\n",
		filepath.Join(main, ".git", "HEAD"): "ref: refs/heads/main\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	seedAILessons(t, main, aiTestLesson("2026-10-31", "Shared lesson", "Check the shared test fixture"))
	setAIMemory(aiMemoryOff)
	if body, _ := m.aiMemoryTurn(child, "test"); body != "" {
		t.Fatal("shared memory bypassed off switch")
	}
	setAIMemory(aiMemoryOn)
	if body, n := m.aiMemoryTurn(child, "test"); n != 1 || !strings.Contains(body, "shared test fixture") {
		t.Fatal("main lesson not offered")
	}
	m.w, m.h = 110, 35
	m.openAIMemory()
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "Main checkout") {
		t.Fatal("shared lessons not discoverable")
	}
	m.onAIMemoryPicked(aiSharedMemoryID)
	if !m.confirmOpen || m.confirmKind != confirmKindSharedMemory || focusedCardID(t, *m) != "close" {
		t.Fatal("shared preview not read only")
	}
	ls, _ := aimemory.Load(child)
	if len(ls) != 0 {
		t.Fatal("shared lessons copied to child")
	}
}
