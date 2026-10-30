package app

import (
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
