package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The memory extractor (OM-2): after a chat turn that used a tool or
// failed, with memory on in the workspace, the quick model slot reads the
// turn (redacted) and writes up to three lessons to lessons.md. It runs
// in a goroutine with a timeout, so the chat never waits for it; the
// answer gets a "memory: N lessons saved" line when it is done. Plain
// question-and-answer turns are skipped (cost).

// aiMemoryTimeout is how long the quick model may take (a var for tests).
var aiMemoryTimeout = 30 * time.Second

// aiMemoryInputMax caps the turn text sent to the quick model.
const aiMemoryInputMax = 16 << 10

// aiMemoryWriteMu serialises the read-append-write of lessons.md (two
// extractors may end at the same time).
var aiMemoryWriteMu sync.Mutex

// aiMemoryExtractPrompt is the quick model's system prompt.
const aiMemoryExtractPrompt = `You keep the memory of a coding assistant for one repository.
Read the chat turn below (the user's request, the assistant's tool calls, answer and errors) and write at most 3 short lessons that would help the assistant in later sessions in this repository: project facts, commands that work, mistakes to avoid.
Rules:
- Only what this turn showed to be true. Nothing the user did not confirm, no guesses.
- Never write secrets, passwords, keys, tokens, personal data or text marked [REDACTED].
- Paths only relative to the repository root; never paths outside it.
- Do not repeat a lesson that is already saved (titles listed below).
- When nothing is worth keeping, return an empty list.
Reply with only this JSON in a fenced block:
` + "```json\n" + `{"lessons":[{"title":"at most 80 characters","text":"at most 300 characters","paths":["repo/relative/path"]}]}` + "\n```"

// aiMemoryDoneMsg is the extractor's result.
type aiMemoryDoneMsg struct {
	convID string
	msgIdx int // the message the "memory: N lessons saved" line goes on
	n      int // lessons added to the file
	err    error
	usage  *ai.Usage
	cfg    ai.Config // the quick slot's config (usage and cost)
}

// aiMemoryTurnFrom is the user message index of the turn that just ended
// when it qualifies for lessons (a tool call, an error or a failed run
// step); -1 otherwise. A turn the user stopped does not qualify.
func (s *aiState) aiMemoryTurnFrom() int {
	from := s.turnMsg
	if from < 0 || from >= len(s.conv.Messages) || s.conv.Messages[from].Role != ai.StoredUser {
		return -1
	}
	ok := false
	for _, msg := range s.conv.Messages[from+1:] {
		switch msg.Role {
		case ai.StoredTool:
			ok = true
		case ai.StoredError:
			if strings.Contains(msg.Text, context.Canceled.Error()) {
				return -1
			}
			ok = true
		case ai.StoredAssistant:
			if msg.Run == nil {
				continue
			}
			if msg.Run.State == ai.RunStateCancelled {
				return -1
			}
			if msg.Run.State == ai.RunStateFailed {
				ok = true
			}
			for _, st := range msg.Steps {
				ok = ok || len(st.Tools) > 0 || st.Error != ""
			}
		}
	}
	if !ok {
		return -1
	}
	return from
}

// aiMemoryTranscript is the turn from the user message at from, as plain
// text for the quick model: redacted (the IDE's known secrets, then
// key-looking strings) and capped.
func (s *aiState) aiMemoryTranscript(from int) string {
	var b strings.Builder
	for _, msg := range s.conv.Messages[from:] {
		switch msg.Role {
		case ai.StoredUser:
			b.WriteString("User: " + msg.Text + "\n")
		case ai.StoredTool:
			b.WriteString("Tool: " + msg.Text + "\n")
		case ai.StoredError:
			b.WriteString("Error: " + msg.Text + "\n")
		case ai.StoredAssistant:
			for _, st := range msg.Steps {
				title := st.Title
				if title == "" {
					title = st.ID
				}
				b.WriteString("Step " + title + ":\n")
				for _, tl := range st.Tools {
					b.WriteString("  Tool: " + tl + "\n")
				}
				if st.Error != "" {
					b.WriteString("  Error: " + st.Error + "\n")
				}
				if st.Text != "" {
					b.WriteString("  Answer: " + st.Text + "\n")
				}
			}
			if msg.Text != "" {
				b.WriteString("Assistant: " + msg.Text + "\n")
			}
		}
	}
	text := ai.Redact(ai.RedactSecrets(b.String()))
	if len(text) > aiMemoryInputMax {
		head, tail := text[:aiMemoryInputMax/4], text[len(text)-aiMemoryInputMax*3/4:]
		text = strings.ToValidUTF8(head, "") + "\n…\n" + strings.ToValidUTF8(tail, "")
	}
	return text
}

// aiMemoryAfterTurn starts the extractor for the turn that just ended,
// or returns nil (the turn does not qualify, memory is off, no quick
// model slot). In a workspace that never decided it asks once instead.
func (m *Model) aiMemoryAfterTurn() tea.Cmd {
	s := m.aiS()
	from := s.aiMemoryTurnFrom()
	if from < 0 {
		return nil
	}
	switch aiMemoryState() {
	case aiMemoryOn:
		return m.startAIMemory(from)
	case aiMemoryUndecided:
		return m.askAIMemory(from) // ask once (ai_memory_ask.go)
	}
	return nil
}

// startAIMemory runs the extractor for the turn at from.
func (m *Model) startAIMemory(from int) tea.Cmd {
	s := m.aiS()
	slot, err := s.resolveSlot(ai.SlotQuick)
	if err != nil {
		if s.memSlotWarned {
			return nil
		}
		s.memSlotWarned = true
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Memory: no quick model, lessons are not saved",
			"Set the Quick slot in AI: Edit Model Slots")
		return c
	}
	root := aiWorkspaceRoot()
	existing, _ := aimemory.Load(root)
	var known strings.Builder
	for i := len(existing) - 1; i >= 0 && i >= len(existing)-30; i-- {
		known.WriteString("\n- " + aimemory.CleanLine(existing[i].Title, aimemory.MaxTitleRunes))
	}
	user := "The chat turn:\n\n" + s.aiMemoryTranscript(from)
	if known.Len() > 0 {
		user += "\n\nLessons already saved:" + ai.Redact(ai.RedactSecrets(known.String()))
	}
	req := ai.Request{Model: slot.cfg.Model, Messages: []ai.Message{
		{Role: ai.RoleSystem, Content: aiMemoryExtractPrompt},
		{Role: ai.RoleUser, Content: user},
	}}
	convID, msgIdx := s.conv.ID, len(s.conv.Messages)-1
	date := clock.Now().Format("2006-01-02")
	timeout := aiMemoryTimeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		text, last := ai.Collect(slot.p.Stream(ctx, req))
		done := aiMemoryDoneMsg{convID: convID, msgIdx: msgIdx, usage: last.Usage, cfg: slot.cfg}
		if last.Err != nil {
			done.err = last.Err
			return done
		}
		done.n, done.err = aiMemorySave(root, text, date)
		return done
	}
}

// aiMemorySave parses a quick model reply and appends its lessons to the
// workspace's file; it returns how many are new. A lesson that holds a
// secret after the redaction is dropped.
func aiMemorySave(root, reply, date string) (int, error) {
	ls, err := aimemory.ParseReply(reply)
	if err != nil {
		return 0, err
	}
	var add []aimemory.Lesson
	for _, l := range ls {
		l.Title, l.Text = ai.Redact(ai.RedactSecrets(l.Title)), ai.Redact(ai.RedactSecrets(l.Text))
		bad := strings.Contains(l.Title+l.Text, aimemory.Redacted)
		for _, p := range l.Paths {
			bad = bad || ai.Redact(ai.RedactSecrets(p)) != p
		}
		if bad {
			continue
		}
		l.Date = date
		add = append(add, l)
	}
	if len(add) == 0 {
		return 0, nil
	}
	aiMemoryWriteMu.Lock()
	defer aiMemoryWriteMu.Unlock()
	cur, err := aimemory.Load(root)
	if err != nil {
		return 0, err
	}
	had := map[string]bool{}
	for _, l := range cur {
		had[l.ID] = true
	}
	next := aimemory.Append(cur, add...)
	n := 0
	for _, l := range next {
		if !had[l.ID] {
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, aimemory.Save(root, next)
}

// onAIMemoryDone records the extractor's result: usage, the audit line,
// the "memory: N lessons saved" note on the turn. Errors are only
// logged (redacted), never lesson text.
func (m *Model) onAIMemoryDone(msg aiMemoryDoneMsg) tea.Cmd {
	s := m.aiS()
	if u := msg.usage; u != nil {
		c := *u
		model := c.Model
		if model == "" {
			model = msg.cfg.Model
		}
		e := aiUsageEntry{Agent: "memory", Model: model, U: c, Sub: msg.cfg.Kind == ai.KindClaudeOAuth}
		if usd, ok := ai.NewPricing(s.settings.Prices).Cost(msg.cfg.Kind, c); ok {
			e.Cost = &usd
		}
		s.recordUsage(e)
	}
	stamp := clock.Now().Format("15:04:05")
	if msg.err != nil {
		if !errors.Is(msg.err, aimemory.ErrNoLessons) {
			recordError("[ai] memory: " + ai.Redact(msg.err.Error()))
		}
		m.output.Append(aiAuditChannel, stamp+"  memory · no lessons ("+ai.Redact(msg.err.Error())+")")
		return nil
	}
	m.output.Append(aiAuditChannel, stamp+"  memory · "+strconv.Itoa(msg.n)+" lesson"+plural(msg.n)+" saved")
	if msg.n == 0 {
		return nil
	}
	if msg.convID == s.conv.ID {
		if msg.msgIdx >= 0 && msg.msgIdx < len(s.conv.Messages) {
			s.conv.Messages[msg.msgIdx].Memory = msg.n
			if err := s.store.Save(&s.conv); err != nil {
				recordError("[ai] save chat: " + err.Error())
			}
		}
		return nil
	}
	if c, err := s.store.Load(msg.convID); err == nil && msg.msgIdx >= 0 && msg.msgIdx < len(c.Messages) {
		c.Messages[msg.msgIdx].Memory = msg.n
		_ = s.store.Save(&c)
	}
	return nil
}

// aiMemorySavedLine is the dim "memory: 2 lessons saved · view" line
// under a turn; a click opens AI: Memory….
func aiMemorySavedLine(n, width int) aiLine {
	text := "memory: " + strconv.Itoa(n) + " lesson" + plural(n) + " saved · view"
	if lim := width - aiIndent - 1; runewidth.StringWidth(text) > lim && lim > 3 {
		text = runewidth.Truncate(text, lim, "…")
	}
	return aiLine{text: strings.Repeat(" ", aiIndent) + text, style: theme.Fg(theme.TextDim),
		buttons: []aiLineButton{{aiIndent, aiIndent + runewidth.StringWidth(text), "memory-view"}}}
}
