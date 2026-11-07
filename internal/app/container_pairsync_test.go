package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/pairsync"
)

func TestPairSyncFolder(t *testing.T) {
	if got, err := pairSyncFolder(" /app/./src/ "); err != nil || got != "/app/src" {
		t.Fatalf("got %q %v", got, err)
	}
	for _, bad := range []string{"", "app", "/", "/.."} {
		if _, err := pairSyncFolder(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestPairSyncCardBodyListsEveryGroup(t *testing.T) {
	var p pairsync.Plan
	for i := 0; i < pairSyncCardList+2; i++ {
		p.Actions = append(p.Actions, pairsync.Action{Path: "src/f" + string(rune('a'+i)) + ".go", Op: pairsync.OpPush})
	}
	p.Actions = append(p.Actions,
		pairsync.Action{Path: "go.mod", Op: pairsync.OpPull},
		pairsync.Action{Path: "old.txt", Op: pairsync.OpDeleteRemote},
		pairsync.Action{Path: "gone.txt", Op: pairsync.OpDeleteLocal},
		pairsync.Action{Path: "main.go", Op: pairsync.OpConflict, Reason: "changed on both sides"},
		pairsync.Action{Path: "README.md", Op: pairsync.OpAdopt},
	)
	body := pairSyncCardBody(p, "/home/me/proj", "api-1", "/app")
	for _, want := range []string{
		"Workspace: /home/me/proj", "Container: api-1:/app",
		"→ Copy to the container: 10", "… and 2 more",
		"← Copy to the workspace: 1", "go.mod",
		"✕ Delete in the container: 1", "old.txt",
		"✕ Delete in the workspace: 1", "gone.txt",
		"! Conflicts (left alone): 1", "main.go (changed on both sides)",
		"= Same on both sides: 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("card lacks %q:\n%s", want, body)
		}
	}
	if pairSyncWrites(p) != 13 {
		t.Fatalf("writes = %d", pairSyncWrites(p))
	}
}

func TestPairSyncSummary(t *testing.T) {
	if got := pairSyncSummary(pairsync.Result{}); got != "nothing changed" {
		t.Fatal(got)
	}
	got := pairSyncSummary(pairsync.Result{Pushed: 2, Pulled: 1, DeletedLocal: 1, DeletedRemote: 1, Conflicts: 1, Failed: []string{"a: x"}})
	if got != "→ 2 · ← 1 · ✕ 2 · conflicts 1 · failed 1" {
		t.Fatal(got)
	}
}

func TestPairSyncIsNeverOfferedToTheAI(t *testing.T) {
	pol, ok := aiActionPolicies["containers-pair-sync"]
	if !ok || pol.Gate != aiGateNever {
		t.Fatalf("containers-pair-sync policy: %+v %v", pol, ok)
	}
	found := false
	for _, it := range servicesPaletteItems() {
		if it.ID == "containers-pair-sync" {
			found = true
		}
	}
	if !found {
		t.Fatal("the palette lacks containers-pair-sync")
	}
}
