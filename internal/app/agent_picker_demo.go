package app

import (
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// agentPickerDemo is Agent: Start… with made-up programs on PATH and
// sign-in states (DevActionMsg "agentpickerdemo", for the goldens; no
// CLI runs and PATH is not read).
func (m *Model) agentPickerDemo() {
	in := m.agentPickInputNow()
	on := map[string]bool{"claude": true, "codex": true, "gemini": true, "opencode": true}
	in.installed = func(a agents.Agent) bool { return on[a.ID] }
	in.cli = map[string]profileform.CLIState{ai.CLIClaude: profileform.CLIReady, ai.CLICodex: profileform.CLILoggedOut}
	m.picker = picker.NewItems(" Start an agent ", agentPickerItems(in))
	m.picker.SetNote([]string{agentPickerNote})
	m.picker.SetWidth(agentPickerWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentPicker
}
