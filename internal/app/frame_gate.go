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
		g.skip = false
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
	g.frame, g.at, g.skip = draw(), time.Now(), false
	return g.frame
}
