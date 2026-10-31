package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/preview"
	tea "github.com/charmbracelet/bubbletea"
)

// This report uses the same slot resolution as runs. It reads config only:
// no provider request, key reveal, token refresh, or CLI login probe.
func (s *aiState) aiDoctorReport() string {
	var b strings.Builder
	b.WriteString("Model order: step provider/model → step slot → profile category → quick or main.\nKey state is local only; it does not check the server.\nFallback order: none. This build uses one model per slot.\n")
	for _, name := range s.profiles.Names() {
		p, _ := s.profiles.Get(name)
		fmt.Fprintf(&b, "\n%s (%s)\n", p.Name, p.Source)
		steps := append([]ai.ProfileStep(nil), p.Steps...)
		if p.Loop != nil {
			steps = append(steps, p.Loop.Fix)
		}
		for _, st := range steps {
			if st.CLI != "" {
				model := st.Model
				if model == "" {
					model = "default"
				}
				fmt.Fprintf(&b, "  %s → %s · slot: external · key: CLI login (not checked)\n", st.ID, model)
				continue
			}
			slot := p.SlotFor(st)
			cfg, ok := s.aiSlotConfig(slot)
			if !ok {
				fmt.Fprintf(&b, "  %s → slot: %s · not found\n", st.ID, slot)
				continue
			}
			key := "missing"
			switch {
			case cfg.Kind == ai.KindOllama || cfg.Kind == ai.KindLMStudio:
				key = "not needed"
			case cfg.Token != nil:
				key = "sign-in saved (not checked)"
			case cfg.APIKey != "":
				key = "present"
			case s.secretsLocked:
				key = "store locked"
			case cfg.Kind == ai.KindCompatible:
				key = "not set (may be optional)"
			}
			fmt.Fprintf(&b, "  %s → %s · provider: %s · slot: %s · key: %s\n", st.ID, cfg.Model, cfg.Kind, slot, key)
		}
	}
	if len(s.profiles.Errors) > 0 {
		b.WriteString("\nConfig notes:\n" + strings.Join(s.profiles.Errors, "\n"))
	}
	return ai.Redact(aiCleanText(b.String()))
}

func (m *Model) openAIDoctor() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	m.preview = preview.New(" AI Doctor ", s.aiDoctorReport())
	m.preview.SetHints("↑↓ scroll · Esc close · ? help")
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
	return nil
}
