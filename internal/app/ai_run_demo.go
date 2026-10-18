package app

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// The agent pipeline shots (dev action "aipipedemo:running|done"): the
// built-in "review" profile runs on two scripted providers, so the
// golden screens show real step cards with no network.

// aiDemoProvider replays one reply per request (the last one repeats).
// With hold, the last reply streams its text and then waits until the
// run is stopped (a step that is still writing).
type aiDemoProvider struct {
	name    string
	replies []ai.Chunk
	hold    bool

	mu sync.Mutex
	n  int
}

func (p *aiDemoProvider) Name() string        { return p.name }
func (p *aiDemoProvider) Configured() bool    { return true }
func (p *aiDemoProvider) SupportsTools() bool { return true }
func (p *aiDemoProvider) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	i := min(p.n, len(p.replies)-1)
	p.n++
	p.mu.Unlock()
	r := p.replies[i]
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		if r.Delta != "" {
			ch <- ai.Chunk{Delta: r.Delta}
		}
		if p.hold && i == len(p.replies)-1 {
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls, Usage: r.Usage}
	}()
	return ch
}

// aiPipelineDemo starts the demo run; "running" leaves the second step
// writing, "done" lets it finish.
func (m *Model) aiPipelineDemo(arg string) tea.Cmd {
	s := m.aiS()
	rev := &aiDemoProvider{name: "anthropic", replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "d1", Name: "read_file", Args: map[string]any{"path": "README.md"}}},
			Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 1800, Output: 60, Requests: 1, PeakContext: 1800}},
		{Delta: "1. `quit.go:42` shows the confirm card twice when the terminal tab is busy.\n2. The test does not cover a busy tab.",
			Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 2100, Output: 140, Requests: 1, PeakContext: 2100}},
	}}
	fix := &aiDemoProvider{name: "openai", hold: arg == "running", replies: []ai.Chunk{
		{Delta: "I fixed the double confirm in `quit.go`: the card is now shown once, and I added a test for a busy terminal tab.",
			Usage: &ai.Usage{Model: "gpt-5-mini", Input: 9000, Output: 800, Requests: 1, PeakContext: 9000}},
	}}
	if arg == "running" {
		fix.replies[0].Delta = "The reviewer found two problems. I will fix the double confirm first, so I will also update the test that"
	}
	s.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: rev, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: fix, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	s.profile = ai.ProfileReview
	return m.sendAIChat("review my last commit and fix what you find")
}

// aiProfileDemo sets up the profile UI shots (dev action
// "profiledemo:chip|picker|trust"): a sample chat with the "review"
// profile active, the profile picker with an untrusted project profile,
// or that profile's trust card.
func (m *Model) aiProfileDemo(arg string) tea.Cmd {
	s := m.aiS()
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "Why does the toast slide out twice?"},
		{Role: ai.StoredAssistant, Text: aiDemoAnswer},
	}
	s.scroll = 0
	house := ai.Profile{Name: "house-review", Title: "House style review", Source: ai.ProfileSourceProject,
		Steps: []ai.ProfileStep{
			{ID: "style", Title: "Style check", Category: agentdag.CategoryQuick,
				System: "Check the change against docs/STYLE.md. List each problem with file:line.\nDo not edit files.",
				Tools:  agentdag.ToolRule{Preset: agentdag.PresetReadOnly}},
			{ID: "fix", Title: "Fixer", System: "Fix the problems the style check listed. Keep the changes small.",
				Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}},
		}}
	s.profFake = []ai.Profile{house}
	s.reloadProfiles()
	s.profile = ai.ProfileReview
	switch arg {
	case "picker":
		return m.openAIProfilePicker()
	case "trust":
		return m.askAIProfileTrust(house, aiTrustAfterSwitch, "")
	}
	return nil
}
