package app

import (
	"context"
	"strings"
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

// aiDemoSlots are the two demo models: main is claude-sonnet-4-5 and
// quick is gpt-5-mini (every demo screen uses the same two, so the
// numbers agree between screens).
func aiDemoSlots(main, quick ai.Provider) map[string]aiSlot {
	return map[string]aiSlot{
		ai.SlotMain:  {p: main, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
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
	s.slotFake = aiDemoSlots(nil, nil) // the models the picker shows for each step
	s.reloadProfiles()
	s.profile = ai.ProfileReview
	switch arg {
	case "picker":
		return m.openAIProfilePicker()
	case "trust":
		return m.askAIProfileTrust(house, aiTrustAfterSwitch, "")
	case "save":
		return m.askAIProfileSave(ai.Profile{Name: "my-review", Title: "Review then fix", Steps: ai.BuiltinProfiles()[1].Steps}, false)
	}
	return nil
}

// aiDemoFuncProvider answers each request with reply(request); with
// hold=true the answer streams its text and then waits until the run is
// stopped (a step that is still working). Safe for parallel steps.
type aiDemoFuncProvider struct {
	name  string
	reply func(ai.Request) (ai.Chunk, bool)
}

func (p *aiDemoFuncProvider) Name() string        { return p.name }
func (p *aiDemoFuncProvider) Configured() bool    { return true }
func (p *aiDemoFuncProvider) SupportsTools() bool { return true }
func (p *aiDemoFuncProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	r, hold := p.reply(req)
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		if r.Delta != "" {
			ch <- ai.Chunk{Delta: r.Delta}
		}
		if hold {
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls, Usage: r.Usage}
	}()
	return ch
}

// aiDemoDiamondPlan is the planned run of the AGENTS shots: one map
// step, two steps that use its result side by side, one check of both.
const aiDemoDiamondPlan = "```json\n" + `{"nodes": [
  {"id": "map", "title": "Map the parser", "prompt": "TASK-map: list the parser's hot paths.", "category": "quick"},
  {"id": "lexer", "title": "Faster lexer", "prompt": "TASK-lexer: speed up the lexer.", "category": "quick", "deps": ["map"]},
  {"id": "cache", "title": "Token cache", "prompt": "TASK-cache: cache tokens.", "category": "quick", "deps": ["map"]},
  {"id": "verify", "title": "Verify", "prompt": "TASK-verify: run the benchmarks.", "category": "deep", "deps": ["lexer", "cache"]}
]}` + "\n```"

// aiDemoTask is the planned task id in a request ("" for none).
func aiDemoTask(r ai.Request) string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == ai.RoleUser {
			c := r.Messages[i].Content
			if j := strings.Index(c, "TASK-"); j >= 0 {
				return strings.SplitN(c[j+5:], ":", 2)[0]
			}
			return ""
		}
	}
	return ""
}

// aiAgentsDemo starts a planned diamond run on scripted models (dev
// action "agentsdemo:running|done") and shows it in the AGENTS view:
// "running" leaves the two middle steps working, "done" lets the run
// finish.
func (m *Model) aiAgentsDemo(arg string) tea.Cmd {
	s := m.aiS()
	running := arg == "running"
	use := func(model string, in, out int) *ai.Usage {
		return &ai.Usage{Model: model, Input: in, Output: out, Requests: 1, PeakContext: in}
	}
	toolDone := func(r ai.Request) bool { return r.Messages[len(r.Messages)-1].Role == ai.RoleTool }
	deep := &aiDemoFuncProvider{name: "anthropic", reply: func(r ai.Request) (ai.Chunk, bool) {
		if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "You are the planner") {
			return ai.Chunk{Delta: aiDemoDiamondPlan, Usage: use("claude-sonnet-4-5", 2400, 260)}, false
		}
		return ai.Chunk{Delta: "Both changes hold: the parser benchmark is 31 % faster and every test passes.",
			Usage: use("claude-sonnet-4-5", 5200, 120)}, false
	}}
	quick := &aiDemoFuncProvider{name: "openai", reply: func(r ai.Request) (ai.Chunk, bool) {
		switch aiDemoTask(r) {
		case "map":
			if !toolDone(r) {
				return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "m1", Name: "read_file", Args: map[string]any{"path": "README.md"}}},
					Usage: use("gpt-5-mini", 1500, 40)}, false
			}
			return ai.Chunk{Delta: "The lexer and the token lookups take most of the time.", Usage: use("gpt-5-mini", 2600, 90)}, false
		case "lexer":
			if running {
				return ai.Chunk{Delta: "The lexer copies every token; I will switch it to slices of the input so that"}, true
			}
			return ai.Chunk{Delta: "The lexer now slices the input instead of copying.", Usage: use("gpt-5-mini", 3100, 150)}, false
		case "cache":
			if !toolDone(r) {
				return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "README.md"}}},
					Usage: use("gpt-5-mini", 1400, 30)}, false
			}
			if running {
				return ai.Chunk{}, true
			}
			return ai.Chunk{Delta: "Token lookups go through a small cache now.", Usage: use("gpt-5-mini", 2900, 110)}, false
		}
		return ai.Chunk{Delta: "?"}, false
	}}
	s.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: deep, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	s.profile = ai.ProfilePlan
	cmd := m.sendAIChat("make the parser faster")
	return tea.Batch(cmd, m.showAgents(""))
}
