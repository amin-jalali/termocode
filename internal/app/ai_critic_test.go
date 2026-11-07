package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// CMP-53: when a lane (a step's own worktree) ends, a second model (the
// critic) reviews its diff before the user merges it. Its verdict shows
// on the card; Merge with open problems asks first (Cancel first).

// aiCriticRunModel runs the two-lane chat and puts a critic model on the
// quick slot (the left step ran on main, so its critic is quick).
func aiCriticRunModel(t *testing.T, reply string) (*Model, string, string, *recordingAIProvider) {
	t.Helper()
	m, dir, _ := aiWorktreeRunModel(t, true)
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	rm := aiRunMsg(t, m)
	critic := &recordingAIProvider{replies: []ai.Chunk{{Delta: reply}, {Delta: reply}}}
	m.ai.slotFake[ai.SlotQuick] = aiSlot{p: critic, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "critic-mini"}}
	return m, dir, rm.Run.ID + ":left", critic
}

// A finished lane is marked for the critic at once; its run sends the
// lane's diff to the other model and the verdict lands on the card.
func TestAICriticReviewsLane(t *testing.T) {
	m, _, key, critic := aiCriticRunModel(t, "VERDICT: ISSUES\n- a.txt:1 says left, the task wanted right")
	st := aiRunMsg(t, m).Steps[0]
	if st.Worktree.Critic == nil || st.Worktree.Critic.State != ai.CriticRunning {
		t.Fatalf("the critic did not start when the lane ended: %+v", st.Worktree.Critic)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "critic reviewing") || !strings.Contains(got, "Critic") {
		t.Errorf("card does not show the critic:\n%s", got)
	}
	runWorktreeCmd(t, m, m.aiCriticStart(key))
	if len(critic.reqs) != 1 {
		t.Fatalf("critic requests %d", len(critic.reqs))
	}
	in := lastUserContent(critic.reqs[0])
	if !strings.Contains(in, "```diff") || !strings.Contains(in, "+left") {
		t.Errorf("the critic did not get the lane's diff: %q", in)
	}
	c := aiRunMsg(t, m).Steps[0].Worktree.Critic
	if c.State != ai.CriticIssues || c.Model != "critic-mini" || !strings.Contains(c.Notes, "a.txt:1") {
		t.Fatalf("critic %+v", c)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "critic: problems") {
		t.Errorf("verdict not on the card:\n%s", got)
	}
	// The Critic chip shows the notes on a Cancel-first card.
	m.onAIButton("wt-critic:" + key)
	if !m.confirmOpen || m.confirmKind != confirmKindAICriticNotes || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("notes card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	m.onAIConfirmClosed()
	m.confirmOpen = false
}

// Merge while the critic found problems asks first: Cancel writes
// nothing; Merge Anyway merges.
func TestAICriticGatesMerge(t *testing.T) {
	m, dir, key, _ := aiCriticRunModel(t, "VERDICT: ISSUES\n- a.txt:1 wrong")
	runWorktreeCmd(t, m, m.aiCriticStart(key))
	if cmd := m.onAIButton("wt-merge:" + key); cmd != nil {
		t.Fatal("Merge ran past the critic's problems")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAICriticMerge || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("merge card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	if cmd := m.onAIConfirmClosed(); cmd != nil {
		t.Error("Esc returned a cmd")
	}
	m.confirmOpen = false
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Fatalf("a.txt changed before the answer: %q", got)
	}
	m.onAIButton("wt-merge:" + key)
	cmd, _ := m.handleAIConfirm("merge-anyway")
	runWorktreeCmd(t, m, cmd)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "left\n" {
		t.Errorf("a.txt after Merge Anyway %q", got)
	}
}

// A clean verdict lets Merge run at once.
func TestAICriticOKMerges(t *testing.T) {
	m, dir, key, _ := aiCriticRunModel(t, "VERDICT: OK")
	runWorktreeCmd(t, m, m.aiCriticStart(key))
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "critic ok") {
		t.Errorf("card:\n%s", got)
	}
	runWorktreeCmd(t, m, m.onAIButton("wt-merge:"+key))
	if got := readFile(filepath.Join(dir, "a.txt")); got != "left\n" {
		t.Errorf("a.txt after merge %q", got)
	}
}

// ai_critic off: no critic starts when a lane ends.
func TestAICriticOff(t *testing.T) {
	m, _, _ := aiWorktreeRunModel(t, true)
	m.ai.settings.Critic = ai.CriticOff
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	if c := aiRunMsg(t, m).Steps[0].Worktree.Critic; c != nil {
		t.Errorf("critic with ai_critic off: %+v", c)
	}
}

// AI: Toggle Critic Review saves ai_critic; the AI may never run it.
func TestAICriticToggle(t *testing.T) {
	m, _ := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	m.dispatchAIPalette("ai-critic-toggle")
	p, _ := ai.ConfigPath()
	if m.ai.settings.UseCritic() || ai.LoadSettings(p).Critic != ai.CriticOff {
		t.Error("not off")
	}
	m.dispatchAIPalette("ai-critic-toggle")
	if !m.ai.settings.UseCritic() || ai.LoadSettings(p).Critic != "" {
		t.Error("not on again")
	}
	if pol, ok := aiActionPolicies["ai-critic-toggle"]; !ok || pol.Gate != aiGateNever {
		t.Errorf("policy %+v %v", pol, ok)
	}
}
