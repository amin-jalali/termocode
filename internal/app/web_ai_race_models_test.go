package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webRaceModelsRig is a race env with a hub and one controller (the phone).
func webRaceModelsRig(t *testing.T, bins ...string) (*raceEnv, *webFilesRig) {
	t.Helper()
	e := newRaceEnv(t, bins...)
	h := newWebHub()
	t.Cleanup(h.stop)
	e.m.web = h
	r := &webFilesRig{t: t, m: e.m, h: h, root: e.dir, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return e, r
}

// Spec (MOB-63, mobile plan 2.4 and 3.9): Compare models from the phone
// waits for a laptop card (Cancel first) naming the agent, the models,
// the task and the device; nothing starts before the laptop says yes.
// Then each lane runs the agent on its own model in its own worktree.
func TestWebAIRaceModelsThroughTheLaptop(t *testing.T) {
	e, r := webRaceModelsRig(t, "claude", "codex")
	m := e.m
	f := r.call(webproto.OpAIRaceModels, &webproto.AIRaceModelsArgs{Task: "fix  the\tlogin", Agent: "claude", Models: []string{"opus", "sonnet", "haiku"}, Verify: "go test ./..."})
	var ref webproto.AskRef
	webResData(t, f, &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("compare asked: %v open %v", f, m.confirmOpen)
	}
	body := m.web.asks.shown.body
	for _, want := range []string{"Compare 3 models", "Claude Code", "opus, sonnet, haiku", "fix the login", "go test ./...", "Requested from"} {
		if !strings.Contains(body, want) {
			t.Errorf("card body lacks %q:\n%s", want, body)
		}
	}
	if m.race != nil && len(m.race.races) != 0 {
		t.Fatal("started before the laptop said yes")
	}
	m.confirmOpen = false
	e.feed(m.answerWebAsk(webproto.ChoiceConfirm))
	race := e.race(t)
	if race.Prompt != "fix the login" || len(race.Lanes) != 3 || race.Lanes[2].Model != "haiku" || race.Lanes[0].Worktree == "" {
		t.Fatalf("race %+v", race)
	}

	// Cancel starts nothing.
	r.call(webproto.OpAIRaceModels, &webproto.AIRaceModelsArgs{Task: "other", Agent: "codex", Models: []string{"gpt-6-astra", "gpt-6-luna"}})
	m.confirmOpen = false
	e.feed(m.answerWebAsk(webproto.ChoiceCancel))
	if n := len(m.raceS().races); n != 1 {
		t.Errorf("%d races after cancel", n)
	}
}

// Spec (MOB-63): the laptop checks before any card: an agent that is not
// installed, or whose CLI takes no --model, or the same model twice, is
// refused with the reason.
func TestWebAIRaceModelsRefusals(t *testing.T) {
	e, r := webRaceModelsRig(t, "claude", "amp")
	for _, a := range []webproto.AIRaceModelsArgs{
		{Task: "x", Agent: "codex", Models: []string{"a", "b"}},
		{Task: "x", Agent: "amp", Models: []string{"a", "b"}},
		{Task: "x", Agent: "claude", Models: []string{"opus", "opus"}},
	} {
		f := r.call(webproto.OpAIRaceModels, &a)
		if webErrCode(f) != string(webproto.CodeBadRequest) {
			t.Errorf("%+v: %v", a, f)
		}
	}
	if e.m.confirmOpen {
		t.Error("a card opened for a bad compare")
	}
}

// Spec (MOB-63): the phone reads the agents that can compare models and
// their model lists; a viewer may read it too (it starts nothing).
func TestWebAIRaceModelList(t *testing.T) {
	_, r := webRaceModelsRig(t, "claude")
	var out webproto.AIRaceModelList
	webResData(t, r.callOn(r.conn("v-1", webproto.RoleViewer), webproto.OpAIRaceModelList, &webproto.EmptyArgs{}), &out)
	if len(out.Agents) != 2 || out.Agents[0].ID != "claude" || !out.Agents[0].Installed || out.Agents[1].Installed || len(out.Agents[0].Models) == 0 {
		t.Fatalf("list %+v", out)
	}
}
