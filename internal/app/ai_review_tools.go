package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/textdiff"
)

// AI change review (Group R2). With ai_edit_review = review (the default)
// and fileWrite not auto-approved, the agent's file-changing tools do not
// write: they propose the new text, the review card (ai_review.go) shows
// it as a diff, and the tool call waits until the user accepted or
// rejected every change (or Stop, or aiReviewTimeout). The model is told
// exactly what was written. Accepted text goes through aiWriteFile, so an
// open tab shows it as one undo step and the IDE follows as before.
//
// Reviewed tools: write_file, create_file, format, lsp_rename and
// lsp_code_actions op=apply (an action that runs a server command can not
// be previewed: it falls back to the confirm dialog).

// aiReviewTimeout bounds how long a tool call waits for the user.
var aiReviewTimeout = 30 * time.Minute

// aiReviewEdits reports whether this turn's file changes are reviewed.
func (m *Model) aiReviewEdits() bool {
	if m.ai == nil {
		return true // the docs catalog: the default
	}
	return m.ai.settings.ReviewEdits() && !contains(m.ai.settings.AutoApprove, ai.ClassFileWrite)
}

// aiFileChange is one proposed file text.
type aiFileChange struct {
	abs, rel string
	text     string
	create   bool // create_file: the file must not exist
}

// aiProposed is what proposing returned: the channels to wait on (one per
// file under review) and notes for files that needed no review.
type aiProposed struct {
	chans []chan string
	notes []string
	err   error
}

// aiReviewTools turns the file-changing tools into reviewed ones.
func (m *Model) aiReviewTools(tools []ai.Tool, root string, call aiIDECaller) []ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	client := m.nvim
	lua := func(code string, args ...any) (string, error) {
		if client == nil {
			return "", errors.New("the editor is not running")
		}
		var out string
		err := client.ExecLuaResult(code, &out, args...)
		return out, err
	}
	resolve := func(a map[string]any) (string, error) {
		p := ai.ArgString(a, "path")
		if p == "" {
			return "", errors.New("path is required")
		}
		return ws.Resolve(p)
	}
	// previewed runs a preview Lua and reviews the files it changes.
	previewed := func(ctx context.Context, abs, what, none string, code string, args ...any) (string, error) {
		out, err := lua(code, args...)
		if err != nil {
			return "", err
		}
		if out, err = aiLSPResult(abs, what, out); err != nil {
			return "", err
		}
		changes, err := aiLSPPreviewChanges(out, rel)
		if err != nil {
			return "", err
		}
		if len(changes) == 0 {
			return none, nil
		}
		return aiReviewChanges(ctx, call, changes)
	}
	review := " The user reviews the change as a diff and accepts or rejects each part; the result says what was written."
	for i, t := range tools {
		switch t.Name {
		case "write_file", "create_file":
			create := t.Name == "create_file"
			t.Class = ai.ClassReview
			t.Description += review
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := ws.Resolve(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				return aiReviewChanges(ctx, call, []aiFileChange{{abs: abs, rel: rel(abs), text: ai.ArgString(a, "content"), create: create}})
			}
		case "format":
			t.Class = ai.ClassReview
			t.Description = "Format a file with its language server. The user reviews the change as a diff; accepted parts are saved."
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				return previewed(ctx, abs, "formatting", rel(abs)+" is already formatted; nothing changed", aiFormatPreviewLua, abs)
			}
		case "lsp_rename":
			t.Class = ai.ClassReview
			t.Description = "Rename the symbol at a file position in the whole workspace via the language server." + review
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				return previewed(ctx, abs, "rename", "the rename changes nothing", aiLSPRenameLua, abs, root,
					ai.ArgInt(a, "line")-1, ai.ArgInt(a, "character")-1, ai.ArgString(a, "new_name"), "preview")
			}
		case "lsp_code_actions":
			innerGate, innerRun, tool := t.Gate, t.Run, t
			t.Gate = func(a map[string]any) (bool, string) {
				if mut, class := innerGate(a); !mut {
					return mut, class
				}
				return true, ai.ClassReview
			}
			t.Description += " op=apply shows the change as a diff for the user to review."
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				if ai.ArgString(a, "op") != "apply" {
					return innerRun(ctx, a)
				}
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				out, err := lua(aiLSPCodeActionsLua, abs, root, ai.ArgInt(a, "line")-1, ai.ArgInt(a, "character")-1,
					"preview", ai.ArgInt(a, "index"), ai.ArgString(a, "title"))
				if err != nil {
					return "", err
				}
				if out, err = aiLSPResult(abs, "code actions", out); err != nil {
					return "", err
				}
				if strings.HasPrefix(out, "CMD\t") {
					if !aiAskConfirm(ctx, call, tool, tool.Human(a)) {
						return "lsp_code_actions: the user declined this action. Do not retry it; continue without it or ask what to do instead.", nil
					}
					return innerRun(ctx, a)
				}
				changes, err := aiLSPPreviewChanges(out, rel)
				if err != nil {
					return "", err
				}
				if len(changes) == 0 {
					return "the code action changes nothing", nil
				}
				return aiReviewChanges(ctx, call, changes)
			}
		default:
			continue
		}
		tools[i] = t
	}
	return tools
}

// aiBeforeCalls proposes every write_file / create_file of a step at
// once, so the review list shows all its files; each call then waits for
// its own file (aiReviewChanges finds the proposal by call id).
func aiBeforeCalls(root string, call aiIDECaller) func(context.Context, []ai.ToolCall) {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	return func(ctx context.Context, calls []ai.ToolCall) {
		if len(calls) < 2 {
			return
		}
		seen := map[string]bool{}
		for _, c := range calls {
			if c.Name != "write_file" && c.Name != "create_file" {
				continue
			}
			abs, err := ws.Resolve(ai.ArgString(c.Args, "path"))
			if err != nil || seen[abs] {
				continue // the call reports the error; a second write waits for the first
			}
			seen[abs] = true
			ch := aiFileChange{abs: abs, rel: rel(abs), text: ai.ArgString(c.Args, "content"), create: c.Name == "create_file"}
			id := c.ID
			_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
				p := m.aiProposeChanges([]aiFileChange{ch})
				s := m.aiS()
				if s.preReview == nil {
					s.preReview = map[string]aiProposed{}
				}
				s.preReview[id] = p
				return "", nil, nil
			})
		}
	}
}

// aiReviewChanges proposes changes and waits for the user's decision on
// each; it returns the text for the model.
func aiReviewChanges(ctx context.Context, call aiIDECaller, changes []aiFileChange) (string, error) {
	if l := agentrun.ReviewLocker(ctx); l != nil { // pipeline steps review one at a time
		l.Lock()
		defer l.Unlock()
	}
	id := ai.CallID(ctx)
	var p aiProposed
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		s := m.aiS()
		if pre, ok := s.preReview[id]; ok && id != "" && len(changes) == 1 {
			delete(s.preReview, id)
			p = pre
		} else {
			p = m.aiProposeChanges(changes)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	if p.err != nil {
		return "", p.err
	}
	out := append([]string(nil), p.notes...)
	timer := time.NewTimer(aiReviewTimeout)
	defer timer.Stop()
	for _, ch := range p.chans {
		select {
		case r := <-ch:
			out = append(out, r)
		case <-ctx.Done():
			return "", errAIChatStopped
		case <-timer.C:
			_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
				m.cancelAIReviewTurn()
				return "", nil, nil
			})
			return "", fmt.Errorf("the user did not review the change within %s; nothing more was written. Ask them before you try again", aiReviewTimeout)
		}
	}
	return strings.Join(out, "\n\n"), nil
}

// aiProposeChanges checks the changes and adds them to the review card
// (on Update). A file open with unsaved edits is refused, as write_file
// always did.
func (m *Model) aiProposeChanges(changes []aiFileChange) aiProposed {
	var p aiProposed
	gen := m.aiS().chatGen
	type prop struct {
		c       aiFileChange
		base    string
		existed bool
		tag     string
	}
	var props []prop
	for _, c := range changes {
		fi, statErr := os.Stat(c.abs)
		if statErr == nil && fi.IsDir() {
			p.err = fmt.Errorf("%s is a folder", c.rel)
			return p
		}
		if c.create && statErr == nil {
			p.err = fmt.Errorf("%s already exists (use write_file to change it)", c.rel)
			return p
		}
		bufs, err := m.aiBuffersUnder(c.abs)
		if err != nil {
			p.err = err
			return p
		}
		for _, b := range bufs {
			if b.Path == c.abs && b.Loaded && b.Modified {
				p.err = fmt.Errorf("not changed: %s is open in the editor with unsaved changes. "+
					"Ask the user to save or discard them first; do not overwrite their work", c.rel)
				return p
			}
		}
		base := ""
		if statErr == nil {
			b, err := os.ReadFile(c.abs)
			if err != nil {
				p.err = err
				return p
			}
			base = string(b)
		}
		if statErr == nil && base == c.text {
			p.notes = append(p.notes, c.rel+" already has this content; nothing changed")
			continue
		}
		tag := ""
		if statErr != nil {
			tag = "new file"
		}
		props = append(props, prop{c, base, statErr == nil, tag})
	}
	for _, pr := range props {
		ch := make(chan string, 1)
		p.chans = append(p.chans, ch)
		m.addAIReview(m.diskReviewItem(pr.c, pr.base, pr.existed, pr.tag, gen, ch))
	}
	return p
}

// diskReviewItem is a file change written by aiWriteFile once accepted.
func (m *Model) diskReviewItem(c aiFileChange, base string, existed bool, tag string, gen uint64, ch chan string) *aiReviewItem {
	it := &aiReviewItem{file: diffview.File{Name: c.rel, Old: base, New: c.text, Tag: tag}, gen: gen}
	it.finish = func(m *Model, d *textdiff.Diff, acc map[int]bool, edit bool) (string, tea.Cmd) {
		sum := aiReviewSummary(d, acc)
		if len(acc) == 0 {
			ch <- aiReviewRejectedText(c.rel, existed)
			return sum, nil
		}
		// The file may have changed while the user looked at the diff.
		cur, err := os.ReadFile(c.abs)
		if (err == nil) != existed || (err == nil && string(cur) != base) {
			ch <- "not written: " + c.rel + " changed on disk while the user reviewed the change. Read it again before you propose a new change."
			return "Not applied: the file changed", m.aiInfo(c.rel + " changed while you reviewed it — not written")
		}
		final := d.Apply(acc)
		out, cmd, err := m.aiWriteFile(c.abs, c.rel, final)
		if err != nil {
			ch <- "error: " + err.Error()
			return "Not applied: " + err.Error(), cmd
		}
		text := aiReviewAcceptedText(c.rel, d, acc, final) + "\n" + out
		if edit {
			line := 1
			for i, h := range d.Hunks {
				if acc[i] {
					line = h.OldStart + 1
					break
				}
			}
			m.openFileAtLine(c.abs, line)
			m.focus = FocusEditor
			text += "\nThe user opened " + c.rel + " to edit it by hand; read it again before you change it."
		}
		ch <- text
		return sum, cmd
	}
	return it
}

func aiReviewRejectedText(rel string, existed bool) string {
	what := "the change to " + rel
	if !existed {
		what = "creating " + rel
	}
	return "The user reviewed and rejected " + what + "; nothing was written. " +
		"Do not make the same change again; continue without it or ask the user what they want instead."
}

// aiReviewAcceptedText says what was written; for a partial accept it
// lists the rejected changes so the model knows what is not in the file.
func aiReviewAcceptedText(rel string, d *textdiff.Diff, acc map[int]bool, final string) string {
	n := max(len(d.Hunks), 1)
	k := len(acc)
	if k >= n {
		if n == 1 {
			return "The user accepted the change to " + rel + "."
		}
		return fmt.Sprintf("The user accepted all %d changes to %s.", n, rel)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The user accepted %d of %d changes to %s; only those were written. Rejected (NOT in the file):", k, n, rel)
	const maxLines, maxBytes = 4, 3000
	for i, h := range d.Hunks {
		if acc[i] || b.Len() > maxBytes {
			continue
		}
		fmt.Fprintf(&b, "\n- change %d (your lines %d-%d):", i+1, h.NewStart+1, h.NewStart+max(h.NewCount, 1))
		var rm, add []string
		for _, r := range d.Rows[h.First:h.End] {
			if r.Kind == textdiff.Removed {
				rm = append(rm, r.Text)
			} else {
				add = append(add, r.Text)
			}
		}
		for j, l := range rm {
			if j == maxLines {
				fmt.Fprintf(&b, "\n    … %d more removed", len(rm)-j)
				break
			}
			b.WriteString("\n    -" + l)
		}
		for j, l := range add {
			if j == maxLines {
				fmt.Fprintf(&b, "\n    … %d more added", len(add)-j)
				break
			}
			b.WriteString("\n    +" + l)
		}
	}
	fmt.Fprintf(&b, "\n%s now has %d lines. Read it again before you change it.", rel, len(textdiff.SplitLines(final)))
	return b.String()
}

// ── LSP previews ──────────────────────────────────────────────────────────

// aiLSPPreviewLua (Lua prelude): preview(edit, encoding) returns
// "PREVIEW\t<json>" with the text edits per file of a WorkspaceEdit,
// without applying them. File operations (create / rename / delete) can
// not be previewed.
const aiLSPPreviewLua = `
local function preview(edit, enc)
  local files, order = {}, {}
  local function add(uri, edits)
    local f = vim.uri_to_fname(uri)
    if not files[f] then files[f] = {}; order[#order + 1] = f end
    for _, e in ipairs(edits or {}) do table.insert(files[f], e) end
  end
  for uri, edits in pairs((edit or {}).changes or {}) do add(uri, edits) end
  for _, dc in ipairs((edit or {}).documentChanges or {}) do
    if dc.kind then return 'ERR\tthe change also creates, renames or deletes files, so it can not be shown as a diff' end
    add(dc.textDocument.uri, dc.edits)
  end
  local out = {}
  for _, f in ipairs(order) do
    local b = vim.fn.bufnr(f)
    local modified = b ~= -1 and vim.api.nvim_buf_is_loaded(b) and vim.bo[b].modified or false
    out[#out + 1] = { path = f, modified = modified, edits = files[f] }
  end
  return 'PREVIEW\t' .. vim.json.encode({ encoding = enc or 'utf-16', files = out })
end
`

// aiFormatPreviewLua asks the language server for formatting edits.
const aiFormatPreviewLua = aiLSPPrelude + aiLSPPreviewLua + `
local path = ...
local b, clients = lspbuf(path)
if #clients == 0 then return 'NOLSP\t' .. vim.bo[b].filetype end
local cs, names = capable(clients, 'documentFormattingProvider')
if #cs == 0 then return 'NOCAP\t' .. names end
local c = cs[1]
local params = vim.lsp.util.make_formatting_params({})
params.textDocument = { uri = vim.uri_from_fname(path) }
local r = req(c, 'textDocument/formatting', params, b)
if not r then return 'ERR\tthe language server (' .. c.name .. ') did not answer' end
if r.err then return 'ERR\tformat failed: ' .. (r.err.message or vim.inspect(r.err)) end
return preview({ changes = { [vim.uri_from_fname(path)] = r.result or {} } }, c.offset_encoding)
`

type lspPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspTextEdit struct {
	Range struct {
		Start lspPos `json:"start"`
		End   lspPos `json:"end"`
	} `json:"range"`
	NewText string `json:"newText"`
}

type lspPreview struct {
	Encoding string `json:"encoding"`
	Files    []struct {
		Path     string        `json:"path"`
		Modified bool          `json:"modified"`
		Edits    []lspTextEdit `json:"edits"`
	} `json:"files"`
}

// aiLSPPreviewChanges turns a PREVIEW result into file changes (edits
// applied to the text on disk). A file with unsaved edits is refused.
func aiLSPPreviewChanges(out string, rel func(string) string) ([]aiFileChange, error) {
	js, ok := strings.CutPrefix(out, "PREVIEW\t")
	if !ok {
		return nil, fmt.Errorf("unexpected language server answer: %.200s", out)
	}
	var p lspPreview
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		return nil, err
	}
	var changes []aiFileChange
	for _, f := range p.Files {
		if f.Modified {
			return nil, fmt.Errorf("not changed: %s is open in the editor with unsaved changes. Ask the user to save it first", rel(f.Path))
		}
		b, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		text, err := applyTextEdits(string(b), f.Edits, p.Encoding)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel(f.Path), err)
		}
		if text != string(b) {
			changes = append(changes, aiFileChange{abs: f.Path, rel: rel(f.Path), text: text})
		}
	}
	return changes, nil
}

// applyTextEdits applies LSP text edits to text. Positions count in the
// given encoding ("utf-16" by default, "utf-8", "utf-32").
func applyTextEdits(text string, edits []lspTextEdit, enc string) (string, error) {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	off := func(p lspPos) (int, error) {
		if p.Line >= len(starts) {
			return len(text), nil
		}
		if p.Line < 0 || p.Character < 0 {
			return 0, errors.New("bad edit position")
		}
		i := starts[p.Line]
		end := len(text)
		if p.Line+1 < len(starts) {
			end = starts[p.Line+1] - 1
		}
		units := 0
		for i < end && units < p.Character {
			r, size := utf8.DecodeRuneInString(text[i:])
			switch enc {
			case "utf-8":
				units += size
			case "utf-32":
				units++
			default:
				units += utf16.RuneLen(r)
			}
			i += size
		}
		return i, nil
	}
	type span struct {
		s, e, idx int
		t         string
	}
	spans := make([]span, 0, len(edits))
	for i, e := range edits {
		s, err := off(e.Range.Start)
		if err != nil {
			return "", err
		}
		en, err := off(e.Range.End)
		if err != nil {
			return "", err
		}
		if en < s {
			return "", errors.New("bad edit range")
		}
		spans = append(spans, span{s, en, i, e.NewText})
	}
	sort.Slice(spans, func(a, b int) bool {
		if spans[a].s != spans[b].s {
			return spans[a].s > spans[b].s
		}
		return spans[a].idx > spans[b].idx
	})
	for _, sp := range spans {
		text = text[:sp.s] + sp.t + text[sp.e:]
	}
	return text, nil
}

// aiAskConfirm opens the "AI wants to" dialog from inside a tool run (a
// code action that can not be previewed) and waits for the answer.
func aiAskConfirm(ctx context.Context, call aiIDECaller, t ai.Tool, human string) bool {
	reply := make(chan bool, 1)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		s := m.aiS()
		t.Class = "" // no "Always allow" here
		s.confirm = &aiPendingConfirm{tool: t, human: human, reply: reply}
		m.confirm = confirm.New("✦ AI wants to: "+t.Name, human+"\n(This change runs a language-server command, so it can not be shown as a diff first.)",
			[]confirm.Button{{ID: "allow", Title: "Allow", Style: confirm.StylePrimary}, {ID: "deny", Title: "Deny", Style: confirm.StyleDestructive}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAITool
		return "", nil, nil
	}); err != nil {
		return false
	}
	select {
	case ok := <-reply:
		return ok
	case <-ctx.Done():
		return false
	}
}

// toggleAIEditReview flips ai_edit_review between review and auto.
func (m *Model) toggleAIEditReview() tea.Cmd {
	s := m.aiS()
	next := ai.EditReviewAuto
	if !s.settings.ReviewEdits() {
		next = ai.EditReviewOn
	}
	if path, err := ai.ConfigPath(); err == nil {
		cur := ai.LoadSettings(path)
		cur.EditReview = next
		if err := ai.SaveSettings(path, cur); err != nil {
			return m.aiToastErr("Could not save the setting", err)
		}
	}
	s.settings.EditReview = next
	if next == ai.EditReviewAuto {
		return m.aiInfo("AI file changes: written after the confirm dialog (no diff review)")
	}
	msg := "AI file changes: shown as a diff to accept or reject"
	if contains(s.settings.AutoApprove, ai.ClassFileWrite) {
		msg += " (but fileWrite is auto-approved: turn it off in AI: Agent Auto-Approve…)"
	}
	return m.aiInfo(msg)
}
