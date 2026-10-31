package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Model fallback in the chat (OMX-4, ai/fallback.go): the agent of a
// turn gets the slot's backup models; when it moves to one, the chat
// shows a "↪ … Now using …" line (not sent to the model).

// aiFallbackMark starts the chat line of a fallback.
const aiFallbackMark = "↪ "

// aiSlotLabel is a slot's name in the fallback line: "gpt (gpt-5)".
func aiSlotLabel(name string, cfg ai.Config) string {
	if name == "" {
		name = ai.SlotMain
	}
	if cfg.Model == "" || strings.HasSuffix(name, "/"+cfg.Model) {
		return name
	}
	return name + " (" + cfg.Model + ")"
}

// aiFallbacks are the backup models of slot name that can run (a slot
// that does not resolve or has no key is left out).
func (s *aiState) aiFallbacks(name string) []ai.Fallback {
	var out []ai.Fallback
	for _, f := range s.settings.FallbackNames(name) {
		sl, err := s.resolveSlot(f)
		if err != nil {
			continue
		}
		out = append(out, ai.Fallback{Name: aiSlotLabel(f, sl.cfg), Provider: sl.p, Model: sl.cfg.Model})
	}
	return out
}

// aiFallbackLine reports whether a stored tool line is a fallback line.
func aiFallbackLine(text string) bool { return strings.HasPrefix(text, aiFallbackMark) }
