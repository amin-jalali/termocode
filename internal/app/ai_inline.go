package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/ai"
	"termocode/internal/toast"
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

const (
	aiInlineDebounce   = 300 * time.Millisecond
	aiInlineTimeout    = 12 * time.Second
	aiInlineMaxTokens  = 192
	aiInlineCtxBefore  = 80
	aiInlineCtxAfter   = 30
	aiInlineDebounceID = "ai-inline"
	aiInlineReqID      = "ai-inline-req"
)

// aiInlineTriggerMsg fires after the debounce (or on Alt+\).
type aiInlineTriggerMsg struct{ manual bool }

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
		return m.debounce.Do(aiInlineDebounceID, aiInlineDebounce, func() tea.Msg {
			return aiInlineTriggerMsg{}
		})
	})
	registerNotifyHandler("termocode_ai_cancel", func(m *Model, args []any) tea.Cmd {
		m.cancelAIInline()
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

// startAIInline snapshots the caret and launches one request.
func (m *Model) startAIInline(manual bool) tea.Cmd {
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || !strings.HasPrefix(snap.Mode, "i") || snap.Buftype != "" {
		if manual {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Info, "Inline completion works in insert mode in a file")
			return c
		}
		return nil
	}
	// Skip pure-whitespace context (empty buffer): nothing to complete.
	if strings.TrimSpace(snap.Before+snap.After) == "" && !manual {
		return nil
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
	comp, ok := ai.ParseInlineResponse(msg.raw)
	if !ok {
		if msg.manual {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Info, "AI has no suggestion here")
			return c
		}
		return nil
	}
	res := ai.ResolveInlineCompletion(msg.snap.LineBefore, msg.snap.LineAfter, comp.InsertText)
	if strings.TrimSpace(res.Insert) == "" {
		return nil
	}
	imports := map[string]any{"at": 0, "lines": []string{}}
	if len(comp.ImportLines) > 0 && m.nvim != nil {
		if lines, err := m.nvim.CurrentBufLines(); err == nil {
			at, add := ai.ImportInsertion(lines, msg.snap.Filetype, comp.ImportLines)
			if len(add) > 0 {
				imports = map[string]any{"at": at, "lines": add}
			}
		}
	}
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`local a = _G._termocode_ai
if a then a.show(...) end`, msg.snap.Buf, msg.snap.Tick, msg.snap.Row, msg.snap.Col, res.Insert, res.DeleteBefore, imports)
	}
	return nil
}

// aiTriggerInline is Alt+\ / palette: ask now, skip the debounce.
func (m *Model) aiTriggerInline() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	return m.startAIInline(true)
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
