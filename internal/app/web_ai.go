package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's AI (WEBP-3): ghost text (ai.complete) and the
// next edit suggestion (ai.next_edit). The page names a buffer it opened,
// the changedtick its text is at and the caret; the hub reads the text
// from its mirror of that buffer and asks the laptop's provider with the
// laptop's own prompts and suggestion policy, as the laptop's editor
// does. The laptop's settings decide (ai_inline, ai_next_edit); a file
// that may hold secrets never goes to the model. A newer request of the
// same connection cancels the older one.

func init() {
	webOps[webproto.OpAIComplete] = (*Model).webAIComplete
	webOps[webproto.OpAINextEdit] = (*Model).webAINextEdit
	webOps[webproto.OpAIAction] = (*Model).webAIAction
}

// webAICalls is the page's AI calls in flight (Update only).
type webAICalls struct {
	inline map[webAIKey]*webAIRun
	nes    map[int]*webAINES // by buffer
}

// webAIKey is one kind of AI call (its op) of one connection.
type webAIKey struct {
	conn *webConn
	op   string
}

// webAINES is a buffer's text at the page's last pause and its recent
// edits (the next edit suggestion's memory, as the laptop keeps its own).
type webAINES struct {
	shadow []string
	hist   ai.EditHistory
}

// webAIRun is one AI call of a connection.
type webAIRun struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// webAIProvider is the provider of the page's ghost text and next edits
// (a seam for tests).
var webAIProvider = func(m *Model) (ai.Provider, string) {
	s := m.aiS()
	return s.provider, s.cfg.InlineModel
}

// webAIBuf is the mirror of a file buffer the page opened, at the tick
// the page asks at (false: answered).
func (m *Model) webAIBuf(call *webCall, buf, tick int) (*webBuf, bool) {
	if m.web == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil, false
	}
	b := m.web.filesState().bufs[buf]
	if b == nil || b.term || b.first != 0 || b.cap != 0 {
		call.fail(webproto.CodeNotFound, "Open the file first.")
		return nil, false
	}
	if b.tick != tick {
		webFailData(call, webproto.CodeConflict, "The file changed. Ask again.", webproto.ConflictData{Tick: b.tick})
		return nil, false
	}
	return b, true
}

// webCol16 turns a UTF-16 column of row into a byte offset (clamped).
func webCol16(row string, col16 int) int {
	n := 0
	for i, r := range row {
		if n >= col16 {
			return i
		}
		n += len(utf16.Encode([]rune{r}))
	}
	return len(row)
}

// webLen16 is the UTF-16 length of s.
func webLen16(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// webInlineSnap is the laptop's inline context (_termocode_ai.context)
// read from the mirror: aiInlineCtxBefore rows before the caret's row
// and aiInlineCtxAfter after it.
func webInlineSnap(b *webBuf, row, col int) aiInlineSnapshot {
	cur := ""
	if row < len(b.rows) {
		cur = b.rows[row]
	}
	col = min(col, len(cur))
	first := max(row-aiInlineCtxBefore, 0)
	last := min(row+aiInlineCtxAfter, len(b.rows)-1)
	var before, after []string
	for i := first; i < row && i < len(b.rows); i++ {
		before = append(before, b.rows[i])
	}
	before = append(before, cur[:col])
	after = append(after, cur[col:])
	for i := row + 1; i <= last; i++ {
		after = append(after, b.rows[i])
	}
	return aiInlineSnapshot{Buf: b.buf, Tick: b.tick, Row: row, Col: col, Mode: "i", Filetype: b.ft, Path: b.path,
		Before: strings.Join(before, "\n"), After: strings.Join(after, "\n"), LineBefore: cur[:col], LineAfter: cur[col:]}
}

// webAINone answers "nothing here"; why only when the user asked.
func webAINone(call *webCall, manual bool, why string) {
	if !manual {
		why = ""
	}
	call.reply(webproto.AICompletion{None: true, Why: why})
}

// webAIStart cancels the connection's older AI call and starts the new
// one (it ends with the connection, the next call or the timeout).
func (m *Model) webAIStart(call *webCall) *webAIRun {
	st := &m.web.ai
	if st.inline == nil {
		st.inline = map[webAIKey]*webAIRun{}
	}
	k := webAIKey{call.conn, call.op}
	if old := st.inline[k]; old != nil {
		old.cancel()
	}
	r := &webAIRun{}
	r.ctx, r.cancel = context.WithTimeout(call.ctx, aiInlineTimeout)
	st.inline[k] = r
	return r
}

// end forgets the finished call, unless a newer one took its place.
func (r *webAIRun) end(call *webCall) tea.Msg {
	r.cancel()
	return applyMsg(func(m *Model) tea.Cmd {
		k := webAIKey{call.conn, call.op}
		if m.web != nil && m.web.ai.inline[k] == r {
			delete(m.web.ai.inline, k)
		}
		return nil
	})
}

// webAIComplete is ai.complete: the laptop's ghost text at the page's
// caret.
func (m *Model) webAIComplete(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AICompleteArgs)
	b, ok := m.webAIBuf(call, a.Buf, a.Tick)
	if !ok {
		return nil
	}
	switch {
	case !m.aiConfigured():
		webAINone(call, a.Manual, "Set up AI on the laptop first.")
		return nil
	case !a.Manual && !m.aiInlineEnabled():
		webAINone(call, false, "")
		return nil
	case ai.IsSecretFileName(b.path):
		webAINone(call, a.Manual, "This file may hold secrets: AI does not read it.")
		return nil
	case a.Line >= len(b.rows):
		call.fail(webproto.CodeBadRequest, "That place is not in the file.")
		return nil
	}
	snap := webInlineSnap(b, a.Line, webCol16(b.rows[a.Line], a.Col))
	if !a.Manual {
		if ok, _ := ai.ShouldAutoTrigger(snap.inlineCtx()); !ok {
			webAINone(call, false, "")
			return nil
		}
	}
	rows := append([]string(nil), b.rows...)
	prov, model := webAIProvider(m)
	run := m.webAIStart(call)
	ctx := run.ctx
	call.later()
	return func() tea.Msg {
		temp := 0.1
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{
			Messages:    ai.BuildInlinePrompt(snap.Filetype, snap.Path, snap.Before, snap.After),
			Model:       model,
			MaxTokens:   aiInlineMaxTokens,
			Temperature: &temp,
		}))
		if last.Err != nil {
			if ctx.Err() == nil {
				recordError("[web] ai.complete: " + ai.Redact(last.Err.Error()))
			}
			webAINone(call, a.Manual, "AI completion failed.")
			return run.end(call)
		}
		show, ok := aiInlineAnswer(snap.inlineCtx(), snap.Filetype, text, a.Manual, func() []string { return rows })
		if !ok || len(show.insert) > webproto.MaxAIInsert || !utf8.ValidString(show.insert) {
			webAINone(call, a.Manual, show.why)
			return run.end(call)
		}
		to := webLen16(snap.LineBefore)
		out := webproto.AICompletion{Line: a.Line, From: webLen16(snap.LineBefore[:len(snap.LineBefore)-show.del]), To: to, Text: show.insert}
		if n := len(show.imports); n > 0 && n <= webproto.MaxAIImports {
			out.ImportAt, out.Imports = show.importAt, show.imports
		}
		call.reply(out)
		return run.end(call)
	}
}

// nesOf is a buffer's next edit memory (forgetting all copies when there
// are too many buffers, as the laptop does).
func (st *webAICalls) nesOf(buf int) *webAINES {
	if st.nes == nil || (len(st.nes) >= aiNESMaxBufs && st.nes[buf] == nil) {
		st.nes = map[int]*webAINES{}
	}
	n := st.nes[buf]
	if n == nil {
		n = &webAINES{}
		st.nes[buf] = n
	}
	return n
}

// webAINextEdit is ai.next_edit: after the page's pause, the laptop's
// next edit suggestion from the edits since the last pause.
func (m *Model) webAINextEdit(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AINextEditArgs)
	b, ok := m.webAIBuf(call, a.Buf, a.Tick)
	if !ok {
		return nil
	}
	none := func() tea.Cmd {
		call.reply(webproto.AINextEdit{None: true, Old: []string{}, Lines: []string{}})
		return nil
	}
	st := &m.web.ai
	size := 0
	for _, l := range b.rows {
		size += len(l) + 1
	}
	if !m.aiNESEnabled() {
		return none()
	}
	if ok, _ := ai.NextEditAllowed(b.path, len(b.rows), size); !ok {
		delete(st.nes, a.Buf)
		return none()
	}
	n := st.nesOf(a.Buf)
	old, had := n.shadow, n.shadow != nil
	n.shadow = append([]string(nil), b.rows...)
	if a.Seed || !had {
		return none()
	}
	hunk, changed := ai.DiffHunk(old, n.shadow)
	if !changed {
		return none()
	}
	n.hist.Add(hunk)
	in := ai.NextEditInput{Filetype: b.ft, Path: b.path, Lines: n.shadow, CursorRow: min(a.Line, max(len(n.shadow)-1, 0)),
		Edits: append([]ai.EditHunk(nil), n.hist.Edits()...)}
	prov, model := webAIProvider(m)
	run := m.webAIStart(call)
	ctx := run.ctx
	call.later()
	return func() tea.Msg {
		temp := 0.0
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{
			Messages:    ai.BuildNextEditPrompt(in),
			Model:       model,
			MaxTokens:   aiNESMaxTokens,
			Temperature: &temp,
		}))
		if last.Err != nil && ctx.Err() == nil {
			recordError("[web] ai.next_edit: " + ai.Redact(last.Err.Error()))
		}
		ne, ok := ai.ParseNextEdit(text, in)
		if last.Err != nil || !ok || len(ne.Lines) > webproto.MaxAIEditLines || ne.End-ne.Start >= webproto.MaxAIEditLines {
			none()
			return run.end(call)
		}
		call.reply(webproto.AINextEdit{Start: ne.Start, End: ne.End, Old: in.Lines[ne.Start : ne.End+1], Lines: ne.Lines})
		return run.end(call)
	}
}

// webAIActionTimeout caps one Fix, Edit or Doc request (as the laptop's).
const webAIActionTimeout = 3 * time.Minute

// webAIAction is ai.action: the editor's Explain, Fix, Edit and Doc on
// the page's selection or caret, with the laptop's prompts. Explain asks
// in the laptop's open chat (the page shows the chat); the others answer
// with the lines to replace: nothing changes before the user accepts.
func (m *Model) webAIAction(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AIActionArgs)
	b, ok := m.webAIBuf(call, a.Buf, a.Tick)
	if !ok {
		return nil
	}
	s := m.aiS()
	switch {
	case !m.aiConfigured():
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	case ai.IsSecretFileName(b.path):
		webFail(call, webErrSecret())
		return nil
	case a.End > len(b.rows) || a.Line >= max(len(b.rows), 1):
		call.fail(webproto.CodeBadRequest, "That place is not in the file.")
		return nil
	}
	full := append([]string(nil), b.rows...)
	if len(full) == 0 {
		full = []string{""}
	}
	t := aiRangeTarget{buf: b.buf, path: b.path, ft: b.ft, start: a.Start, end: a.End, cursor: a.Line, full: full, hadSelection: a.End > a.Start}
	if !t.hadSelection {
		t.start, t.end = a.Line, a.Line+1
	}
	var title, instr string
	switch a.Kind {
	case webproto.AIExplain:
		if s.streaming {
			call.fail(webproto.CodeBusy, "The AI is still answering. Stop it or wait.")
			return nil
		}
		att, q := aiExplainAsk(t)
		s.attachments = append(s.attachments, att)
		cmd := m.sendAIChat(q)
		call.reply(webproto.AIAction{Kind: a.Kind, Chat: true, ChatID: s.conv.ID, Lines: []string{}})
		return cmd
	case webproto.AIFix:
		diags := m.webAIDiagnostics(b.path, a.Line)
		if len(diags) == 0 {
			call.fail(webproto.CodeNotFound, "No problem on this line.")
			return nil
		}
		t, instr = aiFixAsk(t, diags)
		title = "AI fix"
	case webproto.AIEdit:
		instr = strings.TrimSpace(a.Instruction)
		title = "AI edit: " + instr
	case webproto.AIDoc:
		t.start, t.end = t.cursor, t.cursor+1
		instr, title = aiDocInstruction, "AI doc comment"
	}
	prov, req := s.provider, aiEditRequest(t, s.cfg.Model, instr)
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webAIActionTimeout)
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, req))
		if last.Err != nil {
			if ctx.Err() == nil {
				recordError("[web] ai.action: " + ai.Redact(last.Err.Error()))
			}
			call.fail(webproto.CodeBusy, "The AI request failed. Try again.")
			return nil
		}
		out := webproto.AIAction{Kind: a.Kind, Title: title, Start: t.start, End: t.end, Lines: []string{}}
		repl, ok := aiEditAnswer(text)
		if !ok || equalLines(t.full[t.start:t.end], repl) {
			out.None = true
			call.reply(out)
			return nil
		}
		out.Lines = repl
		call.reply(out)
		return nil
	}
}

// webAIDiagnostics are the laptop's problems on row (zero-based) of the
// workspace file rel, as Fix with AI words them.
func (m *Model) webAIDiagnostics(rel string, row int) []string {
	if m.problems == nil {
		return nil
	}
	names := []string{"error", "warning", "info", "hint"}
	var out []string
	for _, d := range m.problems.items {
		if d.Line-1 != row {
			continue
		}
		if r, ok := m.webRel(d.Path); !ok || r != rel {
			continue
		}
		sev := "?"
		if d.Severity >= 1 && d.Severity <= len(names) {
			sev = names[d.Severity-1]
		}
		out = append(out, fmt.Sprintf("line %d: [%s] %s", d.Line, sev, d.Message))
	}
	return out
}
