package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
)

// aiMCPTrustAnnotations is mcp_trust_annotations.
func (m *Model) aiMCPTrustAnnotations() bool {
	return mcp.LoadSettings(mcpConfigPath()).TrustAnnotations
}

// wfxAIEnv is what workflow plan and ai steps run with: the quick and
// deep slots of the active profile, resolved now on the UI goroutine.
// The steps get no IDE tools in this version (they answer with text).
func (m *Model) wfxAIEnv() aistep.Env {
	s := m.aiS()
	slots := map[string]aiSlot{}
	cats := map[string]string{}
	if prof, cm, err := s.aiProfileCategories(s.activeProfile()); err == nil {
		for name, slot := range aiCategorySlots(prof, cm) {
			if name != agentdag.CategoryQuick && name != agentdag.CategoryDeep {
				continue
			}
			if sl, err := s.resolveSlot(slot); err == nil {
				slots[slot], cats[name] = sl, slot
			}
		}
	}
	if sl, err := s.resolveSlot(ai.SlotMain); err == nil {
		slots[ai.SlotMain] = sl
	}
	pricing := ai.NewPricing(s.settings.Prices)
	maxSteps := s.settings.MaxSteps
	return aistep.Env{
		Base: agentrun.Options{
			StallAfter: s.settings.StepStall(),
			Cost: func(n agentdag.Node, u ai.Usage) (float64, bool) {
				return pricing.Cost(slots[n.Slot].cfg.Kind, u)
			},
			Build: func(_ context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
				slot := n.Slot
				if slot == "" {
					slot = ai.SlotMain
				}
				sl, ok := slots[slot]
				if !ok || sl.p == nil {
					return nil, nil, fmt.Errorf("model slot %q is not set up (Itermoc → AI Providers)", slot)
				}
				sys := "You are termocode's AI, running one step of a workflow. Do the task and answer with the result as text."
				return &ai.Agent{Provider: sl.p, Native: sl.p.SupportsTools(), MaxSteps: maxSteps},
					[]ai.Message{{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: input}}, nil
			},
		},
		Profile:    s.profiles.Get,
		Categories: cats,
	}
}

// spawnOutput runs argv in dir and returns its output (capped) and exit
// code; a command that cannot start is an error.
func spawnOutput(ctx context.Context, argv []string, dir string) (string, int, error) {
	if len(argv) == 0 {
		return "", 0, errors.New("empty argv")
	}
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := out.String()
	if len(text) > 64<<10 {
		text = text[len(text)-64<<10:]
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return text, ee.ExitCode(), nil
	}
	return text, 0, err
}

// walkSteps calls fn with the type of every step, nested ones too.
func walkSteps(steps []workflow.Step, fn func(typ string)) {
	workflow.Walk(steps, func(s *workflow.Step) bool { fn(s.Type); return true })
}
