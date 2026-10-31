package app

import "github.com/amin-jalali/termocode/internal/ai"

// ── Background runs (OMX-6) ───────────────────────────────────────────

// aiBgRun is a run the AI started that runs beside the chat.
type aiBgRun struct {
	id, chat string
	// user and msg are the run's task and run message. They are held
	// here while the chat turn that started the run goes, and copied
	// into the chat when it is idle (inConv); msg stays the truth.
	user, msg ai.StoredMessage
	inConv    bool
}

// agentsHeld are the open chat's background runs that are not in the
// chat yet (AGENTS shows them after the chat's runs).
func (s *aiState) agentsHeld() []*ai.StoredMessage {
	var out []*ai.StoredMessage
	for _, b := range s.bgRuns {
		if !b.inConv && b.chat == s.conv.ID {
			out = append(out, &b.msg)
		}
	}
	return out
}
