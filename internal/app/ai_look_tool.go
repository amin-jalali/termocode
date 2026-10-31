package app

import "github.com/amin-jalali/termocode/internal/ai"

// aiLookTool is look_at (OMX-32) on the main model slot: it is resolved
// now, on the Update goroutine, so the tool never reads the AI state
// from the agent's goroutine.
func (m *Model) aiLookTool(root string) ai.Tool {
	var p ai.Provider
	model, vision := "", false
	if m.ai != nil {
		if sl, err := m.ai.resolveSlot(""); err == nil {
			p, model, vision = sl.p, sl.cfg.Model, ai.SupportsImages(sl.p)
		}
	}
	return ai.LookAtTool(ai.Workspace{Root: root}, func() (ai.Provider, string, bool) { return p, model, vision })
}
