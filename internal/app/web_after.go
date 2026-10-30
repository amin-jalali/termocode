package app

import (
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Publishing what changed (WEB-12). The views the browsers mirror (status,
// toasts, chat, cards …) are not pushed from the many places that change
// them: at the end of every Update the topics that someone listens to
// compare the Model with what they sent last and publish the difference.
// Nothing is built while no browser listens, and an Update that changed
// nothing sends nothing. A topic's snapshot sets the same baseline, so
// the deltas always continue the snap the browser has.

// webAfterUpdates are the per-topic publishers, registered by the topic
// files. Each runs on Update and returns a Cmd only when it needs one (a
// one-shot tick of a rate limit).
var webAfterUpdates []func(m *Model, msg tea.Msg) tea.Cmd

// webListens reports whether a browser listens to topic now (or may come
// back to it: a kept resume entry keeps its ring).
func (h *webHub) webListens(topic string) bool { return h.rings[topic] != nil }

// webAfterUpdate runs the publishers; nothing when the hub is off. A
// publisher that panics loses its publish, never the editor.
func (m *Model) webAfterUpdate(msg tea.Msg) tea.Cmd {
	h := m.web
	if h == nil {
		return nil
	}
	// The hub runs in every termocode: while nothing is connected or
	// waiting, the publishers run once more (they forget what they sent)
	// and then not at all, so an unused hub costs nothing per Update.
	idle := h.idle()
	if idle && h.settled {
		return nil
	}
	h.settled = idle
	var cmds []tea.Cmd
	for _, f := range webAfterUpdates {
		if c := m.webAfterOne(f, msg); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// idle: no connection (browser or gateway), no topic, no file view, no
// laptop card waiting and no staged upload.
func (h *webHub) idle() bool {
	if len(h.live) != 0 || len(h.rings) != 0 || h.files != nil || len(h.asks.line) != 0 || h.asks.shown != nil {
		return false
	}
	h.mu.Lock()
	conns := len(h.all)
	h.mu.Unlock()
	h.ups.mu.Lock()
	ups := len(h.ups.by)
	h.ups.mu.Unlock()
	return conns == 0 && ups == 0
}

func (m *Model) webAfterOne(f func(*Model, tea.Msg) tea.Cmd, msg tea.Msg) (cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			recordError(fmt.Sprintf("[web] publish panic: %v", r))
			cmd = nil
		}
	}()
	return f(m, msg)
}

// webShellState is what the shell's topics sent last (Update only).
type webShellState struct {
	status webStatusPub
	toasts webToastsPub
	attn   webAttnState
}

// webApplyReplace is the apply of a topic whose delta is the whole new
// state: the delta must decode as T, and it is the next snapshot.
func webApplyReplace[T any](_, delta json.RawMessage) (json.RawMessage, error) {
	var v T
	if err := json.Unmarshal(delta, &v); err != nil {
		return nil, err
	}
	return delta, nil
}
