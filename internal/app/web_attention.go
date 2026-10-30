package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Attention events (WEB-12): one-shot "look here" signals to every
// browser, from the Model's own state changes (an AI run ends, a task's
// terminal finishes, a card waits), never from bytes a program printed.
// The page badges its title and tab bar; the words are its own fixed
// texts per kind.

func init() {
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishAttention)
}

// webAttnState is what the attention events saw last (Update only).
type webAttnState struct {
	runID    string       // the live run of the open chat
	runState string       // and its state
	tasks    map[int]bool // task tabs by buffer: running
}

// webPublishAttention looks for the changes that ask for attention.
func (m *Model) webPublishAttention(tea.Msg) tea.Cmd {
	h := m.web
	a := &h.shell.attn
	if m.ai != nil {
		if rm := m.ai.aiRunMessage(); rm != nil {
			id, st := rm.Run.ID, rm.Run.State
			if id == a.runID && a.runState != st && webRunLive(a.runState) {
				switch st {
				case ai.RunStateDone:
					h.attention(webproto.AttentionRunDone, id)
				case ai.RunStateFailed, ai.RunStateCancelled, ai.RunStateInterrupted:
					h.attention(webproto.AttentionRunFailed, id)
				}
			}
			a.runID, a.runState = id, st
		}
	}
	if len(a.tasks) == 0 && !webHasTaskTab(m.terminalTabs) {
		return nil
	}
	next := map[int]bool{}
	for _, t := range m.terminalTabs {
		if t.Task == "" {
			continue
		}
		if was, seen := a.tasks[t.BufID]; seen && was && !t.Running {
			h.attention(webproto.AttentionTaskDone, t.Task)
		}
		next[t.BufID] = t.Running
	}
	a.tasks = next
	return nil
}

func webRunLive(state string) bool {
	return state == ai.RunStateRunning || state == ai.RunStatePaused
}

func webHasTaskTab(tabs []terminalTab) bool {
	for _, t := range tabs {
		if t.Task != "" {
			return true
		}
	}
	return false
}

// attention sends one attention event to every browser (Update only).
func (h *webHub) attention(kind, id string) {
	if len(h.live) == 0 {
		return
	}
	data, err := webData(webproto.Attention{Kind: kind, ID: id})
	if err != nil {
		recordError("[web] attention: " + err.Error())
		return
	}
	for c := range h.live {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAttention, Data: data})
	}
}
