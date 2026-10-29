package app

import (
	"context"
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Next edit suggestion (G-NES). Flow:
//
//	TextChanged(I) / InsertLeave ─notify→ termocode_ai_edit ─debounce
//	400 ms→ aiNESTriggerMsg ─diff with the copy kept at the last pause→
//	request (inline model, last edits + lines around the caret) ─→
//	aiNESResultMsg ─ai.ParseNextEdit→ _termocode_ai.nes_show(...)
//
// BufEnter sends termocode_ai_edit 'seed': Go keeps a copy of the buffer
// so the first change already has something to diff against. A new edit
// cancels the request in flight; stale replies (older generation, other
// changedtick, target text changed) are dropped in Go and again in Lua.
// Files the policy blocks (secret-looking names, big buffers) are never
// sent. Tab / Esc live in Lua (ai_lua.go).

const (
	aiNESDebounce   = 400 * time.Millisecond
	aiNESTimeout    = 15 * time.Second
	aiNESMaxTokens  = 512
	aiNESDebounceID = "ai-nes"
	aiNESReqID      = "ai-nes-req"
	// aiNESMaxBufs caps the buffers whose copy is kept.
	aiNESMaxBufs = 16
)

// aiNESState is the next edit part of aiState.
type aiNESState struct {
	shadow map[int][]string        // buffer → its lines at the last pause
	hist   map[int]*ai.EditHistory // buffer → its recent edits
	cancel context.CancelFunc
	busy   bool
	// shown is what Lua shows: "" (nothing), "shown" or "jumped".
	shown string
}

// aiNESTriggerMsg fires after the pause that follows an edit.
type aiNESTriggerMsg struct{}

// aiNESResultMsg carries the model's raw answer back to Update.
type aiNESResultMsg struct {
	gen  uint64
	buf  int
	tick int
	in   ai.NextEditInput
	raw  string
	err  error
}

func init() {
	registerNotifyHandler("termocode_ai_edit", func(m *Model, args []any) tea.Cmd {
		if !m.aiNESEnabled() {
			return nil
		}
		kind := ""
		if len(args) > 1 {
			kind, _ = args[1].(string)
		}
		if kind == "seed" {
			if len(args) > 0 {
				m.aiNESSeed(notifyInt(args[0]))
			}
			return nil
		}
		m.cancelAINES()
		return m.debounce.Do(aiNESDebounceID, aiNESDebounce, func() tea.Msg { return aiNESTriggerMsg{} })
	})
	registerNotifyHandler("termocode_ai_nes", func(m *Model, args []any) tea.Cmd {
		st := ""
		if len(args) > 0 {
			st, _ = args[0].(string)
		}
		if st == "gone" {
			st = ""
		}
		m.aiS().nes.shown = st
		return nil
	})
}

// notifyInt reads an integer notify argument (msgpack ints arrive as
// int64 or uint64).
func notifyInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

// aiNESEnabled is the effective next edit toggle.
func (m *Model) aiNESEnabled() bool {
	return m.aiS().settings.NextEditEnabled() && m.aiConfigured()
}

// cancelAINES drops the pending pause and aborts the request in flight.
func (m *Model) cancelAINES() {
	s := m.aiS()
	m.debounce.Cancel(aiNESDebounceID)
	m.debounce.Next(aiNESReqID)
	if s.nes.cancel != nil {
		s.nes.cancel()
		s.nes.cancel = nil
	}
	s.nes.busy = false
}

// aiNESBuf is one buffer as read from nvim.
type aiNESBuf struct {
	Path  string   `json:"path"`
	Bt    string   `json:"bt"`
	Lines []string `json:"lines"`
}

// aiNESReadBuf reads buffer buf when it is a file small enough to send.
func (m *Model) aiNESReadBuf(buf int) (aiNESBuf, bool) {
	var b aiNESBuf
	if m.nvim == nil || buf <= 0 {
		return b, false
	}
	var raw string
	err := m.nvim.EvalLuaArgs(`local b, max = ...
if not vim.api.nvim_buf_is_valid(b) or vim.api.nvim_buf_line_count(b) > max then return '' end
return vim.json.encode({ path = vim.api.nvim_buf_get_name(b), bt = vim.bo[b].buftype,
  lines = vim.api.nvim_buf_get_lines(b, 0, -1, false) })`, &raw, buf, ai.NextEditMaxBufLines)
	if err != nil || raw == "" || json.Unmarshal([]byte(raw), &b) != nil || b.Bt != "" {
		return b, false
	}
	size := 0
	for _, l := range b.Lines {
		size += len(l) + 1
	}
	if ok, _ := ai.NextEditAllowed(b.Path, len(b.Lines), size); !ok {
		return b, false
	}
	return b, true
}

// aiNESSeed keeps a first copy of buffer buf to diff the next edit with.
func (m *Model) aiNESSeed(buf int) {
	s := m.aiS()
	if _, have := s.nes.shadow[buf]; have {
		return
	}
	b, ok := m.aiNESReadBuf(buf)
	if !ok {
		return
	}
	m.aiNESKeep(buf, b.Lines)
}

// aiNESKeep stores the copy of a buffer (forgetting all copies when there
// are too many buffers).
func (m *Model) aiNESKeep(buf int, lines []string) {
	s := m.aiS()
	if s.nes.shadow == nil || len(s.nes.shadow) >= aiNESMaxBufs {
		if _, have := s.nes.shadow[buf]; !have {
			s.nes.shadow = map[int][]string{}
			s.nes.hist = map[int]*ai.EditHistory{}
		}
	}
	s.nes.shadow[buf] = lines
}

// startAINES diffs the current buffer with its copy and, when it changed,
// asks the model for the next edit.
func (m *Model) startAINES() tea.Cmd {
	if !m.aiNESEnabled() {
		return nil
	}
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || snap.Buftype != "" {
		return nil
	}
	b, ok := m.aiNESReadBuf(snap.Buf)
	s := m.aiS()
	if !ok {
		delete(s.nes.shadow, snap.Buf)
		return nil
	}
	old, had := s.nes.shadow[snap.Buf]
	m.aiNESKeep(snap.Buf, b.Lines)
	if !had {
		return nil
	}
	hunk, changed := ai.DiffHunk(old, b.Lines)
	if !changed {
		return nil
	}
	h := s.nes.hist[snap.Buf]
	if h == nil {
		h = &ai.EditHistory{}
		s.nes.hist[snap.Buf] = h
	}
	h.Add(hunk)
	in := ai.NextEditInput{Filetype: snap.Filetype, Path: b.Path, Lines: b.Lines, CursorRow: snap.Row,
		Edits: append([]ai.EditHunk(nil), h.Edits()...)}
	m.cancelAINES()
	gen := m.debounce.Next(aiNESReqID)
	ctx, cancel := context.WithTimeout(context.Background(), aiNESTimeout)
	s.nes.cancel = cancel
	s.nes.busy = true
	prov, model := s.provider, s.cfg.InlineModel
	buf, tick := snap.Buf, snap.Tick
	return func() tea.Msg {
		defer cancel()
		temp := 0.0
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{
			Messages:    ai.BuildNextEditPrompt(in),
			Model:       model,
			MaxTokens:   aiNESMaxTokens,
			Temperature: &temp,
		}))
		return aiNESResultMsg{gen: gen, buf: buf, tick: tick, in: in, raw: text, err: last.Err}
	}
}

// onAINESResult checks the answer and shows it in nvim.
func (m *Model) onAINESResult(msg aiNESResultMsg) tea.Cmd {
	s := m.aiS()
	if !m.debounce.Current(aiNESReqID, msg.gen) {
		return nil // superseded
	}
	s.nes.busy = false
	s.nes.cancel = nil
	if msg.err != nil {
		if msg.err != context.Canceled && msg.err != context.DeadlineExceeded {
			recordError("[ai] next edit: " + ai.Redact(msg.err.Error()))
		}
		return nil
	}
	ne, ok := ai.ParseNextEdit(msg.raw, msg.in)
	if !ok || m.nvim == nil {
		return nil
	}
	old := msg.in.Lines[ne.Start : ne.End+1]
	var out string
	_ = m.nvim.EvalLuaArgs(`local a = _G._termocode_ai
if a and a.nes_show and a.nes_show(...) then return 'y' end
return ''`, &out, msg.buf, msg.tick, ne.Start, ne.End, old, ne.Lines)
	return nil
}
