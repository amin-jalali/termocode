package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/toast"
)

// CodeActionsMsg carries the LSP-fetched code actions awaiting picker display.
type CodeActionsMsg struct {
	Actions []nvim.CodeAction
}

// fetchCodeActionsCmd asks every LSP attached to the current buffer for the
// actions available at the cursor's line. Returns a CodeActionsMsg, even when
// the list is empty — the receiver decides whether to open the picker or
// surface a "no actions" toast.
func (m Model) fetchCodeActionsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		acts, err := c.FetchCodeActions()
		if err != nil {
			return ErrMsg{Err: err}
		}
		return CodeActionsMsg{Actions: acts}
	}
}

// applyCodeActionsMsg wires the freshly-fetched action list into the picker
// overlay. Empty list → friendly toast. The picker is built with grouped
// section headers (Quick Fix / Source / Refactor / Generate / Test / Git /
// Other) so the user can scan by category instead of one flat list.
//
// Phase 2 additions: extraCodeActionsForContext synthesises non-LSP items
// (Run Test, Git Blame, Generate Doc, …) and we splice them into the same
// list before bucketing. They're carried through nvim.CodeAction with a
// negative Index sentinel, so the apply path can branch on the sign.
func (m *Model) applyCodeActionsMsg(msg CodeActionsMsg) tea.Cmd {
	// Synthesise Phase-2 actions and append; bucketing then groups them
	// alongside whatever the LSP returned. We do this even when LSP returned
	// nothing — the user opening Ctrl+. on a non-LSP file should still see
	// Git and test affordances when they're applicable.
	extra := m.extraCodeActionsForContext()
	all := make([]nvim.CodeAction, 0, len(msg.Actions)+len(extra))
	all = append(all, msg.Actions...)
	all = append(all, extra...)

	if len(all) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No code actions available")
		// Clear any stale preferred-action hint pinned to the current
		// cursor — there's nothing to apply.
		m.preferredCodeAction = nil
		return toastCmd
	}
	m.codeActionsIndex = make(map[string]int, len(all))

	// Bucket each action into its display group.
	groups := map[string][]nvim.CodeAction{}
	for _, a := range all {
		g := codeActionGroup(a)
		groups[g] = append(groups[g], a)
	}

	// Stable group order: Quick Fix → Source → Refactor → Generate → Test
	// → Git → Other. Empty groups don't contribute a header. "Git" is the
	// Phase-2 synthesized bucket; it sits between Test and Other so the
	// LSP-driven groups stay at the top of the list.
	order := []string{"Quick Fix", "Source", "Refactor", "Generate", "Test", "Git", "Other"}
	items := make([]picker.Item, 0, len(all)+len(order))
	for _, g := range order {
		gActions := groups[g]
		if len(gActions) == 0 {
			continue
		}
		items = append(items, picker.Item{
			ID:     "hdr-" + g,
			Title:  g,
			Header: true,
			Group:  g,
		})
		for _, a := range gActions {
			// Picker IDs use the absolute value (the negative-index
			// sentinel format would clash with picker conventions); the
			// codeActionsIndex map preserves the sign so the apply path
			// can dispatch synth vs LSP correctly.
			id := fmt.Sprintf("ca-%d", a.Index)
			items = append(items, picker.Item{
				ID:    id,
				Title: formatCodeActionTitle(a),
				Hint:  formatCodeActionHint(a),
				Group: g,
			})
			m.codeActionsIndex[id] = a.Index
		}
	}

	// Track the preferred action separately so Ctrl+Shift+. and the
	// inline 💡 hint can apply / advertise it without round-tripping
	// through the picker. Only LSP actions are eligible — synth actions
	// (negative index) never set IsPreferred=true so the inline hint
	// belongs to the LSP-flagged Quick Fix.
	m.preferredCodeAction = nil
	for i := range msg.Actions {
		if msg.Actions[i].IsPreferred {
			a := msg.Actions[i] // copy (not ranged ptr)
			m.preferredCodeAction = &a
			m.preferredCodeActionLine = m.cursorLine
			break
		}
	}

	m.picker = picker.NewItems(" Code Actions ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindCodeAction
	return nil
}

// codeActionGroup buckets an LSP code action into one of the picker's
// display groups based on its `kind` (an LSP-defined dotted string) and a
// few title heuristics. The returned label matches the section-header
// strings the picker renders.
//
// LSP standard kinds we recognize:
//
//	quickfix*                     → "Quick Fix"
//	source.organizeImports*       → "Source"
//	source.fixAll*                → "Source"
//	source*                       → "Source"
//	refactor.extract*             → "Refactor"
//	refactor.inline*              → "Refactor"
//	refactor.rewrite*             → "Refactor"
//	refactor*                     → "Refactor"
//
// Title heuristics (applied AFTER kind buckets so kind always wins):
//
//	"test" in kind / title        → "Test"
//	"generate" in title           → "Generate"
//
// Anything else falls into "Other".
func codeActionGroup(a nvim.CodeAction) string {
	kind := strings.ToLower(a.Kind)
	titleLower := strings.ToLower(a.Title)
	// Phase-2 synthesized kinds bucketed first so the LSP-prefix rules below
	// can't accidentally bucket "test.run" into "Test" via a substring match
	// on "test" only.
	switch kind {
	case synthKindGitBlame, synthKindGitDiff, synthKindGitCopy:
		return "Git"
	case synthKindTestRun, synthKindTestCommand:
		return "Test"
	case synthKindGenerateDoc, synthKindGenerateTbl:
		return "Generate"
	}
	switch {
	case strings.HasPrefix(kind, "quickfix"):
		return "Quick Fix"
	case strings.HasPrefix(kind, "source"):
		return "Source"
	case strings.HasPrefix(kind, "refactor"):
		return "Refactor"
	}
	if strings.Contains(titleLower, "test") || strings.Contains(kind, "test") {
		return "Test"
	}
	if strings.Contains(titleLower, "generate") {
		return "Generate"
	}
	return "Other"
}

// applySelectedCodeAction runs the action picked from the overlay. Index is
// looked up from the picker ID via m.codeActionsIndex (populated when the
// list was first shown), then handed off to nvim which resolves and applies
// the LSP edit/command. NEGATIVE indices are Phase-2 synthesized actions —
// those are dispatched to applySynthCodeAction (Go-side handlers, no nvim
// LSP round-trip).
func (m *Model) applySelectedCodeAction(id string) tea.Cmd {
	idx, ok := m.codeActionsIndex[id]
	if !ok {
		return nil
	}
	// Phase-2 synth dispatch: handlers run Go code (exec.Cmd, blame, doc
	// insert) without going through nvim's LSP applier.
	if idx < 0 {
		// Clear the preferred-action hint — same contract as the LSP
		// branch below: whichever action the user just picked, the
		// previous hint is now stale.
		m.preferredCodeAction = nil
		return m.applySynthCodeAction(idx)
	}
	if m.nvim == nil {
		return nil
	}
	if err := m.nvim.ApplyCodeAction(idx); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Code action failed", err.Error())
		return toastCmd
	}
	// Clear the preferred-action hint — whatever the user just applied,
	// the diagnostic that drove the hint is most likely gone.
	m.preferredCodeAction = nil
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Applied code action")
	return toastCmd
}

// applyPreferredCodeActionFromCache fires the LSP-flagged preferred action
// recorded by the most recent applyCodeActionsMsg. When no cached preferred
// action exists, the caller should fall back to opening the picker (which
// is what the dispatcher in update.go does). Mirrors applySelectedCodeAction
// for the apply / toast / hint-clear contract.
func (m *Model) applyPreferredCodeActionFromCache() tea.Cmd {
	if m.preferredCodeAction == nil || m.nvim == nil {
		return nil
	}
	idx := m.preferredCodeAction.Index
	title := m.preferredCodeAction.Title
	if err := m.nvim.ApplyCodeAction(idx); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Code action failed", err.Error())
		return toastCmd
	}
	m.preferredCodeAction = nil
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Applied: "+strings.SplitN(title, "\n", 2)[0])
	return toastCmd
}

// formatCodeActionTitle is the headline shown in the picker. We prefix with
// a small kind glyph so users can distinguish refactor/quickfix at a glance.
func formatCodeActionTitle(a nvim.CodeAction) string {
	icon := codeActionIcon(a.Kind)
	title := strings.SplitN(a.Title, "\n", 2)[0]
	if icon != "" {
		return icon + "  " + title
	}
	return title
}

// formatCodeActionHint shows the LSP client and the action kind on the right.
func formatCodeActionHint(a nvim.CodeAction) string {
	parts := make([]string, 0, 2)
	if a.Kind != "" {
		parts = append(parts, a.Kind)
	}
	if a.Client != "" {
		parts = append(parts, a.Client)
	}
	return strings.Join(parts, "  ")
}

// codeActionIcon picks a small glyph that visually distinguishes the LSP
// action kinds. Quick fixes get a wrench, refactors get a tool, source-level
// (organize imports etc) gets a sparkle. Unknown kinds get a dot.
func codeActionIcon(kind string) string {
	switch kind {
	case synthKindTestRun, synthKindTestCommand:
		return "▶"
	case synthKindGitBlame, synthKindGitDiff, synthKindGitCopy:
		return "⎇"
	case synthKindGenerateDoc, synthKindGenerateTbl:
		return "✚"
	}
	switch {
	case kind == "":
		return "·"
	case strings.HasPrefix(kind, "quickfix"):
		return "🔧"
	case strings.HasPrefix(kind, "refactor"):
		return "✎"
	case strings.HasPrefix(kind, "source"):
		return "✦"
	}
	return "·"
}
