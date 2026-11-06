package worktrees

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ── Agent notes and status checkpoints (WTX-12) ──────────────────────────
//
// Agents leave short notes ("adding the pin row") and a checkpoint
// (working / blocked / done) on the worktree they work in, through the
// CLI (termocode worktree note|status) or the worktree_note tool. A note
// is free text from an agent: one line, control characters dropped,
// redacted, drawn as text only (never run, never a link).

// Limits of the notes.
const (
	MaxNoteLen = 500 // characters of one note
	MaxNotes   = 50  // notes kept per worktree (the oldest go first)
)

// Errors of CleanNote.
var (
	ErrNoteEmpty = errors.New("the note is empty")
	ErrNoteLong  = fmt.Errorf("the note is over %d characters", MaxNoteLen)
)

// Note is one note with the time it was left.
type Note struct {
	Text string    `json:"text"`
	At   time.Time `json:"at,omitzero"`
}

// UnmarshalJSON reads a note, or a plain string (an older file).
func (n *Note) UnmarshalJSON(b []byte) error {
	if b = bytes.TrimSpace(b); len(b) > 0 && b[0] == '"' {
		*n = Note{}
		return json.Unmarshal(b, &n.Text)
	}
	type plain Note
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*n = Note(p)
	return nil
}

// CleanNote makes text one line of plain text: newlines and tabs become
// spaces, other control characters (escape sequences) are dropped, runs
// of spaces are one, then redact runs. Empty or over MaxNoteLen is an
// error.
func CleanNote(text string, redact func(string) string) (string, error) {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r) || r == utf8.RuneError:
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if redact != nil {
		s = redact(s)
	}
	switch {
	case s == "":
		return "", ErrNoteEmpty
	case utf8.RuneCountInString(s) > MaxNoteLen:
		return "", ErrNoteLong
	}
	return s, nil
}

// AddNote appends a (clean) note; only the newest MaxNotes are kept.
func (m *Meta) AddNote(text string, at time.Time) {
	m.Notes = append(m.Notes, Note{Text: text, At: at})
	if n := len(m.Notes); n > MaxNotes {
		m.Notes = append([]Note(nil), m.Notes[n-MaxNotes:]...)
	}
}

// LastNote is the newest note.
func (m Meta) LastNote() (Note, bool) {
	if len(m.Notes) == 0 {
		return Note{}, false
	}
	return m.Notes[len(m.Notes)-1], true
}

// Checkpoints an agent sets on its worktree.
const (
	CheckWorking = "working"
	CheckBlocked = "blocked"
	CheckDone    = "done"
)

// Checkpoints lists them in order.
var Checkpoints = []string{CheckWorking, CheckBlocked, CheckDone}

// ValidCheckpoint reports one of Checkpoints.
func ValidCheckpoint(s string) bool {
	for _, c := range Checkpoints {
		if s == c {
			return true
		}
	}
	return false
}

// SetCheckpoint sets the checkpoint; a bad one changes nothing.
func (m *Meta) SetCheckpoint(state string, at time.Time) error {
	if !ValidCheckpoint(state) {
		return fmt.Errorf("status must be one of %s", strings.Join(Checkpoints, ", "))
	}
	m.Checkpoint, m.CheckpointAt = state, at
	return nil
}

// ── Parent / child worktrees (WTX-11) ────────────────────────────────────

// SetParent makes parent the parent of child in metas; a parent that is
// the child or one of its descendants (a cycle) is refused.
func SetParent(metas map[string]Meta, child, parent string) error {
	if parent == child {
		return errors.New("a worktree cannot be its own parent")
	}
	seen := map[string]bool{child: true}
	for p := parent; p != ""; p = metas[p].Parent {
		if seen[p] {
			return errors.New("that parent would make a loop of worktrees")
		}
		seen[p] = true
	}
	md := metas[child]
	md.Parent = parent
	metas[child] = md
	return nil
}

// Nest orders a list so each child follows its parent (depth one more).
// parents[i] is the parent path of paths[i] ("" for none). A child whose
// parent is not in the list, or who is part of a loop, is a root. Roots
// and siblings keep the list's order.
func Nest(paths, parents []string) (order, depth []int) {
	idx := make(map[string]int, len(paths))
	for i, p := range paths {
		idx[p] = i
	}
	parentOf := func(i int) int {
		if j, ok := idx[parents[i]]; ok && j != i {
			return j
		}
		return -1
	}
	root := make([]bool, len(paths))
	for i := range paths {
		seen := map[int]bool{i: true}
		j := parentOf(i)
		for j >= 0 && !seen[j] {
			seen[j] = true
			j = parentOf(j)
		}
		root[i] = parentOf(i) < 0 || j == i // j == i: i is in a loop
	}
	kids := make([][]int, len(paths))
	for i := range paths {
		if !root[i] {
			kids[parentOf(i)] = append(kids[parentOf(i)], i)
		}
	}
	var walk func(i, d int)
	walk = func(i, d int) {
		order, depth = append(order, i), append(depth, d)
		for _, k := range kids[i] {
			walk(k, d+1)
		}
	}
	for i := range paths {
		if root[i] {
			walk(i, 0)
		}
	}
	return order, depth
}
