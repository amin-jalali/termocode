package ai

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// G-WT: own worktrees for parallel agents are on unless ai_worktrees is
// "off"; the palette toggle saves the key and leaves other keys alone.
func TestSettingsWorktrees(t *testing.T) {
	if !(Settings{}).UseWorktrees() {
		t.Error("default: want on")
	}
	if (Settings{Worktrees: WorktreesOff}).UseWorktrees() {
		t.Error(`"off": want off`)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := SaveSettings(p, Settings{Worktrees: WorktreesOff, MaxSteps: 7}); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(p); s.Worktrees != WorktreesOff || s.MaxSteps != 7 {
		t.Errorf("loaded %+v", s)
	}
	if err := SaveSettings(p, Settings{MaxSteps: 7}); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(p); s.Worktrees != "" {
		t.Errorf("on again: key kept %q", s.Worktrees)
	}
}

// A step that ran in its own worktree keeps it in the chat file, so its
// Merge / Discard card is there after a restart.
func TestStoredStepWorktree(t *testing.T) {
	st := StoredStep{ID: "a", Worktree: &StoredWorktree{ID: "r1-a", Path: "/p", Branch: "termocode/agent-r1-a", State: WorktreeReady, Files: 3}}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back StoredStep
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Worktree == nil || !reflect.DeepEqual(*back.Worktree, *st.Worktree) {
		t.Errorf("round trip %+v", back.Worktree)
	}
	if !back.Worktree.Open() {
		t.Error("ready: want open")
	}
	for _, s := range []string{WorktreeMerged, WorktreeDiscarded} {
		if (&StoredWorktree{State: s}).Open() {
			t.Errorf("%s: want closed", s)
		}
	}
}
