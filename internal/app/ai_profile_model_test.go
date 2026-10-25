package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// modelAIProvider answers with a fixed text and records the model of
// each request it got (the request's, else the one it was built for).
type modelAIProvider struct {
	cfg  ai.Config
	mu   *sync.Mutex
	seen *[]string
}

func (modelAIProvider) Name() string        { return "model" }
func (modelAIProvider) Configured() bool    { return true }
func (modelAIProvider) SupportsTools() bool { return true }
func (p modelAIProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	m := req.Model
	if m == "" {
		m = p.cfg.Model
	}
	*p.seen = append(*p.seen, m)
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: "answer from " + p.cfg.Model}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

// aiSlotModelChat is a chat on claude-opus-5-5 whose slots are real
// (ResolveSlots) but built as recording fakes.
func aiSlotModelChat(t *testing.T) (*Model, *[]string) {
	t.Helper()
	main := ai.Config{Kind: ai.KindAnthropic, APIKey: "k", Model: "claude-opus-5-5", InlineModel: "claude-haiku-4-5"}
	mu, seen := &sync.Mutex{}, &[]string{}
	build := func(c ai.Config) ai.Provider { return modelAIProvider{cfg: c, mu: mu, seen: seen} }
	m, _ := aiIDETestModel(t, build(main))
	s := m.ai
	s.cfg = main
	s.settings = ai.Settings{Models: map[string]ai.ModelSlot{"big": {Provider: ai.KindOpenAI, Model: "gpt-big"}}}
	creds := ai.Credentials{Keys: map[string]string{ai.KindOpenAI: "k"}}
	cfgs, errs := ai.ResolveSlots(s.settings, main, creds, func(string) string { return "" })
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s.slots = ai.NewSlotPool(cfgs, build)
	s.slotKnown = map[string]bool{ai.SlotMain: true, ai.SlotQuick: true, ai.SlotLocal: true, "big": true}
	s.profFake = []ai.Profile{{Name: "pick", Steps: []ai.ProfileStep{{ID: "a", Title: "Answer", Slot: "big"}}}}
	s.reloadProfiles()
	return m, seen
}

func TestAIProfileStepUsesItsSlotModel(t *testing.T) {
	for _, c := range []struct{ profile, model string }{
		{ai.ProfileFast, "claude-haiku-4-5"},
		{"pick", "gpt-big"},
		{ai.ProfileDefault, "claude-opus-5-5"},
	} {
		t.Run(c.profile, func(t *testing.T) {
			m, seen := aiSlotModelChat(t)
			m.ai.profile = c.profile
			driveAIChat(t, m, m.sendAIChat("hi"), nil)
			if len(*seen) != 1 || (*seen)[0] != c.model {
				t.Fatalf("requests went to %v, want [%s]", *seen, c.model)
			}
			if label, _ := m.aiStatus(); !strings.Contains(label, c.model) {
				t.Errorf("status badge %q lacks %s", label, c.model)
			}
			tr := aiTranscriptText(m, 100)
			if !strings.Contains(tr, "AI · "+c.model) {
				t.Errorf("answer header lacks %s:\n%s", c.model, tr)
			}
		})
	}
}
