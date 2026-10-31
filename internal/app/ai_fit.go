package app

import "github.com/amin-jalali/termocode/internal/ai"

// aiContextWindow is model's context window in tokens (ai_context_windows
// over the built-in table; 0 unknown).
func aiContextWindow(st ai.Settings, model string) int {
	w, _ := ai.NewContextWindows(st.ContextWindows).Lookup(model)
	return w
}
