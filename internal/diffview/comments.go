package diffview

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Review comments (CMP-51): the user leaves a short note on a change
// (c), the card shows it under the change header, and S hands every
// note of the card to the app in one batch, to send to an agent. The
// model only keeps the notes; asking for the text and sending are the
// app's.

// Comment is the note on one change of a file.
type Comment struct {
	File  int    // file index on the card
	Path  string // the file's display name
	Hunk  int    // change index in the file
	Lines string // "line 13", "lines 4–9", "removes line 7"
	Code  string // the change as diff lines ("-old" / "+new"), capped
	Text  string
}

// CommentAskMsg asks the app for the text of the comment on a change.
// Text is the comment it has now ("" for a new one); Where names the
// change for the prompt ("x.go · change 2 of 3 · line 13").
type CommentAskMsg struct {
	File, Hunk int
	Text       string
	Where      string
}

// SendCommentsMsg asks the app to send the card's comments (S).
type SendCommentsMsg struct{}

// commentCodeMax caps the code lines of one comment.
const commentCodeMax = 12

// SetComment sets the comment on change hunk of file; an empty text
// removes it.
func (m *Model) SetComment(file, hunk int, text string) {
	if file < 0 || file >= len(m.files) {
		return
	}
	f := m.files[file]
	if hunk < 0 || hunk >= max(len(f.diff.Hunks), 1) {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		delete(f.notes, hunk)
	} else {
		if f.notes == nil {
			f.notes = map[int]string{}
		}
		f.notes[hunk] = text
	}
	m.cache.reset()
}

// Comment is the comment on change hunk of file ("" when none).
func (m Model) Comment(file, hunk int) string {
	if file < 0 || file >= len(m.files) {
		return ""
	}
	return m.files[file].notes[hunk]
}

// CommentCount is the number of comments on the card.
func (m Model) CommentCount() int {
	n := 0
	for _, f := range m.files {
		n += len(f.notes)
	}
	return n
}

// ClearComments drops every comment (after they were sent).
func (m *Model) ClearComments() {
	for _, f := range m.files {
		f.notes = nil
	}
	m.cache.reset()
}

// Comments lists the comments in file and change order.
func (m Model) Comments() []Comment {
	var out []Comment
	for i, f := range m.files {
		hs := make([]int, 0, len(f.notes))
		for h := range f.notes {
			hs = append(hs, h)
		}
		sort.Ints(hs)
		for _, h := range hs {
			c := Comment{File: i, Path: f.Name, Hunk: h, Text: f.notes[h]}
			if h < len(f.diff.Hunks) {
				c.Lines = hunkLines(f.diff.Hunks[h])
				c.Code = hunkCode(f, h)
			} else {
				c.Lines = "the final newline"
			}
			out = append(out, c)
		}
	}
	return out
}

// hunkCode is change h as "-old" / "+new" lines, capped.
func hunkCode(f *fileState, h int) string {
	hk := f.diff.Hunks[h]
	var b strings.Builder
	n := 0
	for k := hk.First; k < hk.End && k < len(f.diff.Rows); k++ {
		r := f.diff.Rows[k]
		mark := "+"
		if r.OldLine > 0 && r.NewLine == 0 {
			mark = "-"
		} else if r.OldLine > 0 && r.NewLine > 0 {
			mark = " "
		}
		if n == commentCodeMax {
			fmt.Fprintf(&b, "… %d more lines\n", hk.End-k)
			break
		}
		b.WriteString(mark + r.Text + "\n")
		n++
	}
	return strings.TrimRight(b.String(), "\n")
}

// askComment asks for the comment on the focused change. Commit-mode
// cards (header) have their own keys and owner, so they take none yet.
func (m *Model) askComment() tea.Cmd {
	f := m.file()
	if m.header != nil || f == nil || f.Pending || f.Binary || f.Note != "" || len(f.dec) == 0 && len(f.diff.Hunks) == 0 {
		return nil
	}
	h := f.focus
	where := fmt.Sprintf("%s · change %d of %d", f.Name, h+1, max(len(f.diff.Hunks), 1))
	if h < len(f.diff.Hunks) {
		where += " · " + hunkLines(f.diff.Hunks[h])
	}
	msg := CommentAskMsg{File: m.cur, Hunk: h, Text: f.notes[h], Where: where}
	return func() tea.Msg { return msg }
}

// sendComments asks the app to send the comments (none: nothing).
func (m *Model) sendComments() tea.Cmd {
	if m.header != nil || m.CommentCount() == 0 {
		return nil
	}
	return func() tea.Msg { return SendCommentsMsg{} }
}

// remapNotes moves the comments of old onto the changes of nf (the same
// file diffed again): each goes to the change that starts nearest to
// where its change started in the new text.
func remapNotes(old, nf *fileState) {
	if len(old.notes) == 0 {
		return
	}
	nf.notes = map[int]string{}
	for h, text := range old.notes {
		to := 0
		if h < len(old.diff.Hunks) && len(nf.diff.Hunks) > 0 {
			at := old.diff.Hunks[h].NewStart
			best := -1
			for k, hk := range nf.diff.Hunks {
				if d := abs(hk.NewStart - at); best < 0 || d < best {
					best, to = d, k
				}
			}
		}
		if prev, ok := nf.notes[to]; ok {
			text = prev + "\n" + text
		}
		nf.notes[to] = text
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// FormatComments is the comments as one message for an agent: what the
// changes are, then each comment with its file, lines and code.
func FormatComments(title string, cs []Comment) string {
	var b strings.Builder
	word := "comments"
	if len(cs) == 1 {
		word = "comment"
	}
	fmt.Fprintf(&b, "%d review %s on the diff", len(cs), word)
	if title != "" {
		fmt.Fprintf(&b, " \"%s\"", title)
	}
	b.WriteString(". Please address each one in the code, then say what you changed.\n")
	for i, c := range cs {
		fmt.Fprintf(&b, "\n%d. %s (%s)\n", i+1, c.Path, c.Lines)
		if c.Code != "" {
			b.WriteString("```diff\n" + c.Code + "\n```\n")
		}
		b.WriteString(c.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
