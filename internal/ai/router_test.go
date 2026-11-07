package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-25: the router is off unless ai_router.mode is auto.
func TestRouterOffByDefault(t *testing.T) {
	if c := (Router{}).Pick(RouteInput{Text: "hi"}); c.Slot != "" {
		t.Fatalf("off router picked %+v", c)
	}
	if c := (Router{Mode: "off"}).Pick(RouteInput{Text: "hi"}); c.Slot != "" {
		t.Fatalf("mode off picked %+v", c)
	}
}

// CMP-25: a short plain question goes to quick, a hard one to main.
func TestRouterPicksByHardness(t *testing.T) {
	r := Router{Mode: "auto"}
	cases := []struct {
		in   RouteInput
		slot string
		hard bool
	}{
		{RouteInput{Text: "what does git rebase do?"}, SlotQuick, false},
		{RouteInput{Text: "refactor the session store into two files"}, SlotMain, true},
		{RouteInput{Text: "why is this slow"}, SlotMain, true},
		{RouteInput{Text: "fix this:\n```go\nx := 1\n```"}, SlotMain, true},
		{RouteInput{Text: "rename it", Attachments: 1}, SlotMain, true},
		{RouteInput{Text: "look", Images: 1}, SlotMain, true},
		{RouteInput{Text: "short", Think: "high"}, SlotMain, true},
		{RouteInput{Text: strings.Repeat("word ", RouteHardRunes/5+1)}, SlotMain, true},
		{RouteInput{Text: strings.Repeat("line\n", RouteHardLines)}, SlotMain, true},
	}
	for _, c := range cases {
		got := r.Pick(c.in)
		if got.Slot != c.slot || got.Hard != c.hard || got.Reason == "" {
			t.Errorf("Pick(%q) = %+v, want slot %s hard %v", c.in.Text, got, c.slot, c.hard)
		}
	}
}

// CMP-25: words match whole words only ("plan" is not in "planet").
func TestRouterWholeWords(t *testing.T) {
	if hard, _ := RouteHard(RouteInput{Text: "name a planet"}); hard {
		t.Fatal("planet matched plan")
	}
	if hard, _ := RouteHard(RouteInput{Text: "make a plan."}); !hard {
		t.Fatal("plan. did not match plan")
	}
}

// CMP-25: the first rule that matches wins over the guess; own quick
// and deep slots are used.
func TestRouterRulesAndOwnSlots(t *testing.T) {
	r := Router{Mode: "auto", Quick: "mini", Deep: "opus", Rules: []RouteRule{
		{Words: []string{"docs", "README"}, Slot: "writer"},
		{Words: []string{"docs"}, Slot: "never"},
	}}
	if c := r.Pick(RouteInput{Text: "refactor the readme"}); c.Slot != "writer" || c.Rule != 1 {
		t.Fatalf("rule: %+v", c)
	}
	if c := r.Pick(RouteInput{Text: "hello"}); c.Slot != "mini" {
		t.Fatalf("own quick: %+v", c)
	}
	if c := r.Pick(RouteInput{Text: "debug the race"}); c.Slot != "opus" {
		t.Fatalf("own deep: %+v", c)
	}
}

// CMP-25: a broken router is reported and picks nothing.
func TestCheckRouter(t *testing.T) {
	bad := []Router{
		{Mode: "sometimes"},
		{Mode: "auto", Quick: "no spaces allowed"},
		{Mode: "auto", Rules: []RouteRule{{Words: []string{"x"}, Slot: ""}}},
		{Mode: "auto", Rules: []RouteRule{{Words: []string{" "}, Slot: "gpt"}}},
		{Mode: "auto", Rules: make([]RouteRule, MaxRouteRules+1)},
	}
	for _, r := range bad {
		if CheckRouter(r) == nil {
			t.Errorf("CheckRouter(%+v) = nil", r)
		}
		if c := r.Pick(RouteInput{Text: "hi"}); c.Slot != "" {
			t.Errorf("broken router picked %+v", c)
		}
	}
	good := Router{Mode: "AUTO", Deep: "openai/gpt-5", Rules: []RouteRule{{Words: []string{"sql"}, Slot: "local"}}}
	if err := CheckRouter(good); err != nil {
		t.Fatal(err)
	}
	_, errs := ResolveSlots(Settings{Router: Router{Mode: "maybe"}}, Config{}, Credentials{}, func(string) string { return "" })
	found := false
	for _, e := range errs {
		found = found || strings.HasPrefix(e, "ai_router:")
	}
	if !found {
		t.Fatalf("ResolveSlots errors %v miss ai_router", errs)
	}
}

// CMP-25: SaveRouterMode sets only ai_router.mode.
func TestSaveRouterModeKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ai_model":"m","ai_router":{"quick":"mini","rules":[{"words":["sql"],"slot":"local"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveRouterMode(path, "auto"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model  string `json:"ai_model"`
		Router Router `json:"ai_router"`
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "m" || got.Router.Mode != "auto" || got.Router.Quick != "mini" || len(got.Router.Rules) != 1 {
		t.Fatalf("after save: %s", b)
	}
	if err := SaveRouterMode(path, "sometimes"); err == nil {
		t.Fatal("bad mode saved")
	}
	if s := LoadSettings(path); !s.Router.On() {
		t.Fatal("LoadSettings does not read ai_router")
	}
}
