package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// frameInterval is the shortest time between two full redraws (60/s, the
// rate Bubble Tea flushes frames to the terminal anyway).
const frameInterval = 16 * time.Millisecond

// frameGate makes View draw at most once per frame. Bubble Tea calls View
// after every message, and one held key brings several (the key, nvim
// redraws, state replies); drawing the whole screen for each used most of
// a CPU core, so keys queued up and the cursor kept moving after the key
// was let go. Messages inside a frame only update the state; a
// frameTickMsg at the end of the frame draws the latest state.
//
// It is shared by every copy of the Model (a pointer) and is on only in
// the real program (ThrottleFrames): tests call Update then View and
// expect the new frame at once.
type frameGate struct {
	on    bool
	frame string    // the last frame drawn
	at    time.Time // when it was drawn
	skip  bool      // View returns frame as is
	tick  bool      // a frameTickMsg is on its way
	// free: the last draw gave the same frame, so the next message may
	// draw inside the interval; freeUsed: this draw is that one.
	free, freeUsed bool
	updated        bool // the next View follows an Update (not a repeat call)
}

type frameTickMsg struct{}

var throttleFrames bool

// ThrottleFrames turns the frame gate on for models made by New after
// this call. cmd/termocode calls it before starting the program.
func ThrottleFrames() { throttleFrames = true }

// gateUpdate runs after every Update: inside a frame it holds the redraw
// back and makes sure a tick will draw the latest state.
func (m Model) gateUpdate(msg tea.Msg, cmd tea.Cmd) tea.Cmd {
	g := m.gate
	if g == nil || !g.on {
		return cmd
	}
	if _, ok := msg.(frameTickMsg); ok {
		g.tick, g.skip = false, false
		return cmd
	}
	since := time.Since(g.at)
	if g.frame == "" || since >= frameInterval || drawAtOnce(msg) {
		g.skip, g.updated = false, true
		return cmd
	}
	if g.free {
		g.free, g.freeUsed, g.skip, g.updated = false, true, false, true
		return cmd
	}
	g.skip = true
	if !g.tick {
		g.tick = true
		cmd = tea.Batch(cmd, tea.Tick(frameInterval-since, func(time.Time) tea.Msg { return frameTickMsg{} }))
	}
	return cmd
}

// drawAtOnce: messages whose frame must not wait for the tick. A
// terminal change (a session client attached) marks its first frame for
// the server (session.AttachedSeq); the server sends the new client only
// what follows the mark, so the full repaint after it must come in that
// same frame, not one tick later. A resize repaints the whole screen.
func drawAtOnce(msg tea.Msg) bool {
	switch msg.(type) {
	case TerminalChangedMsg, tea.WindowSizeMsg:
		return true
	}
	return false
}

// gatedView returns the last frame while the gate holds, else draws.
func (m Model) gatedView(draw func() string) string {
	g := m.gate
	if g == nil || !g.on {
		return draw()
	}
	if g.skip && g.frame != "" {
		return g.frame
	}
	// A draw that gives the same frame sent nothing to the terminal: the
	// next message (nvim's redraw right after the key that caused it) may
	// draw at once instead of waiting for the tick, once per interval so
	// a burst of messages that change nothing is still held (PERF-2).
	f := draw()
	g.free = g.updated && f == g.frame && !g.freeUsed
	g.freeUsed, g.updated = false, false
	g.frame, g.at, g.skip = f, time.Now(), false
	return g.frame
}
