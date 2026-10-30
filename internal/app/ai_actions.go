package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// AI code actions (Group A): Explain, Fix diagnostic, Edit selection,
// Doc comment. Edits are review-then-apply: the proposal opens in the
// side-by-side nvim diff (current buffer vs proposed text) and a confirm
// dialog offers Apply / Discard / Review. Apply is ONE nvim_buf_set_lines
// call, so a single `u` undoes it.

// aiRangeTarget is the buffer region an action works on.
type aiRangeTarget struct {
	buf          int
	path, ft     string
	start, end   int // 0-based, end-exclusive line range
	cursor       int // 0-based cursor line
	full         []string
	hadSelection bool
}

// aiProposal is a pending AI edit.
type aiProposal struct {
	target aiRangeTarget
	repl   []string
	title  string
}

// aiActionResultMsg is the one-shot model answer.
type aiActionResultMsg struct {
	gen    uint64
	kind   string
	title  string
	target aiRangeTarget
	text   string
	err    error
}

// Synthesized code-action kinds for the "AI" group (code_actions*.go).
const (
	synthKindAIExplain = "ai.explain"
	synthKindAIFix     = "ai.fix"
	synthKindAIEdit    = "ai.edit"
	synthKindAIDoc     = "ai.doc"
)

func isAISynthKind(kind string) bool { return strings.HasPrefix(kind, "ai.") }

// aiCodeActions adds the AI group to the Ctrl+. picker (only when a file
// is open; unconfigured providers still list them — picking one shows the
// "configure" toast).
func (m *Model) aiCodeActions(path string, line int, add func(title, kind string, payload synthCodeAction)) {
	if path == "" {
		return
	}
	add("Explain with AI", synthKindAIExplain, synthCodeAction{Path: path, Line: line})
	if m.aiLineHasDiagnostic(line) {
		add("Fix with AI", synthKindAIFix, synthCodeAction{Path: path, Line: line})
	}
	add("Edit with AI…", synthKindAIEdit, synthCodeAction{Path: path, Line: line})
	add("Generate Doc Comment with AI", synthKindAIDoc, synthCodeAction{Path: path, Line: line})
}

// applyAICodeAction runs a picked AI code action.
func (m *Model) applyAICodeAction(a synthCodeAction) tea.Cmd {
	switch a.Kind {
	case synthKindAIExplain:
		return m.aiExplain()
	case synthKindAIFix:
		return m.aiFixDiagnostic()
	case synthKindAIEdit:
		return m.aiEditSelection()
	case synthKindAIDoc:
		return m.aiDocComment()
	}
	return nil
}

// aiCaptureTarget reads the current buffer + selection (visual mode) or
// cursor line. Leaves visual mode. ok=false without a normal file buffer.
func (m *Model) aiCaptureTarget(_ bool) (aiRangeTarget, bool) {
	var t aiRangeTarget
	if m.nvim == nil {
		return t, false
	}
	raw, err := m.nvim.EvalLuaString(`
local buf = vim.api.nvim_get_current_buf()
if vim.bo[buf].buftype ~= '' then return '' end
local mode = vim.fn.mode()
local s, e, sel = vim.fn.line('.'), vim.fn.line('.'), false
if mode == 'v' or mode == 'V' or mode == '\22' then
  s, e, sel = vim.fn.line('v'), vim.fn.line('.'), true
  if s > e then s, e = e, s end
  vim.cmd('normal! \27')
end
return vim.json.encode({
  buf = buf, path = vim.api.nvim_buf_get_name(buf), ft = vim.bo[buf].filetype,
  s = s - 1, e = e, cur = vim.fn.line('.') - 1, sel = sel,
  lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false),
})`)
	if err != nil || raw == "" {
		return t, false
	}
	var r struct {
		Buf   int      `json:"buf"`
		Path  string   `json:"path"`
		Ft    string   `json:"ft"`
		S     int      `json:"s"`
		E     int      `json:"e"`
		Cur   int      `json:"cur"`
		Sel   bool     `json:"sel"`
		Lines []string `json:"lines"`
	}
	if json.Unmarshal([]byte(raw), &r) != nil || r.Path == "" {
		return t, false
	}
	if r.E > len(r.Lines) {
		r.E = len(r.Lines)
	}
	if r.S < 0 || r.S > r.E {
		r.S = r.E
	}
	return aiRangeTarget{buf: r.Buf, path: r.Path, ft: r.Ft, start: r.S, end: r.E, cursor: r.Cur, full: r.Lines, hadSelection: r.Sel}, true
}

// aiLineHasDiagnostic reports whether the 1-based line has a diagnostic.
func (m *Model) aiLineHasDiagnostic(line int) bool {
	if m.nvim == nil || line <= 0 {
		return false
	}
	s, _ := m.nvim.EvalLuaString(fmt.Sprintf(`return #vim.diagnostic.get(0, { lnum = %d }) > 0 and 'y' or ''`, line-1))
	return s == "y"
}

// aiDiagnosticsAt returns "line N: [severity] message" strings for a line.
func (m *Model) aiDiagnosticsAt(row int) []string {
	if m.nvim == nil {
		return nil
	}
	s, _ := m.nvim.EvalLuaString(fmt.Sprintf(`
local out = {}
local names = { 'error', 'warning', 'info', 'hint' }
for _, d in ipairs(vim.diagnostic.get(0, { lnum = %d })) do
  out[#out + 1] = 'line ' .. (d.lnum + 1) .. ': [' .. (names[d.severity] or '?') .. '] ' .. (d.message or '')
end
return table.concat(out, '\n')`, row))
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// aiExplain sends the selection / current line to the chat.
func (m *Model) aiExplain() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	s := m.aiS()
	att, q := aiExplainAsk(t)
	s.attachments = append(s.attachments, att)
	m.openAIPanel(true)
	return m.sendAIChat(q)
}

// aiExplainAsk is Explain's attachment (the selection, or ±10 lines
// around the cursor: a single line explains little) and question.
func aiExplainAsk(t aiRangeTarget) (aiAttachment, string) {
	start, end := t.start, t.end
	if !t.hadSelection {
		start, end = maxInt(t.cursor-10, 0), minInt(t.cursor+11, len(t.full))
	}
	label := fmt.Sprintf("%s:%d-%d", filepath.Base(t.path), start+1, end)
	q := "Explain this code."
	if !t.hadSelection {
		q = fmt.Sprintf("Explain the code around line %d.", t.cursor+1)
	}
	return aiAttachment{Label: label, Text: strings.Join(t.full[start:end], "\n")}, q
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// aiFixDiagnostic proposes a fix for the diagnostic(s) on the cursor line.
func (m *Model) aiFixDiagnostic() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	diags := m.aiDiagnosticsAt(t.cursor)
	if len(diags) == 0 {
		return m.aiInfo("No diagnostic on this line")
	}
	t, instr := aiFixAsk(t, diags)
	return m.runAIEdit(t, "fix", "AI fix", instr)
}

// aiFixAsk is Fix's range (the selection, or the lines around the
// cursor) and instruction.
func aiFixAsk(t aiRangeTarget, diags []string) (aiRangeTarget, string) {
	if !t.hadSelection {
		t.start, t.end = maxInt(t.cursor-4, 0), minInt(t.cursor+5, len(t.full))
	}
	return t, "Fix these problems:\n" + strings.Join(diags, "\n")
}

// aiEditSelection asks for an instruction, then proposes the edit.
func (m *Model) aiEditSelection() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	m.aiS().editTarget = &t
	title := fmt.Sprintf("line %d", t.start+1)
	if t.end-t.start > 1 {
		title = fmt.Sprintf("lines %d-%d", t.start+1, t.end)
	}
	m.prompt = prompt.New("✦ Edit with AI ("+title+")", "Instruction:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIEdit
	return nil
}

// onAIEditInstruction is the promptKindAIEdit submit.
func (m *Model) onAIEditInstruction(value string) tea.Cmd {
	s := m.aiS()
	t := s.editTarget
	s.editTarget = nil
	if t == nil || strings.TrimSpace(value) == "" {
		return nil
	}
	return m.runAIEdit(*t, "edit", "AI edit: "+strings.TrimSpace(value), value)
}

// aiDocComment proposes a doc comment above the declaration at the cursor.
func (m *Model) aiDocComment() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	t.start, t.end = t.cursor, minInt(t.cursor+1, len(t.full))
	return m.runAIEdit(t, "doc", "AI doc comment", aiDocInstruction)
}

// aiDocInstruction is Doc Comment's instruction (the range is the
// cursor's line).
const aiDocInstruction = "Write a doc comment for the declaration on the first line, following the language's conventions. " +
	"Return the doc comment followed by the original line(s) unchanged."

const aiEditSystemPrompt = "You are a precise code editor. You get a file and a line range from it. " +
	"Return ONLY the replacement text for that line range — complete lines, same indentation style — " +
	"in ONE fenced code block. No explanation."

// aiEditPrompt builds the user message for a range edit (file capped to a
// window around the range).
func aiEditPrompt(t aiRangeTarget, instruction string) string {
	lo, hi := maxInt(t.start-150, 0), minInt(t.end+150, len(t.full))
	var b strings.Builder
	fmt.Fprintf(&b, "File %s (language: %s), lines %d-%d of %d:\n```\n", filepath.Base(t.path), t.ft, lo+1, hi, len(t.full))
	b.WriteString(strings.Join(t.full[lo:hi], "\n"))
	fmt.Fprintf(&b, "\n```\n\nRange to replace (lines %d-%d):\n```\n%s\n```\n\nInstruction: %s",
		t.start+1, t.end, strings.Join(t.full[t.start:t.end], "\n"), instruction)
	return b.String()
}

// aiEditRequest is the one-shot request of a range edit.
func aiEditRequest(t aiRangeTarget, model, instruction string) ai.Request {
	return ai.Request{Model: model, Messages: []ai.Message{
		{Role: ai.RoleSystem, Content: aiEditSystemPrompt},
		{Role: ai.RoleUser, Content: aiEditPrompt(t, instruction)},
	}}
}

// aiEditAnswer is the replacement lines of an edit answer (its first
// code block, else all of it); false: no code.
func aiEditAnswer(text string) ([]string, bool) {
	code, ok := ai.FirstCodeBlock(text)
	if !ok {
		code = strings.TrimSpace(text)
	}
	if strings.TrimSpace(code) == "" {
		return nil, false
	}
	return strings.Split(code, "\n"), true
}

// runAIEdit sends a one-shot edit request.
func (m *Model) runAIEdit(t aiRangeTarget, kind, title, instruction string) tea.Cmd {
	s := m.aiS()
	if s.actionCancel != nil {
		s.actionCancel()
	}
	s.actionGen++
	gen := s.actionGen
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	s.actionCancel = cancel
	s.actionBusy = kind
	prov, req := s.provider, aiEditRequest(t, s.cfg.Model, instruction)
	return tea.Batch(m.aiInfo("✦ AI is working… (AI: Stop Generating to cancel)"), func() tea.Msg {
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, req))
		return aiActionResultMsg{gen: gen, kind: kind, title: title, target: t, text: text, err: last.Err}
	})
}

// onAIActionResult turns a model answer into a proposal.
func (m *Model) onAIActionResult(msg aiActionResultMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.actionGen {
		return nil
	}
	s.actionBusy = ""
	s.actionCancel = nil
	if msg.err != nil {
		if msg.err == context.Canceled {
			return nil
		}
		return m.aiToastErr("AI request failed", msg.err)
	}
	repl, ok := aiEditAnswer(msg.text)
	if !ok {
		return m.aiInfo("AI returned no code")
	}
	return m.openAIProposal(aiProposal{target: msg.target, repl: repl, title: msg.title})
}

// proposeWholeFile proposes code as the full content of the current file.
func (m *Model) proposeWholeFile(code, title string) tea.Cmd {
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open the target file in the editor first")
	}
	t.start, t.end = 0, len(t.full)
	return m.openAIProposal(aiProposal{target: t, repl: strings.Split(strings.TrimRight(code, "\n"), "\n"), title: title})
}

// openAIProposal shows the change in the review card (ai_review.go).
func (m *Model) openAIProposal(p aiProposal) tea.Cmd {
	t := p.target
	if equalLines(t.full[t.start:t.end], p.repl) {
		return m.aiInfo("AI suggests no changes")
	}
	return m.openBufferProposal(p)
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// aiProposalApplyLua replaces the range in ONE nvim_buf_set_lines call
// after checking the buffer still matches what the model saw.
// Args: buf, start, end, repl, orig range, original line count.
const aiProposalApplyLua = `
local buf, s, e, repl, orig, total = ...
if not vim.api.nvim_buf_is_valid(buf) or vim.api.nvim_buf_line_count(buf) ~= total then return 'stale' end
local cur = vim.api.nvim_buf_get_lines(buf, s, e, false)
if #cur ~= #orig then return 'stale' end
for i = 1, #cur do if cur[i] ~= orig[i] then return 'stale' end end
vim.api.nvim_buf_set_lines(buf, s, e, false, repl)
local win = vim.fn.bufwinid(buf)
if win ~= -1 then
  vim.api.nvim_set_current_win(win)
  pcall(vim.api.nvim_win_set_cursor, win, { s + 1, 0 })
end
return 'ok'
`
