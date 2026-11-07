package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// CMP-62: rewind the chat to any turn. "AI: Rewind Chat to Turn…" lists
// your questions; picking one asks with a card (Cancel first) and then
// puts back the files of every AI turn from that question on, newest
// first, with the turns' restore points (ai_restore.go). "Code + chat"
// also drops the messages from that question on and puts the question
// back in the input, ready to edit; "Code only" keeps the chat.
//
// Outside CLI agents' turns are rolled back with code checkpoints
// instead (checkpoints_ui.go).

const (
	pickerKindAIRewind  pickerKindEnum  = 6262
	confirmKindAIRewind confirmKindEnum = 6262
)

// aiRewindTurns are the restore points of the turns from user message
// msg on, newest first.
func (s *aiState) aiRewindTurns(msg int) []*aiRestoreTurn {
	p := s.aiRestoreLoad()
	var out []*aiRestoreTurn
	for i := len(p.Turns) - 1; i >= 0; i-- {
		if p.Turns[i].Msg >= msg {
			out = append(out, &p.Turns[i])
		}
	}
	return out
}

// aiRewindFiles is how many different files a rewind to msg puts back.
func (s *aiState) aiRewindFiles(msg int) int {
	seen := map[string]bool{}
	for _, t := range s.aiRewindTurns(msg) {
		for _, f := range t.Files {
			seen[f.Path] = true
		}
	}
	return len(seen)
}

// aiRewindItems are the picker rows: your questions, newest first.
func aiRewindItems(m *Model) []picker.Item {
	s := m.aiS()
	limit := aiForkLimit(s)
	num := 0
	var items []picker.Item
	for i, msg := range s.conv.Messages {
		if msg.Role != ai.StoredUser {
			continue
		}
		num++
		if i > limit {
			break
		}
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(msg.Text), "\n", 2)[0])
		if r := []rune(line); len(r) > 60 {
			line = string(r[:60]) + "…"
		}
		hint := "message " + strconv.Itoa(num) + " · chat only (no AI file change)"
		if n := s.aiRewindFiles(i); n > 0 {
			hint = fmt.Sprintf("message %d · %d file%s to put back", num, n, plural(n))
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: "You: " + line, Hint: hint})
	}
	for a, b := 0, len(items)-1; a < b; a, b = a+1, b-1 {
		items[a], items[b] = items[b], items[a]
	}
	return items
}

// openAIRewindPicker asks which question to rewind to.
func (m *Model) openAIRewindPicker() tea.Cmd {
	if m.aiS().streaming {
		return m.aiInfo("The AI is still working — stop it first")
	}
	items := aiRewindItems(m)
	if len(items) == 0 {
		return m.aiInfo("Nothing to rewind yet: send a message first")
	}
	m.picker = picker.NewItems(" Rewind Chat to Turn ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIRewind
	return nil
}

// onAIRewindPicked shows the rewind card for the picked question.
func (m *Model) onAIRewindPicked(id string) tea.Cmd {
	s := m.aiS()
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(s.conv.Messages) || s.conv.Messages[i].Role != ai.StoredUser {
		return nil
	}
	if s.streaming {
		return m.aiInfo("The AI is still working — stop it first")
	}
	turns := s.aiRewindTurns(i)
	files := s.aiRewindFiles(i)
	var changed, unsaved, tooBig []string
	add := func(dst *[]string, xs []string) {
		for _, x := range xs {
			dup := false
			for _, y := range *dst {
				dup = dup || x == y
			}
			if !dup {
				*dst = append(*dst, x)
			}
		}
	}
	for _, t := range turns {
		c, u, b := m.aiRestoreChanged(t)
		add(&changed, c)
		add(&unsaved, u)
		add(&tooBig, b)
	}
	var body strings.Builder
	if files > 0 {
		fmt.Fprintf(&body, "Code: puts back %d file%s the AI changed in %d turn%s from this question on (newest first); files those turns created are deleted.",
			files, plural(files), len(turns), plural(len(turns)))
	} else {
		body.WriteString("Code: the AI changed no file from this question on.")
	}
	body.WriteString("\nCode + chat also removes the messages from this question on and puts the question back in the input to edit.")
	line := func(format string, names []string) {
		if len(names) > 0 {
			body.WriteString("\n")
			fmt.Fprintf(&body, format, aiListNames(names))
		}
	}
	line("Changed since the AI turn: %s. Rewinding puts back the text from before; these later changes are lost (an open tab can undo it with u).", changed)
	line("Open with unsaved edits, left as they are: %s.", unsaved)
	line("Too big to restore, left as they are: %s.", tooBig)
	body.WriteString("\nOutside agents' edits: AI: Restore Code Checkpoint…")
	s.rewindAt, s.rewindSet = i, true
	// A rewind can drop later work: Cancel comes first and has the focus.
	m.confirm = confirm.Risky("↶ Rewind to before this question?", body.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "code", Title: "Code only"},
		confirm.Button{ID: "all", Title: "Code + chat", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIRewind
	return nil
}

// answerAIRewind answers the rewind card.
func (m *Model) answerAIRewind(id string) tea.Cmd {
	s := m.aiS()
	at, ok := s.rewindAt, s.rewindSet
	s.rewindSet = false
	if !ok {
		return nil
	}
	switch id {
	case "code":
		return m.aiRewindTo(at, false)
	case "all":
		return m.aiRewindTo(at, true)
	}
	return nil
}

// aiRewindTo puts back the files of every AI turn from user message msg
// on and, with chat, drops the messages from msg on (the question goes
// back to the input).
func (m *Model) aiRewindTo(msg int, chat bool) tea.Cmd {
	s := m.aiS()
	if s.streaming {
		return m.aiInfo("The AI is still working — stop it first")
	}
	if msg < 0 || msg >= len(s.conv.Messages) {
		return nil
	}
	question := s.conv.Messages[msg].Text
	var restored, skipped []string
	var cmds []tea.Cmd
	end := m.aiBeginBatch()
	for _, t := range s.aiRewindTurns(msg) {
		r, k, c := m.aiRestoreTurnFiles(t)
		restored = append(restored, r...)
		skipped = append(skipped, k...)
		cmds = append(cmds, c...)
	}
	cmds = append(cmds, end())
	p := s.aiRestoreLoad()
	kept := p.Turns[:0]
	for _, t := range p.Turns {
		if t.Msg < msg {
			kept = append(kept, t)
		}
	}
	p.Turns = kept
	s.restoreGen++
	s.aiRestoreSave()
	summary := "Rewind to before message " + strconv.Itoa(msg+1)
	if len(restored) > 0 {
		summary += " → restored " + aiListNames(restored)
	} else {
		summary += " → no file to restore"
	}
	if len(skipped) > 0 {
		summary += "; not restored: " + aiListNames(skipped)
	}
	if chat {
		s.conv.Messages = s.conv.Messages[:msg]
		s.input = []rune(question)
		s.caret = len(s.input)
		s.scroll = 0
		s.sel.clear()
	} else {
		s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ " + summary})
	}
	m.aiAudit(clock.Now(), "rewind", fmt.Sprintf("message=%d chat=%v", msg, chat), "user", "user", summary)
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
	text := fmt.Sprintf("Rewound: restored %d file%s", len(restored), plural(len(restored)))
	if chat {
		text += "; edit the question and press Enter"
	}
	if len(skipped) > 0 {
		text += fmt.Sprintf(" (%d not restored)", len(skipped))
	}
	return tea.Batch(append(cmds, m.aiInfo(text))...)
}
