package app

import (
	"context"
	"strings"
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
}

// webAICalls is the page's AI calls in flight (Update only).
type webAICalls struct {
	inline map[*webConn]*webAIRun
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
		st.inline = map[*webConn]*webAIRun{}
	}
	if old := st.inline[call.conn]; old != nil {
		old.cancel()
	}
	r := &webAIRun{}
	r.ctx, r.cancel = context.WithTimeout(call.ctx, aiInlineTimeout)
	st.inline[call.conn] = r
	return r
}

// end forgets the finished call, unless a newer one took its place.
func (r *webAIRun) end(call *webCall) tea.Msg {
	r.cancel()
	return applyMsg(func(m *Model) tea.Cmd {
		if m.web != nil && m.web.ai.inline[call.conn] == r {
			delete(m.web.ai.inline, call.conn)
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
