package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat topic (WEB-13, web design 2.2): the chat open on the laptop,
// followed live. A streaming answer grows by append deltas (merged while
// queued); a finished or changed message goes out whole with its markdown
// as an AST; removed messages are a truncate; the laptop opening another
// chat is one switched delta with all of it.
//
// Secrets: every chat string passes the registered-secret redaction
// before it is cut into nodes or chunks. A streaming answer holds back an
// end that could be the start of a secret (ai.SecretTailHold), so a
// secret never leaves in two pieces; the held bytes go out with the next
// chunk or when the answer ends.

func init() {
	webTopics["chat"] = webTopicDef{snapshot: webChatSnapshot, apply: webApplyChat}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishChat)
}

// webChatSent is what the browsers have of one message.
type webChatSent struct {
	role      string
	text      string // the raw text it was built from
	streaming bool
	sent      int  // streaming: raw bytes of text the browsers have
	truncated bool // streaming: the text reached the cap
	key       string
}

// webChatPub is what the chat topic sent last (Update only).
type webChatPub struct {
	listen bool
	id     string
	title  string
	busy   bool
	msgs   []webChatSent
}

func webChatSnapshot(m *Model, _ string) (any, error) {
	if m.web == nil {
		return m.webChatBuild(&webChatPub{}), nil
	}
	return m.webChatBuild(&m.web.chat), nil
}

// webChatBuild builds the whole open chat and makes it p's baseline.
func (m *Model) webChatBuild(p *webChatPub) webproto.ChatSnap {
	*p = webChatPub{listen: true}
	snap := webproto.ChatSnap{Messages: []webproto.ChatMsg{}}
	s := m.ai
	if s == nil {
		return snap
	}
	snap.ID, snap.Title, snap.Busy = s.conv.ID, webChatTitle(s.conv), s.streaming
	snap.Profile, snap.Model = s.profile, s.cfg.Model
	p.id, p.title, p.busy = snap.ID, snap.Title, snap.Busy
	n := len(s.conv.Messages)
	full := webChatFullFrom(s.conv.Messages)
	for i := range s.conv.Messages {
		cm, sent := webChatMsg(i, &s.conv.Messages[i], webChatStreaming(s, i, n))
		if i < full {
			cm = webChatStub(cm)
		}
		snap.Messages = append(snap.Messages, cm)
		p.msgs = append(p.msgs, sent)
	}
	return snap
}

// webChatSnapText is the text a whole-chat snap carries at most: older
// messages beyond it go as stubs (role and index, cut), and the page
// fetches one with chat.get_message when the user scrolls to it. It keeps
// a snap frame well under the hub's frame size.
const webChatSnapText = 256 << 10

// webChatFullFrom is the first message a snap sends whole: the newest
// messages, as many as fit webChatSnapText.
func webChatFullFrom(msgs []ai.StoredMessage) int {
	total := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		size := min(len(msgs[i].Text), webproto.MaxChatMsgText)
		for j := range msgs[i].Steps {
			size += min(len(msgs[i].Steps[j].Text), webproto.MaxChatMsgText)
		}
		if total += size; total > webChatSnapText && i < len(msgs)-1 {
			return i + 1
		}
	}
	return 0
}

// webChatStub is a message without its text: only who wrote it.
func webChatStub(cm webproto.ChatMsg) webproto.ChatMsg {
	return webproto.ChatMsg{I: cm.I, Role: cm.Role, Truncated: true, Model: cm.Model}
}

// webChatStreaming reports whether message i of n is the answer the AI
// is writing now.
func webChatStreaming(s *aiState, i, n int) bool {
	return s.streaming && i == n-1 && s.conv.Messages[i].Role == ai.StoredAssistant && s.conv.Messages[i].Run == nil
}

func webChatTitle(c ai.Conversation) string {
	if c.Title == "" || c.Title == "New chat" {
		return ai.DeriveTitle(c.Messages)
	}
	return c.Title
}

// webPublishChat sends what changed in the open chat since the last call.
func (m *Model) webPublishChat(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.chat
	if !h.webListens("chat") {
		*p = webChatPub{}
		return nil
	}
	s := m.ai
	if s == nil || !p.listen {
		return nil
	}
	if s.conv.ID != p.id {
		snap := m.webChatBuild(p)
		h.publish("chat", webChatDelta{Op: webproto.ChatSwitched, Chat: &snap})
		return nil
	}
	msgs := s.conv.Messages
	n := len(msgs)
	if n < len(p.msgs) {
		h.publish("chat", webChatDelta{Op: webproto.ChatTruncate, N: &n})
		p.msgs = p.msgs[:n]
	}
	for i := range msgs {
		sm := &msgs[i]
		live := webChatStreaming(s, i, n)
		if i < len(p.msgs) {
			old := &p.msgs[i]
			if live && old.streaming && old.role == sm.Role && webChatAppendable(old, sm.Text) {
				end, trunc := webChatVisible(sm.Text)
				if trunc == old.truncated {
					if end > old.sent {
						i := i
						h.publish("chat", webChatDelta{Op: webproto.ChatAppend, I: &i, Text: webChatRedact(sm.Text[old.sent:end])})
						old.sent = end
					}
					old.text = sm.Text
					continue
				}
			}
			if !live && !old.streaming && old.same(sm) {
				continue
			}
		}
		cm, sent := webChatMsg(i, sm, live)
		if i < len(p.msgs) {
			p.msgs[i] = sent
		} else {
			p.msgs = append(p.msgs, sent)
		}
		h.publish("chat", webChatDelta{Op: webproto.ChatMessage, Msg: &cm})
	}
	title := webChatTitle(s.conv)
	if s.streaming != p.busy || title != p.title {
		d := webChatDelta{Op: webproto.ChatState}
		if s.streaming != p.busy {
			busy := s.streaming
			d.Busy = &busy
		}
		if title != p.title {
			d.Title = title
		}
		p.busy, p.title = s.streaming, title
		h.publish("chat", d)
	}
	return nil
}

// webChatAppendable: the message's text still starts with what was sent
// (an answer only grows while it streams).
func webChatAppendable(old *webChatSent, text string) bool {
	return len(text) >= old.sent && text[:old.sent] == old.text[:old.sent]
}

// same reports whether a finished message is unchanged since it was sent.
func (w *webChatSent) same(sm *ai.StoredMessage) bool {
	return w.role == sm.Role && w.text == sm.Text && w.key == webChatKey(sm)
}

// webChatKey sums up the parts of a message besides its text that the
// page shows (run, steps, model, images), to see a change cheaply.
func webChatKey(sm *ai.StoredMessage) string {
	if sm.Run == nil && len(sm.Steps) == 0 && sm.Model == "" && len(sm.Images) == 0 && sm.Rules == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(sm.Model + "\x00" + sm.Rules + "\x00" + strconv.Itoa(len(sm.Images)))
	if r := sm.Run; r != nil {
		fmt.Fprintf(&b, "\x00%s %s %s %d %g %v", r.ID, r.Profile, r.State, r.ElapsedMs, r.Spent, r.PlanWait)
	}
	for i := range sm.Steps {
		st := &sm.Steps[i]
		fmt.Fprintf(&b, "\x00%s %s %s %s %d %d %s", st.ID, st.Title, st.State, st.Model, len(st.Text), len(st.Tools), st.Error)
	}
	return b.String()
}

// webChatVisible is how much of a streaming answer may go out now: all of
// it but an end that could start a secret, cut at a character and at
// the message cap (trunc: the cap cut it).
func webChatVisible(text string) (end int, trunc bool) {
	end = len(text) - ai.SecretTailHold(text)
	if end > webproto.MaxChatMsgText {
		end, trunc = webproto.MaxChatMsgText, true
	}
	return webRuneStart(text, end), trunc
}

// webRuneStart moves a byte offset back to the start of a character.
func webRuneStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// webChatCut cuts text at max bytes (at a character).
func webChatCut(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	return text[:webRuneStart(text, max)], true
}

// webChatRedact hides registered secrets in chat text.
func webChatRedact(s string) string { return ai.RedactSecrets(s) }

// webChatMsg trims a stored message for the page and says what was sent.
func webChatMsg(i int, sm *ai.StoredMessage, streaming bool) (webproto.ChatMsg, webChatSent) {
	if streaming {
		return webChatMsgOf(i, sm, true, webproto.MaxChatMsgText)
	}
	return webChatMsgMax(i, sm, webproto.MaxChatMsgText)
}

// webChatMsgMax is webChatMsg for a finished message cut at max bytes.
func webChatMsgMax(i int, sm *ai.StoredMessage, max int) (webproto.ChatMsg, webChatSent) {
	return webChatMsgOf(i, sm, false, max)
}

func webChatMsgOf(i int, sm *ai.StoredMessage, streaming bool, max int) (webproto.ChatMsg, webChatSent) {
	cm := webproto.ChatMsg{I: i, Role: sm.Role, Model: sm.Model, Rules: sm.Rules}
	sent := webChatSent{role: sm.Role, text: sm.Text, streaming: streaming, key: webChatKey(sm)}
	switch {
	case cm.Role != ai.StoredUser && cm.Role != ai.StoredAssistant && cm.Role != ai.StoredTool && cm.Role != ai.StoredError:
		cm.Role = ai.StoredTool // an unknown role shows as a plain line
		cm.Text, cm.Truncated = webChatCut(webChatRedact(sm.Text), max)
	case streaming:
		end, trunc := webChatVisible(sm.Text)
		cm.Text, cm.Streaming, cm.Truncated = webChatRedact(sm.Text[:end]), true, trunc
		sent.sent, sent.truncated = end, trunc
	case cm.Role == ai.StoredTool || cm.Role == ai.StoredError:
		cm.Text, cm.Truncated = webChatCut(webChatRedact(sm.Text), max)
	default:
		var text string
		text, cm.Truncated = webChatCut(webChatRedact(sm.Text), max)
		if strings.TrimSpace(text) != "" {
			cm.MD = webMarkdown(text)
		}
	}
	for _, img := range sm.Images {
		cm.Images = append(cm.Images, filepath.Base(img))
	}
	if r := sm.Run; r != nil {
		cm.Run = &webproto.ChatRun{ID: r.ID, Profile: r.Profile, State: webRunState(r.State), ElapsedMs: r.ElapsedMs, CostUSD: r.Spent, PlanWait: r.PlanWait}
	}
	for j := range sm.Steps {
		cm.Steps = append(cm.Steps, webChatStep(&sm.Steps[j]))
	}
	return cm, sent
}

// webRunState maps a stored run state onto the page's list.
func webRunState(s string) string {
	switch s {
	case ai.RunStateRunning, ai.RunStateDone, ai.RunStateFailed, ai.RunStateCancelled, ai.RunStateInterrupted, ai.RunStatePaused:
		return s
	}
	return ai.RunStateRunning
}

// webChatStep trims a run step: plain text while it runs, an AST once it
// ended.
func webChatStep(st *ai.StoredStep) webproto.ChatStep {
	out := webproto.ChatStep{ID: st.ID, Title: st.Title, State: st.State, Model: st.Model, Error: webChatRedact(st.Error)}
	for _, t := range st.Tools {
		out.Tools = append(out.Tools, webChatRedact(t))
	}
	text, _ := webChatCut(webChatRedact(st.Text), webproto.MaxChatMsgText)
	switch {
	case strings.TrimSpace(text) == "":
	case st.State == "running" || st.State == "pending" || st.State == "":
		out.Text = text
	default:
		out.MD = webMarkdown(text)
	}
	return out
}

// webChatDelta is a chat delta that merges while queued: appends to the
// same message join; a newer whole message replaces a queued append or
// message of the same index; state changes join.
type webChatDelta webproto.ChatDelta

func (d webChatDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webChatDelta)
	if !ok {
		return nil, false
	}
	switch {
	case d.Op == webproto.ChatAppend && n.Op == webproto.ChatAppend && *d.I == *n.I:
		i := *d.I
		return webChatDelta{Op: webproto.ChatAppend, I: &i, Text: d.Text + n.Text}, true
	case n.Op == webproto.ChatMessage && (d.Op == webproto.ChatAppend && *d.I == n.Msg.I || d.Op == webproto.ChatMessage && d.Msg.I == n.Msg.I):
		return n, true
	case d.Op == webproto.ChatState && n.Op == webproto.ChatState:
		m := d
		if n.Busy != nil {
			m.Busy = n.Busy
		}
		if n.Title != "" {
			m.Title = n.Title
		}
		return m, true
	}
	return nil, false
}

// webApplyChat folds one chat delta into a snapshot (the browser store
// does the same).
func webApplyChat(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.ChatSnap
	var d webproto.ChatDelta
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(delta, &d); err != nil {
		return nil, err
	}
	switch d.Op {
	case webproto.ChatAppend:
		if d.I == nil || *d.I < 0 || *d.I >= len(s.Messages) || !s.Messages[*d.I].Streaming {
			return nil, fmt.Errorf("chat: append to a message that is not streaming")
		}
		s.Messages[*d.I].Text += d.Text
	case webproto.ChatMessage:
		switch {
		case d.Msg == nil || d.Msg.I < 0 || d.Msg.I > len(s.Messages):
			return nil, fmt.Errorf("chat: message out of range")
		case d.Msg.I == len(s.Messages):
			s.Messages = append(s.Messages, *d.Msg)
		default:
			s.Messages[d.Msg.I] = *d.Msg
		}
	case webproto.ChatTruncate:
		if d.N == nil || *d.N < 0 || *d.N > len(s.Messages) {
			return nil, fmt.Errorf("chat: truncate out of range")
		}
		s.Messages = s.Messages[:*d.N]
	case webproto.ChatState:
		if d.Busy != nil {
			s.Busy = *d.Busy
		}
		if d.Title != "" {
			s.Title = d.Title
		}
	case webproto.ChatSwitched:
		if d.Chat == nil {
			return nil, fmt.Errorf("chat: switched without a chat")
		}
		s = *d.Chat
	default:
		return nil, fmt.Errorf("chat: unknown op %q", d.Op)
	}
	return json.Marshal(s)
}
