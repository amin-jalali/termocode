package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Inline ghost-text completion (Group A). Flow:
//
//	TextChangedI ─notify→ termocode_ai_changed ─debounce 300 ms→
//	aiInlineTriggerMsg ─_termocode_ai.context()→ request (fast model,
//	tagged with a debounce generation) ─→ aiInlineResultMsg ─parse +
//	ResolveInlineCompletion→ _termocode_ai.show(...)
//
// Any caret move / InsertLeave cancels the in-flight request; stale
// replies (older generation, different changedtick or caret) are dropped
// in Go and again in Lua.
//
// Suggestion policy (internal/ai/inline_policy.go): automatic requests
// skip mid-word, code right of the caret, comments, top-level empty
// lines and the empty line right after a closed block; an empty line in
// a body waits for a longer pause. Answers are filtered (no invented
// declarations, no repeats of nearby lines, one line mid-statement).
// Typing the start of a shown ghost keeps the rest on screen with no new
// request (type-through).

const (
	aiInlineDebounce   = 300 * time.Millisecond
	aiInlineBlankPause = 700 * time.Millisecond // extra wait on an empty line
	aiInlineTimeout    = 12 * time.Second
	aiInlineMaxTokens  = 192
	aiInlineCtxBefore  = 80
	aiInlineCtxAfter   = 30
	aiInlineDebounceID = "ai-inline"
	aiInlineReqID      = "ai-inline-req"
)

// aiInlineTriggerMsg fires after the debounce (or on Alt+\).
// settled: the extra empty-line pause has passed too.
type aiInlineTriggerMsg struct{ manual, settled bool }

// aiInlineSnapshot is the decoded _termocode_ai.context() result.
type aiInlineSnapshot struct {
	Buf        int    `json:"buf"`
	Tick       int    `json:"tick"`
	Row        int    `json:"row"`
	Col        int    `json:"col"`
	Mode       string `json:"mode"`
	Filetype   string `json:"ft"`
	Buftype    string `json:"bt"`
	Path       string `json:"path"`
	Before     string `json:"before"`
	After      string `json:"after"`
	LineBefore string `json:"line_before"`
	LineAfter  string `json:"line_after"`
}

// inlineCtx is the policy view of the snapshot.
func (s aiInlineSnapshot) inlineCtx() ai.InlineContext {
	return ai.InlineContext{Filetype: s.Filetype, Before: s.Before, After: s.After,
		LineBefore: s.LineBefore, LineAfter: s.LineAfter}
}

// aiInlineResultMsg carries the model's raw answer back to Update.
type aiInlineResultMsg struct {
	gen    uint64
	manual bool
	snap   aiInlineSnapshot
	raw    string
	err    error
}

func init() {
	registerNotifyHandler("termocode_ai_changed", func(m *Model, args []any) tea.Cmd {
		if !m.aiInlineEnabled() {
			return nil
		}
		m.cancelAIInline()
		if m.aiInlineTypeThrough() {
			return nil
		}
		return m.debounce.Do(aiInlineDebounceID, aiInlineDebounce, func() tea.Msg {
			return aiInlineTriggerMsg{}
		})
	})
	registerNotifyHandler("termocode_ai_cancel", func(m *Model, args []any) tea.Cmd {
		m.cancelAIInline()
		if len(args) > 0 && args[0] == "leave" {
			m.aiS().inlineCache.Reset()
		}
		return nil
	})
}

// cancelAIInline drops the pending debounce and aborts any request.
func (m *Model) cancelAIInline() {
	s := m.aiS()
	m.debounce.Cancel(aiInlineDebounceID)
	m.debounce.Next(aiInlineReqID)
	if s.inlineCancel != nil {
		s.inlineCancel()
		s.inlineCancel = nil
	}
	s.inlineBusy = false
}

// aiInlineSnapshotNow reads the cursor context from nvim.
func (m *Model) aiInlineSnapshotNow() (aiInlineSnapshot, bool) {
	var snap aiInlineSnapshot
	if m.nvim == nil {
		return snap, false
	}
	raw, err := m.nvim.EvalLuaString(`if not _G._termocode_ai then return '' end
return vim.json.encode(_G._termocode_ai.context(` + itoa(aiInlineCtxBefore) + `, ` + itoa(aiInlineCtxAfter) + `))`)
	if err != nil || raw == "" {
		return snap, false
	}
	if json.Unmarshal([]byte(raw), &snap) != nil {
		return snap, false
	}
	return snap, true
}

// aiInlineTypeThrough re-shows the rest of the last ghost when the user
// typed its first characters. true: shown, no request needed.
func (m *Model) aiInlineTypeThrough() bool {
	s := m.aiS()
	if s.inlineCache.Empty() {
		return false
	}
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || !strings.HasPrefix(snap.Mode, "i") {
		return false
	}
	rest, ok := s.inlineCache.Match(snap.Buf, snap.Row, snap.LineBefore)
	if !ok {
		s.inlineCache.Reset()
		return false
	}
	if !m.aiShowGhost(snap, rest, 0, s.inlineImp) {
		return false
	}
	s.inlineCache.Remember(snap.Buf, snap.Row, snap.LineBefore, rest)
	return true
}

// aiShowGhost draws ghost text at the snapshot's caret (Lua re-checks
// buffer, tick and caret).
func (m *Model) aiShowGhost(snap aiInlineSnapshot, insert string, del int, imports map[string]any) bool {
	if m.nvim == nil {
		return false
	}
	if imports == nil {
		imports = map[string]any{"at": 0, "lines": []string{}}
	}
	var out string
	err := m.nvim.EvalLuaArgs(`local a = _G._termocode_ai
if a and a.show(...) then return 'y' end
return ''`, &out, snap.Buf, snap.Tick, snap.Row, snap.Col, insert, del, imports)
	return err == nil && out == "y"
}

// startAIInline snapshots the caret and launches one request. Automatic
// requests (not manual) go through the suggestion policy first.
func (m *Model) startAIInline(manual, settled bool) tea.Cmd {
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || !strings.HasPrefix(snap.Mode, "i") || snap.Buftype != "" {
		if manual {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Info, "Inline completion works in insert mode in a file")
			return c
		}
		return nil
	}
	if !manual {
		c := snap.inlineCtx()
		if ok, _ := ai.ShouldAutoTrigger(c); !ok {
			return nil
		}
		if ai.NeedsLongerPause(c) && !settled {
			return m.debounce.Do(aiInlineDebounceID, aiInlineBlankPause, func() tea.Msg {
				return aiInlineTriggerMsg{settled: true}
			})
		}
	}
	s := m.aiS()
	m.cancelAIInline()
	gen := m.debounce.Next(aiInlineReqID)
	ctx, cancel := context.WithTimeout(context.Background(), aiInlineTimeout)
	s.inlineCancel = cancel
	s.inlineBusy = true
	prov, model := s.provider, s.cfg.InlineModel
	return func() tea.Msg {
		defer cancel()
		temp := 0.1
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{
			Messages:    ai.BuildInlinePrompt(snap.Filetype, snap.Path, snap.Before, snap.After),
			Model:       model,
			MaxTokens:   aiInlineMaxTokens,
			Temperature: &temp,
		}))
		return aiInlineResultMsg{gen: gen, manual: manual, snap: snap, raw: text, err: last.Err}
	}
}

// onAIInlineResult validates freshness and draws the ghost text.
func (m *Model) onAIInlineResult(msg aiInlineResultMsg) tea.Cmd {
	s := m.aiS()
	if !m.debounce.Current(aiInlineReqID, msg.gen) {
		return nil // superseded
	}
	s.inlineBusy = false
	s.inlineCancel = nil
	if msg.err != nil {
		if msg.err == context.Canceled || msg.err == context.DeadlineExceeded {
			return nil
		}
		recordError("[ai] inline: " + ai.Redact(msg.err.Error()))
		if msg.manual {
			return m.aiToastErr("AI completion failed", msg.err)
		}
		return nil
	}
	show, ok := aiInlineAnswer(msg.snap.inlineCtx(), msg.snap.Filetype, msg.raw, msg.manual, func() []string {
		if m.nvim == nil {
			return nil
		}
		lines, _ := m.nvim.CurrentBufLines()
		return lines
	})
	if !ok {
		if msg.manual && show.why != "" {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Info, show.why)
			return c
		}
		return nil
	}
	imports := map[string]any{"at": 0, "lines": []string{}}
	if len(show.imports) > 0 {
		imports = map[string]any{"at": show.importAt, "lines": show.imports}
	}
	s.inlineCache.Reset()
	if m.aiShowGhost(msg.snap, show.insert, show.del, imports) && show.del == 0 {
		s.inlineCache.Remember(msg.snap.Buf, msg.snap.Row, msg.snap.LineBefore, show.insert)
		s.inlineImp = imports
	}
	return nil
}

// aiInlineShown is what one inline answer shows at the caret: delete del
// bytes left of it, insert insert, and add the import lines before row
// importAt. why: a manual request that got nothing says so.
type aiInlineShown struct {
	insert   string
	del      int
	importAt int
	imports  []string
	why      string
}

// aiNoSuggestion is the answer to a manual request that got nothing.
const aiNoSuggestion = "AI has no suggestion here"

// aiInlineAnswer turns the model's raw answer into ghost text with the
// suggestion policy (parse, filter, continuation or rewrite, imports).
// lines reads the buffer for the imports, only when there are some. The
// laptop's editor and the web's use it alike.
func aiInlineAnswer(c ai.InlineContext, ft, raw string, manual bool, lines func() []string) (aiInlineShown, bool) {
	comp, ok := ai.ParseInlineResponse(raw)
	if !ok {
		return aiInlineShown{why: aiNoSuggestion}, false
	}
	text, ok := ai.FilterInlineCompletion(c, comp.InsertText, manual)
	if !ok {
		return aiInlineShown{why: aiNoSuggestion}, false
	}
	res := ai.ResolveInlineCompletion(c.LineBefore, c.LineAfter, text)
	if strings.TrimSpace(res.Insert) == "" {
		return aiInlineShown{}, false
	}
	out := aiInlineShown{insert: res.Insert, del: res.DeleteBefore}
	if len(comp.ImportLines) > 0 {
		if ls := lines(); ls != nil {
			out.importAt, out.imports = ai.ImportInsertion(ls, ft, comp.ImportLines)
		}
	}
	return out, true
}

// aiTriggerInline is Alt+\ / palette: ask now, skip the debounce.
func (m *Model) aiTriggerInline() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	return m.startAIInline(true, true)
}

// aiToggleInline flips ai_inline, persists it and syncs Lua.
func (m *Model) aiToggleInline() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	s := m.aiS()
	on := !s.settings.InlineEnabled()
	s.settings.Inline = &on
	if path, err := ai.ConfigPath(); err == nil {
		cur := ai.LoadSettings(path)
		cur.Inline = &on
		if err := ai.SaveSettings(path, cur); err != nil {
			return m.aiToastErr("Settings error", err)
		}
	}
	if !on {
		m.cancelAIInline()
		if m.nvim != nil {
			_ = m.nvim.ExecLua(`if _G._termocode_ai then _G._termocode_ai.clear() end`)
		}
	}
	m.syncAIInline()
	label := "AI inline completions off"
	if on {
		label = "AI inline completions on"
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, label)
	return c
}
