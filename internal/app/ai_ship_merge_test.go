package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// aiShipGreen is a pushed branch with an open, mergeable PR and green CI.
func aiShipGreen() aiShipProbe {
	p := aiShipProbe{branch: "feat/x", base: "main", subjects: []string{"feat: x"}, head: "h2", pushed: true}
	p.snap = ghci.Snapshot{HeadSHA: "h2", PR: &ghci.PR{Number: 7, State: "OPEN", Mergeable: "MERGEABLE"},
		Runs: []ghci.Run{{ID: 2, Workflow: "ci", Status: "completed", Conclusion: "success", HeadSHA: "h2"}}}
	return p
}

// Spec (CMP-34): Ship and Merge When Green shows a merge card (Cancel
// first) once CI is green; a merge choice merges only the commit that was
// green, then the ship stops.
func TestAIShipMergeWhenGreen(t *testing.T) {
	f := &aiShipFake{probes: []aiShipProbe{aiShipGreen()}}
	m, _ := aiShipModel(t, f)
	aiShipDrive(t, m, m.toggleAIShipMerge(), func() bool { return m.confirmOpen && m.confirmKind == confirmKindAIShipMerge })
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("merge card is not Cancel-first: %q", m.confirm.SafeID())
	}
	if len(f.merges) != 0 {
		t.Fatal("merged before the card")
	}
	aiShipDrive(t, m, m.answerAIRulesForTest("squash"), func() bool { return m.aiS().ship == nil })
	if len(f.merges) != 1 || f.merges[0] != "7 squash h2" {
		t.Fatalf("merges %v (calls %v)", f.merges, f.calls)
	}
}

// Spec (CMP-34): Cancel (or Esc) on the merge card stops the ship and
// nothing is merged.
func TestAIShipMergeCancel(t *testing.T) {
	f := &aiShipFake{probes: []aiShipProbe{aiShipGreen()}}
	m, _ := aiShipModel(t, f)
	aiShipDrive(t, m, m.toggleAIShipMerge(), func() bool { return m.confirmOpen && m.confirmKind == confirmKindAIShipMerge })
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if m.aiS().ship != nil || len(f.merges) != 0 {
		t.Fatalf("Esc: ship %v merges %v", m.aiS().ship != nil, f.merges)
	}
}

// Spec (CMP-34): plain Ship still never merges: green stops at
// merge-ready with no card.
func TestAIShipPlainNeverMerges(t *testing.T) {
	f := &aiShipFake{probes: []aiShipProbe{aiShipGreen()}}
	m, _ := aiShipModel(t, f)
	aiShipDrive(t, m, m.toggleAIShip(), func() bool { return m.aiS().ship == nil })
	if m.confirmOpen || len(f.merges) != 0 {
		t.Fatalf("plain ship offered a merge: card %v merges %v", m.confirmOpen, f.merges)
	}
}

// Spec (CMP-34): a pull request with changes requested, or a draft, is
// never offered for merge: the ship stops and says why.
func TestAIShipMergeBlocked(t *testing.T) {
	for name, edit := range map[string]func(*ghci.PR){
		"changes": func(p *ghci.PR) { p.Review = "CHANGES_REQUESTED" },
		"draft":   func(p *ghci.PR) { p.Draft = true },
	} {
		p := aiShipGreen()
		pr := *p.snap.PR
		edit(&pr)
		p.snap.PR = &pr
		f := &aiShipFake{probes: []aiShipProbe{p}}
		m, _ := aiShipModel(t, f)
		aiShipDrive(t, m, m.toggleAIShipMerge(), func() bool { return m.aiS().ship == nil })
		if m.confirmOpen || len(f.merges) != 0 {
			t.Errorf("%s: card %v merges %v", name, m.confirmOpen, f.merges)
		}
	}
}

// Spec (CMP-34): a pull request with merge conflicts gets an AI turn that
// merges the base in and commits; it counts toward the fix cap.
func TestAIShipConflictFix(t *testing.T) {
	p := aiShipGreen()
	pr := *p.snap.PR
	pr.Mergeable = "CONFLICTING"
	p.snap.PR = &pr
	f := &aiShipFake{probes: []aiShipProbe{p}}
	m, rec := aiShipModel(t, f, ai.Chunk{Delta: "merged"})
	aiShipDrive(t, m, m.toggleAIShipMerge(), func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.reqs) == 1
	})
	text := aiReqText(rec.reqs[0])
	if !strings.Contains(text, "origin/main") || !strings.Contains(text, "conflict") || !strings.Contains(text, "git_commit") {
		t.Fatalf("conflict turn:\n%s", text)
	}
	if sh := m.aiS().ship; sh == nil || sh.fixes != 1 {
		t.Fatalf("the conflict turn did not count as a fix")
	}
	if len(f.merges) != 0 {
		t.Fatal("merged a conflicting PR")
	}
}

// The new command's policy: the AI never starts it.
func TestAIShipMergePolicy(t *testing.T) {
	pol, ok := aiActionPolicies["ai-ship-merge"]
	if !ok || pol.Gate != aiGateNever {
		t.Fatalf("policy %+v ok=%v", pol, ok)
	}
}
