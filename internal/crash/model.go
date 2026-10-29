package crash

import (
	"fmt"
	"runtime/debug"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// cmdWhere names a command panic in the toast.
const cmdWhere = "a background task"

// wrapState is shared by every copy of the wrapper (Bubble Tea passes
// models by value).
type wrapState struct {
	mu      sync.Mutex
	dead    bool
	last    string // last good frame
	onFatal func(path string)
}

type wrapped struct {
	m tea.Model
	s *wrapState
}

// Wrap puts m behind a guard. It records the type of every message for
// reports and guards every command m returns (GuardCmd). A panic in
// Init, Update or View is fatal: the report is written and marked for
// the next start (MarkFatal), onFatal gets its path once (it ends the
// program: the hot-exit flush and the terminal restore follow there), and
// m is never called again: Update ignores messages and View shows the
// last good frame. Without onFatal, Update returns tea.Quit.
func Wrap(m tea.Model, onFatal func(path string)) tea.Model {
	return wrapped{m: m, s: &wrapState{onFatal: onFatal}}
}

// fatal handles a panic in the UI loop; it reports whether the caller
// should quit by itself (no onFatal).
func (w wrapped) fatal(where string, r any) bool {
	w.s.mu.Lock()
	first := !w.s.dead
	w.s.dead = true
	w.s.mu.Unlock()
	if !first {
		return w.s.onFatal == nil
	}
	path, err := WriteReport(where, r, debug.Stack())
	if err == nil {
		_ = MarkFatal(path)
	}
	if w.s.onFatal != nil {
		w.s.onFatal(path)
		return false
	}
	return true
}

func (w wrapped) isDead() bool {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	return w.s.dead
}

func (w wrapped) Init() (cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			cmd = nil
			if w.fatal("init", r) {
				cmd = tea.Quit
			}
		}
	}()
	return GuardCmd(cmdWhere, w.m.Init())
}

func (w wrapped) Update(msg tea.Msg) (out tea.Model, cmd tea.Cmd) {
	if w.isDead() {
		return w, nil
	}
	RecordEvent(fmt.Sprintf("%T", msg))
	defer func() {
		if r := recover(); r != nil {
			out, cmd = w, nil
			if w.fatal("update", r) {
				cmd = tea.Quit
			}
		}
	}()
	m, c := w.m.Update(msg)
	return wrapped{m: m, s: w.s}, GuardCmd(cmdWhere, c)
}

func (w wrapped) View() (v string) {
	if w.isDead() {
		return w.lastFrame()
	}
	defer func() {
		if r := recover(); r != nil {
			w.fatal("view", r)
			v = w.lastFrame()
		}
	}()
	v = w.m.View()
	w.s.mu.Lock()
	w.s.last = v
	w.s.mu.Unlock()
	return v
}

func (w wrapped) lastFrame() string {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	return w.s.last
}
