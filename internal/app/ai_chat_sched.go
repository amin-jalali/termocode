package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
)

// ── Chat schedules (CMP-43) ──────────────────────────────────────────────
//
// "Rerun this chat at 9:00": a chat keeps a schedule (ai.ChatSchedule in
// its own file) and, at each slot, gets its saved message as a new user
// turn. The user sets it with AI: Schedule This Chat… (when, then the
// message, then a Cancel-first card); the AI proposes one for its own
// chat with chat_schedule_set, which always asks with the same kind of
// card. A run happens only while termocode is open, the AI is idle and
// the user is not typing in another chat; a slot missed by up to the
// grace runs once, late. Another window that shows the chat runs it
// there (the chat claim decides). At most once an hour: each run is a
// paid turn.

const (
	promptKindAIChatSchedWhen   promptKindEnum  = 4361
	promptKindAIChatSchedPrompt promptKindEnum  = 4362
	confirmKindAIChatSched      confirmKindEnum = 4363
	confirmKindAIChatUnsched    confirmKindEnum = 4364

	aiChatSchedPaletteID   = "ai-chat-schedule"
	aiChatUnschedPaletteID = "ai-chat-unschedule"
)

// aiChatSchedState is the chat schedules of this window.
type aiChatSchedState struct {
	host  *aiChatSchedHost
	clock schedule.Clock // nil: the real clock (tests set a fake one)
	// draft is the schedule a prompt or card is about, for chat draftID.
	draft   ai.ChatSchedule
	draftID string
}

// aiChatSchedHost runs the scheduled chats of this process.
type aiChatSchedHost struct {
	s  *schedule.Scheduler
	ch chan tea.Msg

	mu   sync.Mutex
	keys map[string]string // chat id → the schedule it was added for
}

type (
	aiChatSchedBootMsg struct{}
	aiChatSchedListMsg struct{ chats []ai.Conversation }
	aiChatSchedDueMsg  struct {
		ctx   context.Context
		due   schedule.Due
		reply chan error
	}
	aiChatSchedEventMsg struct{ ev schedule.Event }
)

// aiChatSchedBootCmd is the Init command of the chat schedules.
func aiChatSchedBootCmd() tea.Cmd { return func() tea.Msg { return aiChatSchedBootMsg{} } }

func aiChatSchedKey(c *ai.ChatSchedule) string {
	return c.When + "\x00" + c.TZ + "\x00" + c.Prompt
}

// aiChatSchedNow is the clock of the schedules.
func (m *Model) aiChatSchedNow() time.Time {
	if c := m.aiS().chatSched.clock; c != nil {
		return c.Now()
	}
	return clock.Now()
}

// handleAIChatSchedMsg handles the schedule messages. ok=false: not ours.
func (m *Model) handleAIChatSchedMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case aiChatSchedBootMsg:
		store := m.aiS().store
		return func() tea.Msg { return aiChatSchedListMsg{chats: store.ScheduledChats()} }, true
	case aiChatSchedListMsg:
		var cmd tea.Cmd
		for i := range msg.chats {
			if c := m.aiChatSchedSet(msg.chats[i].ID, msg.chats[i].Schedule); c != nil {
				cmd = c
			}
		}
		return cmd, true
	case aiChatSchedDueMsg:
		h := m.aiS().chatSched.host
		var cmd tea.Cmd
		if msg.ctx.Err() != nil {
			msg.reply <- msg.ctx.Err()
		} else {
			var err error
			cmd, err = m.aiChatSchedRun(msg.due)
			msg.reply <- err
		}
		if h != nil {
			cmd = tea.Batch(cmd, wfxListen(h.ch))
		}
		return cmd, true
	case aiChatSchedEventMsg:
		m.aiChatSchedRecord(msg.ev)
		if h := m.aiS().chatSched.host; h != nil {
			return wfxListen(h.ch), true
		}
		return nil, true
	}
	return nil, false
}

// aiChatSchedSet adds, changes or (sc nil) removes the job of chat id.
// It makes the scheduler on the first job and returns its listen cmd.
func (m *Model) aiChatSchedSet(id string, sc *ai.ChatSchedule) tea.Cmd {
	st := &m.aiS().chatSched
	if sc == nil && st.host == nil {
		return nil
	}
	var cmd tea.Cmd
	if st.host == nil {
		h := &aiChatSchedHost{ch: make(chan tea.Msg, 32), keys: map[string]string{}}
		h.s = schedule.New(schedule.Options{Clock: st.clock, Fire: h.fire, OnEvent: h.record})
		h.s.Start()
		st.host, cmd = h, wfxListen(h.ch)
	}
	h := st.host
	h.mu.Lock()
	old, had := h.keys[id]
	h.mu.Unlock()
	if sc != nil && had && old == aiChatSchedKey(sc) {
		return cmd
	}
	if had {
		h.s.Remove(id)
		h.mu.Lock()
		delete(h.keys, id)
		h.mu.Unlock()
	}
	if sc == nil {
		return cmd
	}
	compiled, err := ai.CompileChatSchedule(sc.When, sc.TZ, m.aiChatSchedNow())
	if err != nil {
		recordError("[ai] chat schedule " + id + ": " + err.Error())
		return cmd
	}
	j := schedule.Job{ID: id, Schedule: compiled}
	if sc.LastRunMs > 0 {
		j.LastRun = time.UnixMilli(sc.LastRunMs)
	}
	if err := h.s.Add(j); err != nil {
		recordError("[ai] chat schedule " + id + ": " + err.Error())
		return cmd
	}
	h.mu.Lock()
	h.keys[id] = aiChatSchedKey(sc)
	h.mu.Unlock()
	return cmd
}

// aiChatSchedStop ends the scheduler (tests; the process end does it too).
func (m *Model) aiChatSchedStop() {
	if m.ai != nil && m.ai.chatSched.host != nil {
		m.ai.chatSched.host.s.Stop()
	}
}

// aiChatSchedNext is the next run of chat id.
func (m *Model) aiChatSchedNext(id string) (time.Time, bool) {
	if m.ai == nil || m.ai.chatSched.host == nil {
		return time.Time{}, false
	}
	return m.ai.chatSched.host.s.NextRun(id)
}

// fire asks the UI goroutine to start the scheduled turn; it returns
// once the turn started (or could not).
func (h *aiChatSchedHost) fire(ctx context.Context, d schedule.Due) error {
	reply := make(chan error, 1)
	select {
	case h.ch <- aiChatSchedDueMsg{ctx: ctx, due: d, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// record hands a handled slot to the UI goroutine (it writes the chat).
func (h *aiChatSchedHost) record(ev schedule.Event) {
	select {
	case h.ch <- aiChatSchedEventMsg{ev: ev}:
	case <-time.After(5 * time.Second): // the UI is gone or stuck: the slot still counts as handled in memory
	}
}

// aiChatSchedRun starts the scheduled turn of d.ID, or says why not.
func (m *Model) aiChatSchedRun(d schedule.Due) (tea.Cmd, error) {
	s := m.aiS()
	if !m.aiConfigured() {
		return nil, errors.New("the AI is not configured")
	}
	if s.streaming {
		return nil, errors.New("skipped: the AI was answering")
	}
	var c ai.Conversation
	if s.conv.ID == d.ID {
		c = s.conv
	} else {
		var err error
		if c, err = s.store.Load(d.ID); err != nil {
			return nil, fmt.Errorf("the chat is gone: %w", err)
		}
	}
	if c.Schedule == nil {
		return nil, errors.New("the chat has no schedule any more")
	}
	if s.conv.ID != d.ID {
		if len(s.input) > 0 {
			return nil, errors.New("skipped: you were typing in another chat")
		}
		if !s.holdChat(d.ID) {
			return nil, errors.New("skipped: the chat is open in another termocode window")
		}
		if len(s.conv.Messages) > 0 {
			if err := s.store.Save(&s.conv); err != nil {
				recordError("[ai] save chat: " + err.Error())
			}
		}
		m.aiShowStoredChat(c)
	}
	sc := *c.Schedule
	late := ""
	if d.Late {
		late = " (late: termocode was closed or asleep)"
	}
	m.aiNote("⏰ Scheduled run · " + sc.Label() + late)
	cmd := m.sendAIChat(sc.Prompt)
	if !s.streaming && s.run == nil {
		return cmd, errors.New("the turn did not start (a card may be waiting)")
	}
	title := aiOneLine(s.conv.Title)
	return tea.Batch(cmd, m.aiInfo("⏰ Scheduled chat started: "+title), m.notifyOS("Scheduled chat started: "+title)), nil
}

// aiChatSchedRecord writes the outcome of a slot to the chat.
func (m *Model) aiChatSchedRecord(ev schedule.Event) {
	s := m.aiS()
	outcome := string(ev.Outcome)
	if ev.Err != nil {
		outcome += ": " + ai.Redact(ev.Err.Error())
	}
	set := func(c *ai.Conversation) bool {
		if c.Schedule == nil {
			return false
		}
		c.Schedule.LastRunMs, c.Schedule.LastOutcome = ev.Slot.UnixMilli(), outcome
		return true
	}
	if s.conv.ID == ev.ID {
		if set(&s.conv) && !s.streaming { // a running turn saves the chat when it ends
			if err := s.store.Save(&s.conv); err != nil {
				recordError("[ai] save chat: " + err.Error())
			}
		}
		return
	}
	claim, err := s.store.Claim(ev.ID)
	if err != nil {
		return // another window shows the chat; it keeps its own record
	}
	defer claim.Release()
	c, err := s.store.Load(ev.ID)
	if err == nil && set(&c) {
		if err := s.store.Save(&c); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
}

// ── The user's commands ──────────────────────────────────────────────────

// openAIChatSchedule is AI: Schedule This Chat…: when, then the message.
func (m *Model) openAIChatSchedule() tea.Cmd {
	s := m.aiS()
	if len(s.conv.Messages) == 0 {
		return m.aiInfo("Send a first message, then schedule the chat")
	}
	st := &s.chatSched
	st.draftID, st.draft = s.conv.ID, ai.ChatSchedule{By: ai.ChatSchedByUser}
	when := "at 09:00"
	if c := s.conv.Schedule; c != nil {
		st.draft.Prompt, st.draft.TZ = c.Prompt, c.TZ
		when = c.When
	}
	m.prompt = prompt.New(" Schedule this chat ", "When should this chat run again?", when)
	m.prompt.SetNote("at 09:00 · weekdays 09:00 · weekly mon 09:00 · monthly 1 09:00 · hourly :15 · or 5 cron fields. At most once an hour; only while termocode is open.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIChatSchedWhen
	return nil
}

// onAIChatSchedWhen checks the time, then asks for the message.
func (m *Model) onAIChatSchedWhen(value string) tea.Cmd {
	s := m.aiS()
	st := &s.chatSched
	if st.draftID != s.conv.ID {
		return m.aiInfo("Not scheduled: the chat changed meanwhile")
	}
	when := strings.TrimSpace(value)
	if _, err := ai.CompileChatSchedule(when, st.draft.TZ, m.aiChatSchedNow()); err != nil {
		return m.pushToastDetail(toast.Warn, "Not scheduled", err.Error())
	}
	st.draft.When = when
	text := st.draft.Prompt
	if text == "" {
		text = aiLastUserText(s.conv)
	}
	m.prompt = prompt.New(" Schedule this chat ", "Message to send at each run:", text)
	m.prompt.SetNote("It is sent as your message, as if you typed it. The last message you sent is filled in.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIChatSchedPrompt
	return nil
}

// onAIChatSchedPrompt checks the whole schedule and shows the card.
func (m *Model) onAIChatSchedPrompt(value string) tea.Cmd {
	s := m.aiS()
	st := &s.chatSched
	if st.draftID != s.conv.ID {
		return m.aiInfo("Not scheduled: the chat changed meanwhile")
	}
	st.draft.Prompt = strings.TrimSpace(value)
	if _, err := st.draft.Check(m.aiChatSchedNow()); err != nil {
		return m.pushToastDetail(toast.Warn, "Not scheduled", err.Error())
	}
	if err := m.aiChatSchedRoom(s.conv.ID); err != nil {
		return m.pushToastDetail(toast.Warn, "Not scheduled", err.Error())
	}
	m.confirm = confirm.Risky("Schedule this chat?", m.aiChatSchedBody(s.conv.Title, st.draft),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "schedule", Title: "Schedule", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIChatSched
	return nil
}

// aiChatSchedBody is the card text of schedule sc for a chat.
func (m *Model) aiChatSchedBody(title string, sc ai.ChatSchedule) string {
	next := "never"
	if compiled, err := ai.CompileChatSchedule(sc.When, sc.TZ, m.aiChatSchedNow()); err == nil {
		if t := compiled.Next(m.aiChatSchedNow()); !t.IsZero() {
			next = t.Format("Mon 2 Jan 15:04")
		}
	}
	msg := sc.Prompt
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	lines := strings.Split(msg, "\n")
	for i, l := range lines {
		lines[i] = sanitizeOutputLine(l)
	}
	return "Chat: " + aiOneLine(title) + "\nWhen: " + sc.Label() + " · next " + next +
		"\n\nSends at each run:\n\n    " + strings.Join(lines, "\n    ") +
		"\n\nEach run is one paid turn in this chat, with the chat's tools and approvals. It runs only while termocode is open " +
		"and the AI is idle; a slot missed by up to 30 minutes runs once, late. Stop it any time: AI: Stop Chat Schedule."
}

// aiChatSchedRoom: chat id may get a schedule (ai.ChatSchedMax).
func (m *Model) aiChatSchedRoom(id string) error {
	st := m.aiS().chatSched
	if st.host == nil {
		return nil
	}
	st.host.mu.Lock()
	n := len(st.host.keys)
	_, had := st.host.keys[id]
	st.host.mu.Unlock()
	if !had && n >= ai.ChatSchedMax {
		return fmt.Errorf("%d chats have a schedule already; stop one first", n)
	}
	return nil
}

// answerAIChatSchedCard saves the schedule on Schedule; Cancel and Esc
// save nothing.
func (m *Model) answerAIChatSchedCard(id string) tea.Cmd {
	m.confirmOpen = false
	s := m.aiS()
	st := &s.chatSched
	draft, chat := st.draft, st.draftID
	st.draft, st.draftID = ai.ChatSchedule{}, ""
	if id != "schedule" {
		return nil
	}
	if chat != s.conv.ID {
		return m.aiInfo("Not scheduled: the chat changed meanwhile")
	}
	text, cmd, err := m.aiChatSchedApply(draft)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Not scheduled", err.Error())
	}
	return tea.Batch(cmd, m.aiInfo(text))
}

// aiChatSchedApply saves schedule sc on the open chat and schedules it.
func (m *Model) aiChatSchedApply(sc ai.ChatSchedule) (string, tea.Cmd, error) {
	s := m.aiS()
	if len(s.conv.Messages) == 0 {
		return "", nil, errors.New("the chat is empty")
	}
	if _, err := sc.Check(m.aiChatSchedNow()); err != nil {
		return "", nil, err
	}
	if err := m.aiChatSchedRoom(s.conv.ID); err != nil {
		return "", nil, err
	}
	sc.CreatedAtMs = m.aiChatSchedNow().UnixMilli()
	if old := s.conv.Schedule; old != nil && aiChatSchedKey(old) == aiChatSchedKey(&sc) {
		sc.LastRunMs, sc.LastOutcome = old.LastRunMs, old.LastOutcome
	}
	s.conv.Schedule = &sc
	by := ""
	if sc.By == ai.ChatSchedByAI {
		by = " (asked by the AI, you allowed it)"
	}
	cmd := m.aiChatSchedSet(s.conv.ID, &sc)
	text := "⏰ Scheduled: " + sc.Label() + by
	if t, ok := m.aiChatSchedNext(s.conv.ID); ok {
		text += " · next " + t.Format("Mon 2 Jan 15:04")
	}
	m.aiChatSchedSave(text)
	return text, cmd, nil
}

// openAIChatUnschedule is AI: Stop Chat Schedule: a card, then off.
func (m *Model) openAIChatUnschedule() tea.Cmd {
	s := m.aiS()
	sc := s.conv.Schedule
	if sc == nil {
		return m.aiInfo("This chat has no schedule")
	}
	s.chatSched.draftID = s.conv.ID
	m.confirm = confirm.Risky("Stop this chat's schedule?",
		"Chat: "+aiOneLine(s.conv.Title)+"\nWhen: "+sc.Label()+"\n\nThe chat stays; it only stops running by itself.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "stop", Title: "Stop schedule", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIChatUnsched
	return nil
}

// answerAIChatUnschedCard removes the schedule on Stop schedule.
func (m *Model) answerAIChatUnschedCard(id string) tea.Cmd {
	m.confirmOpen = false
	s := m.aiS()
	chat := s.chatSched.draftID
	s.chatSched.draftID = ""
	if id != "stop" {
		return nil
	}
	if chat != s.conv.ID {
		return m.aiInfo("Not changed: the chat changed meanwhile")
	}
	return m.aiInfo(m.aiChatSchedClear())
}

// aiChatSchedClear removes the open chat's schedule.
func (m *Model) aiChatSchedClear() string {
	s := m.aiS()
	if s.conv.Schedule == nil {
		return "this chat has no schedule"
	}
	s.conv.Schedule = nil
	m.aiChatSchedSet(s.conv.ID, nil)
	m.aiChatSchedSave("⏰ Schedule stopped")
	return "⏰ Schedule stopped"
}

// aiChatSchedSave saves the open chat with a note. During a turn (the
// AI's own tool call) the turn's end saves it: no note in the middle of
// the answer.
func (m *Model) aiChatSchedSave(note string) {
	if m.aiS().streaming {
		return
	}
	m.aiNote(note)
}

// aiLastUserText is the text of the chat's last user message.
func aiLastUserText(c ai.Conversation) string {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == ai.StoredUser && strings.TrimSpace(c.Messages[i].Text) != "" {
			return c.Messages[i].Text
		}
	}
	return ""
}

// ── The AI's tools ───────────────────────────────────────────────────────

// aiChatSchedAllow is the Allow title of chat_schedule_set's card (the
// card is Cancel-first).
var aiChatSchedAllow = map[string]string{"chat_schedule_set": "Schedule chat"}

// aiChatSchedArgs reads chat_schedule_set's arguments.
func aiChatSchedArgs(a map[string]any) ai.ChatSchedule {
	str := func(k string) string { v, _ := a[k].(string); return strings.TrimSpace(v) }
	return ai.ChatSchedule{When: str("when"), TZ: str("tz"), Prompt: str("prompt"), By: ai.ChatSchedByAI}
}

// aiChatSchedTools let the AI schedule its own chat (always after the
// user's yes), read the schedule and stop it.
func (m *Model) aiChatSchedTools(call aiIDECaller) []ai.Tool {
	set := ai.Tool{
		Name: "chat_schedule_set",
		Description: "Schedule THIS chat to run again by itself, for example every weekday at 09:00 to check a build or a queue. " +
			"At each run the prompt is sent as a new user message in this chat. The user always confirms; it replaces this chat's old schedule. " +
			"At most once an hour; it runs only while termocode is open.",
		Params: map[string]any{
			"when":   map[string]any{"type": "string", "description": `"at 09:00", "weekdays 09:00", "weekly mon 09:00", "monthly 1 09:00", "hourly :15" or 5 cron fields`},
			"prompt": map[string]any{"type": "string", "description": "the message to send at each run; write it so it makes sense on its own"},
			"tz":     map[string]any{"type": "string", "description": "IANA time zone such as Europe/Berlin; empty: the machine's zone"},
		},
		Required: []string{"when", "prompt"},
		Mutating: true, // Class "": always a card, never "always allow"
		Describe: func(a map[string]any) string {
			sc := aiChatSchedArgs(a)
			return "Schedule this chat: " + sc.Label() + " · sends “" + aiOneLineClip(sc.Prompt, 80) + "”"
		},
		Validate: func(a map[string]any) error {
			_, err := aiChatSchedArgs(a).Check(clock.Now())
			return err
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			sc := aiChatSchedArgs(a)
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				text, cmd, err := m.aiChatSchedApply(sc)
				return strings.TrimPrefix(text, "⏰ "), cmd, err
			})
		},
	}
	read := ai.Tool{
		Name:        "chat_schedule_read",
		Description: "Read this chat's schedule: when it runs, the next run, the message and the last outcome.",
		Params:      map[string]any{},
		ReadOnly:    true,
		Describe:    func(map[string]any) string { return "Read chat schedule" },
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				s := m.aiS()
				sc := s.conv.Schedule
				if sc == nil {
					return "this chat has no schedule", nil, nil
				}
				out := "when: " + sc.Label() + "\nset by: " + sc.By
				if t, ok := m.aiChatSchedNext(s.conv.ID); ok {
					out += "\nnext run: " + t.Format(time.RFC3339)
				}
				if sc.LastRunMs > 0 {
					out += "\nlast run: " + time.UnixMilli(sc.LastRunMs).Format(time.RFC3339) + " (" + sc.LastOutcome + ")"
				}
				return out + "\nprompt:\n" + sc.Prompt, nil, nil
			})
		},
	}
	stop := ai.Tool{
		Name:        "chat_schedule_clear",
		Description: "Stop this chat's schedule (the chat stays). Use it when the scheduled work is done.",
		Params:      map[string]any{},
		Describe:    func(map[string]any) string { return "Stop chat schedule" },
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				return strings.TrimPrefix(m.aiChatSchedClear(), "⏰ "), nil, nil
			})
		},
	}
	return []ai.Tool{set, read, stop}
}
