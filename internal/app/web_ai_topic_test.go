package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webAITopicModel is a Model with a configured fake provider (Anthropic,
// a model from its list) and the built-in profiles.
func webAITopicModel(t *testing.T) *Model {
	t.Helper()
	m, _ := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	m.ai.cfg = ai.Config{Kind: ai.KindAnthropic, Model: ai.SuggestedModels(ai.KindAnthropic)[0], APIKey: "sk-" + "test-secret"}
	m.ai.loadProfiles(ai.Credentials{})
	return m
}

func webTestAI(t *testing.T, frames []map[string]any) (webproto.AISnap, int) {
	t.Helper()
	var s webproto.AISnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "ai" {
			s = webproto.AISnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEBP-3): the ai topic tells the page the laptop's provider (its
// name only), model and the models its picker offers, the active profile
// and the profiles with their source and trust, and the ghost text, next
// edit and worktree settings. Never a key. A profile switch on the
// laptop sends it again; nothing new sends nothing.
func TestWebAITopic(t *testing.T) {
	m := webAITopicModel(t)
	webTestClock(t)
	webTestHub(t, m)
	c := webTestSub(t, m, "ai")
	s, n := webTestAI(t, webTestNow(t, c))
	if n != 1 || !s.Configured || s.Provider != ai.KindLabel(ai.KindAnthropic) || s.Model != m.ai.cfg.Model || len(s.Models) == 0 || s.Models[0] != m.ai.cfg.Model {
		t.Fatalf("snap n=%d %+v", n, s)
	}
	if s.Profile != ai.ProfileDefault || len(s.Profiles) < 2 || !s.Inline || !s.Worktrees {
		t.Fatalf("profiles %+v", s)
	}
	for _, p := range s.Profiles {
		if p.Source != "builtin" || p.Trust != "use" {
			t.Errorf("profile %+v", p)
		}
	}
	raw, _ := webData(s)
	if strings.Contains(string(raw), "sk-"+"test") {
		t.Fatalf("a key in the topic: %s", raw)
	}
	m.webAfterUpdate(nil)
	if _, n := webTestAI(t, webTestNow(t, c)); n != 0 {
		t.Fatal("sent with nothing new")
	}
	m.storeAIProfile(ai.ProfileReview)
	m.webAfterUpdate(nil)
	if s, n = webTestAI(t, webTestNow(t, c)); n != 1 || s.Profile != ai.ProfileReview {
		t.Fatalf("after a switch n=%d %+v", n, s.Profile)
	}
}

// Spec (WEBP-3, WEB-7): the ai replay golden.
func TestWebAIReplayGolden(t *testing.T) {
	golden, err := filepath.Abs(webReplayPath("ai", "profile")) // before the model's test folder becomes the cwd
	if err != nil {
		t.Fatal(err)
	}
	// The race agents come from the laptop's PATH; pin them so the golden
	// is the same on every machine (CI has no agent installed).
	old := agentLookPath
	t.Cleanup(func() { agentLookPath = old })
	agentLookPath = func(bin string) (string, error) {
		if bin == "claude" || bin == "codex" {
			return "/usr/bin/" + bin, nil
		}
		return "", exec.ErrNotFound
	}
	m := webAITopicModel(t)
	m.ai.cfg.Model = "claude-test-1"
	webTestClock(t)
	h := webTestHub(t, m)
	c := webReplayRecord(t, m, h, "ai", func() {
		m.storeAIProfile(ai.ProfileReview)
		m.webAfterUpdate(nil)
		off := false
		m.ai.settings.Inline = &off
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["ai"], c)
	webReplayGolden(t, golden, c)
}

// Spec (WEBP-3, web design 3.3): the page can only ask to use another
// profile or model; a laptop card asks (Cancel first) and only the
// laptop's yes switches. A model must be one of the laptop's picker's;
// a project profile the laptop has not trusted is refused.
func TestWebAIUseProfileAndModelThroughTheLaptop(t *testing.T) {
	m := webAITopicModel(t)
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)

	r.call(webproto.OpAIUseProfile, &webproto.AIUseArgs{Name: ai.ProfileReview})
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatal("no laptop card")
	}
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Fatal("switched before the laptop said yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if m.ai.activeProfile().Name != ai.ProfileReview {
		t.Fatalf("profile %q", m.ai.activeProfile().Name)
	}
	if code := webErrCode(r.call(webproto.OpAIUseProfile, &webproto.AIUseArgs{Name: "nope"})); code != "not_found" {
		t.Errorf("unknown profile: %s", code)
	}
	m.ai.profFake = []ai.Profile{{Name: "repo", Source: ai.ProfileSourceProject, Steps: []ai.ProfileStep{{ID: "a", Slot: ai.SlotMain}}}}
	m.ai.reloadProfiles()
	if code := webErrCode(r.call(webproto.OpAIUseProfile, &webproto.AIUseArgs{Name: "repo"})); code != "forbidden" || m.confirmOpen {
		t.Errorf("untrusted project profile: %s", code)
	}

	want := ai.SuggestedModels(ai.KindAnthropic)[1]
	if code := webErrCode(r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: "my-own-model"})); code != "not_found" {
		t.Errorf("typed model: %s", code)
	}
	r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: want})
	if !m.confirmOpen || m.ai.cfg.Model == want {
		t.Fatal("no card, or switched before the laptop said yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if m.ai.cfg.Model == want {
		t.Fatal("cancel switched")
	}
	r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: want})
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if m.ai.settings.Model != want {
		t.Fatalf("model setting %q", m.ai.settings.Model)
	}
}

// The chat's model picker follows the active profile (the owner's GPT:
// one Codex step on gpt-6-sol, the main model Claude): it lists Codex's
// models and the one in use is the step's, not the main model. Picking
// another asks the laptop and saves it into the profile's step. A
// profile of several steps lists its steps and their models instead.
func TestWebAIPickerFollowsTheProfile(t *testing.T) {
	m := webAITopicModel(t)
	m.ai.cfg.Model = "claude-opus-5-5"
	m.ai.profFake = []ai.Profile{gptProfile()}
	m.ai.reloadProfiles()
	m.storeAIProfile("gpt")
	s := m.webAIBuild()
	var codex []string
	for _, mi := range ai.CLIModels(ai.CLICodex) {
		codex = append(codex, mi.ID)
	}
	if s.Provider != "Codex" || s.Model != "gpt-6-sol" || strings.Join(s.Models, " ") != strings.Join(codex, " ") || len(s.Steps) != 0 {
		t.Errorf("one Codex step: provider %q model %q models %v steps %v", s.Provider, s.Model, s.Models, s.Steps)
	}

	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	if code := webErrCode(r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: "claude-sonnet-5-5"})); code != "not_found" {
		t.Errorf("a model of another provider: %s", code)
	}
	if code := webErrCode(r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: "gpt-6-sol"})); code != "conflict" {
		t.Errorf("the model in use: %s", code)
	}
	r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: "gpt-6-astra"})
	if !m.confirmOpen {
		t.Fatal("no laptop card")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	path, _ := ai.ProfilesPath()
	saved := ai.LoadProfiles(path, "", map[string]bool{ai.SlotMain: true}, 1)
	if p, ok := saved.Get("gpt"); !ok || len(p.Steps) != 1 || p.Steps[0].Model != "gpt-6-astra" || p.Steps[0].CLI != ai.CLICodex {
		t.Errorf("saved profile %+v (ok %v)", p, ok)
	}
	if m.ai.cfg.Model != "claude-opus-5-5" {
		t.Errorf("the main model changed to %q", m.ai.cfg.Model)
	}

	m.storeAIProfile(ai.ProfileReview)
	s = m.webAIBuild()
	if len(s.Models) != 0 || s.Model != "" || len(s.Steps) != 2 || s.Steps[0].Title != "Reviewer" || s.Steps[1].Title != "Fixer" {
		t.Errorf("two steps: model %q models %v steps %+v", s.Model, s.Models, s.Steps)
	}
}
