package app

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// N1: the editor sends a StateMsg up to every 100 ms while the user
// scrolls, and each one used to read and write the session files. Now
// the editor part of the session (open files, active file, cursors,
// expanded folders) is kept in memory by a sessionWriter and written:
//   - at once when the open files, the active file or the expanded
//     folders change (file open / close / focus, explorer toggle);
//   - for cursor-only changes: once the editor is idle for
//     sessionIdleDelay, and at most every sessionMaxDelay while busy;
//   - on quit, on Close, and on hangup / crash (FlushSession).
const (
	sessionIdleDelay = 500 * time.Millisecond
	sessionMaxDelay  = 1500 * time.Millisecond
)

// sessionIdleMsg arrives sessionIdleDelay after a session change; gen
// tells if another change came in between (then a later one flushes).
type sessionIdleMsg struct{ gen uint64 }

// sessionFlushTarget is the writer FlushSession writes (the newest Model).
var sessionFlushTarget atomic.Pointer[sessionWriter]

// FlushSession writes the session changes not on disk yet. cmd/termocode
// calls it on hangup / kill and after a crash, next to HotExitFlush.
func FlushSession() {
	sessionFlushTarget.Load().flush()
}

type sessionWriter struct {
	mu  sync.Mutex
	now func() time.Time // tests replace it

	// Last state seen from the editor: a StateMsg equal to it writes
	// nothing.
	files       []string
	active      string
	expanded    []string
	hasExpanded bool
	cursors     map[string][2]int

	// terms is the terminal part last seen (terminal_restore.go).
	terms    *savedTerminals
	hasTerms bool

	// Changes not on disk yet.
	dirty       bool
	setFiles    bool
	setExpanded bool
	setTerms    bool
	pendCursors map[string][2]int

	gen       uint64
	lastWrite time.Time
	writes    int // number of flushes that wrote (tests)
}

func newSessionWriter() *sessionWriter {
	w := &sessionWriter{now: time.Now, cursors: map[string][2]int{}, pendCursors: map[string][2]int{}}
	sessionFlushTarget.Store(w)
	return w
}

// ensureSessionWriter returns m.sess, made on first use for a Model not
// built by New (tests).
func (m *Model) ensureSessionWriter() *sessionWriter {
	if m.sess == nil {
		m.sess = newSessionWriter()
	}
	return m.sess
}

// persistStateSession records the session part of a StateMsg. Files only
// update on a non-empty snapshot (so closing the last buffer doesn't
// clobber a good list). Expanded folders only after ReadyMsg: before it
// the session has not been restored, and the fresh tree (root only)
// would overwrite the saved expanded folders. Cursors merge (a buffer
// closed outside keeps its last position for a later reopen). The
// returned command is the idle timer, or nil.
func (m *Model) persistStateSession(msg StateMsg) tea.Cmd {
	w := m.ensureSessionWriter()
	paths := make([]string, 0, len(msg.Bufs))
	for _, b := range msg.Bufs {
		if b.Path != "" {
			paths = append(paths, b.Path)
		}
	}
	var expanded []string
	if m.nvimAttached {
		expanded = m.explorer.ExpandedPaths()
	}
	return w.observe(paths, msg.Path, m.nvimAttached, expanded, msg.Cursors)
}

// observe takes one editor state. Unchanged → nothing. A structural
// change writes at once; a cursor-only change waits for idle (or writes
// when the last write is sessionMaxDelay old).
func (w *sessionWriter) observe(files []string, active string, attached bool, expanded []string, cursors map[string][2]int) tea.Cmd {
	w.mu.Lock()
	defer w.mu.Unlock()
	urgent := false
	if len(files) > 0 && (active != w.active || !slices.Equal(files, w.files)) {
		w.files, w.active, w.setFiles = files, active, true
		urgent = true
	}
	if attached && (!w.hasExpanded || !slices.Equal(expanded, w.expanded)) {
		w.expanded, w.hasExpanded, w.setExpanded = expanded, true, true
		urgent = true
	}
	moved := false
	for p, pos := range cursors {
		if old, ok := w.cursors[p]; !ok || old != pos {
			w.cursors[p] = pos
			w.pendCursors[p] = pos
			moved = true
		}
	}
	if !urgent && !moved {
		return nil
	}
	w.dirty = true
	w.gen++
	if urgent || w.now().Sub(w.lastWrite) >= sessionMaxDelay {
		w.flushLocked()
		return nil
	}
	gen := w.gen
	return tea.Tick(sessionIdleDelay, func(time.Time) tea.Msg { return sessionIdleMsg{gen: gen} })
}

// expandedChanged records the explorer's expanded folders and writes now.
func (w *sessionWriter) expandedChanged(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hasExpanded && slices.Equal(paths, w.expanded) {
		return
	}
	w.expanded, w.hasExpanded, w.setExpanded = paths, true, true
	w.dirty = true
	w.flushLocked()
}

// terminalsChanged records the terminal part of the session and writes it
// at once when it differs from the last one.
func (w *sessionWriter) terminalsChanged(t *savedTerminals) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hasTerms && reflect.DeepEqual(t, w.terms) {
		return
	}
	w.terms, w.hasTerms, w.setTerms = t, true, true
	w.dirty = true
	w.flushLocked()
}

// flushIdle writes when no change came after the one that started the
// idle timer gen.
func (w *sessionWriter) flushIdle(gen uint64) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if gen == w.gen {
		w.flushLocked()
	}
}

// flush writes the changes not on disk yet (quit, Close, hangup).
func (w *sessionWriter) flush() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

// flushLocked merges the pending changes into the session on disk: the
// other fields (terminal height, roots, AI chat, …) are written by their
// own helpers, so the file is read once here, never per state change.
func (w *sessionWriter) flushLocked() {
	if !w.dirty {
		return
	}
	s := loadSession()
	if w.setFiles {
		s.OpenFiles = slices.Clone(w.files)
		s.ActivePath = w.active
	}
	if w.setExpanded {
		s.ExpandedDirs = slices.Clone(w.expanded)
	}
	if w.setTerms {
		s.Terminals = w.terms
	}
	if s.Cursors == nil {
		s.Cursors = map[string][2]int{}
	}
	for p, pos := range w.pendCursors {
		s.Cursors[p] = pos
	}
	saveSession(s)
	w.dirty, w.setFiles, w.setExpanded, w.setTerms = false, false, false, false
	clear(w.pendCursors)
	w.lastWrite = w.now()
	w.writes++
}
