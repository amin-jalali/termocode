package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// AI provider pages and cards (itermoc_providers.go): a signed-out CLI,
// a list that did not refresh, an environment key in ASCII mode, the
// Disconnect card per method and a refresh that can not reach the
// server.

func TestCovAIProviderPageStates(t *testing.T) {
	imIcons(t, theme.IconModeASCII)
	sn := itermocSnap{providers: []imProvider{
		{kind: ai.KindClaudeOAuth, name: "Claude", method: ai.MethodCLI, cli: "claude", label: "sign-in", cost: "your plan", signedOut: true, models: []string{"opus"}},
		{kind: ai.KindOpenAI, name: "OpenAI", method: ai.MethodKey, label: "API key", cost: "API · pay per use", err: "could not reach OpenAI"},
		{kind: ai.KindOpenRouter, name: "OpenRouter", method: ai.MethodKey, label: "via environment", envOnly: true, models: []string{"x"}},
		{kind: ai.KindCompatible, name: "Box", method: ai.MethodURL, label: "server", host: "http://127.0.0.1:8080", models: []string{"m"}},
	}}
	page := func(kind string) imContent {
		return providerPageContent(sn, imRow{kind: imRowProvider, sec: imSecModels, name: itermocProvName(kind)})
	}
	if c := page(ai.KindClaudeOAuth); c.card == nil || !strings.Contains(c.card.title, "is signed out") || c.right != "! signed out" || c.card.pills[0].key != "i" {
		t.Errorf("signed out page: %+v", c.card)
	}
	c := page(ai.KindOpenAI)
	if c.card == nil || c.right != "! not refreshed" || !strings.Contains(strings.Join(c.card.lines, " "), "could not reach OpenAI") {
		t.Errorf("not refreshed page: %+v", c.card)
	}
	if c.facts[0].lines[0] != "No models yet. r loads them." {
		t.Errorf("no models line = %v", c.facts[0].lines)
	}
	c = page(ai.KindOpenRouter)
	if c.right != "v via environment" || len(c.pills) != 1 || !strings.Contains(c.facts[1].lines[1], "unset "+ai.EnvKeyVar(ai.KindOpenRouter)) {
		t.Errorf("environment page: right %q pills %+v lines %v", c.right, c.pills, c.facts[1].lines)
	}
	c = page(ai.KindCompatible)
	found := false
	for _, r := range c.facts[1].rows {
		found = found || (r[0] == "server" && r[1] == "http://127.0.0.1:8080")
	}
	if !found || c.right != "v connected" {
		t.Errorf("server page rows %v right %q", c.facts[1].rows, c.right)
	}
}

// Disconnect…: Keep is first; the text fits the method; an environment
// key that stays set is named; an env-only AI has no card.
func TestCovAIProviderDisconnectCards(t *testing.T) {
	m := provTestModel(t)
	t.Setenv("OPENAI_API_KEY", "sk-cov-env-123456789")
	imInject(m, func(sn *itermocSnap) {
		sn.providers = []imProvider{
			{kind: ai.KindOpenAI, name: "OpenAI", method: ai.MethodKey},
			{kind: ai.KindOllama, name: "Ollama", method: ai.MethodLocal},
			{kind: ai.KindCompatible, name: "Box", method: ai.MethodURL},
			{kind: ai.KindOpenRouter, name: "OpenRouter", method: ai.MethodKey, envOnly: true},
		}
	})
	for kind, want := range map[string]string{ai.KindOpenAI: "OPENAI_API_KEY is still set", ai.KindOllama: "keeps running on this machine", ai.KindCompatible: "forgets the server URL"} {
		m.confirmOpen = false
		m.openItermocProvDisconnect(kind)
		if !m.confirmOpen || m.confirm.Buttons()[0].ID != "keep" || !strings.Contains(provCard(m), want) {
			t.Errorf("%s card: %q buttons %+v", kind, provCard(m), m.confirm.Buttons())
		}
		if m.answerItermocProvDisconnect("keep") != nil || m.itermocS().provDisconnect != "" {
			t.Errorf("%s: keep did something", kind)
		}
	}
	m.confirmOpen = false
	if m.openItermocProvDisconnect(ai.KindOpenRouter) != nil || m.confirmOpen {
		t.Error("an environment-only AI got a Disconnect card")
	}
	if m.answerItermocProvDisconnect("disconnect") != nil {
		t.Error("disconnect with nothing pending did something")
	}
}

// A refresh that can not reach the server says so (not "key rejected");
// a second refresh while one runs is not started; an unknown kind is
// nothing.
func TestCovAIProviderRefreshFailure(t *testing.T) {
	m := provTestModel(t)
	old := aiListModels
	aiListModels = func(_ context.Context, cfg ai.Config) ([]ai.ModelInfo, error) {
		return nil, errors.New("dial tcp: connection refused")
	}
	t.Cleanup(func() { aiListModels = old })
	cmd := m.itermocProvRefresh(ai.KindAnthropic, true)
	if cmd == nil || m.itermocProvRefresh(ai.KindAnthropic, true) != nil {
		t.Fatal("a second refresh started while one runs")
	}
	provRun(t, m, cmd)
	if got := m.aiS().provErr[ai.KindAnthropic]; got != "could not reach Anthropic" {
		t.Errorf("error = %q", got)
	}
	if m.itermocProvRefresh("no-such-kind", true) != nil {
		t.Error("refresh of an unknown kind")
	}
	if _, ok := m.handleItermocProvMsg("not ours"); ok {
		t.Error("a foreign message was taken")
	}
}
