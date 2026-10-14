package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
)

// What the AI agent may do with each palette command (Group K1). The
// execute_action tool runs a command by id and list_actions finds ids;
// both read this map. TestAIActionPolicyCoversEveryPaletteID fails when a
// palette id has no entry, so every new command is classified on purpose.
//
//   - auto: safe, UI only or read only — runs without asking.
//   - confirm: asks the user first. Class is the "Always allow" group
//     (ide, fileWrite, git, run); class "" is destructive and always asks,
//     with no "Always allow" button.
//   - never: the AI can not run it; Reason is told to the model.

type aiGate string

const (
	aiGateAuto    aiGate = "auto"
	aiGateConfirm aiGate = "confirm"
	aiGateNever   aiGate = "never"
)

type aiActionPolicy struct {
	Gate   aiGate
	Class  string // confirm only; "" = destructive, no "Always allow"
	Reason string // never (required); a short note for the others
}

func aiAuto() aiActionPolicy                { return aiActionPolicy{Gate: aiGateAuto} }
func aiConfirm(class string) aiActionPolicy { return aiActionPolicy{Gate: aiGateConfirm, Class: class} }
func aiDestructive() aiActionPolicy         { return aiActionPolicy{Gate: aiGateConfirm} }
func aiNever(reason string) aiActionPolicy  { return aiActionPolicy{Gate: aiGateNever, Reason: reason} }

// Reasons shared by many never ids.
const (
	aiNoPicker   = "opens a picker or prompt that only the user can answer; use a typed tool"
	aiNoSession  = "ends or resets the user's session"
	aiNoOutside  = "acts outside the IDE (OS shell, file manager or browser)"
	aiNoAIConfig = "AI provider, credentials and approvals stay with the user"
	aiNoNested   = "starts or changes an AI chat or edit; answer in this chat instead"
	aiNoTransfer = "moves files between the user's machine and the server; user only"
)

var aiActionPolicies = map[string]aiActionPolicy{
	// ── Files and tabs ──
	"save":              aiConfirm(ai.ClassFileWrite),
	"save-all":          aiConfirm(ai.ClassFileWrite),
	"close-buffer":      aiConfirm(ai.ClassIDE),
	"reopen-closed":     aiAuto(),
	"next-buffer":       aiAuto(),
	"prev-buffer":       aiAuto(),
	"alternate-file":    aiAuto(),
	"pin-tab":           aiAuto(),
	"reload-buffer":     aiDestructive(), // drops unsaved edits
	"quick-open":        aiNever(aiNoPicker + " (open_file)"),
	"open-recent":       aiNever(aiNoPicker + " (open_file)"),
	"open-workspace":    aiNever(aiNoPicker),
	"diff-files":        aiNever(aiNoPicker),
	"toggle-auto-save":  aiConfirm(ai.ClassIDE),
	"copy-file-ref":     aiConfirm(ai.ClassIDE), // overwrites the system clipboard
	"clipboard-history": aiNever(aiNoPicker),
	"quit":              aiNever("quits termocode"),

	// ── Layout and views ──
	"toggle-explorer":      aiAuto(),
	"toggle-actions-panel": aiNever("hides the AI chat the user is talking in"),
	"focus-explorer":       aiAuto(),
	"focus-editor":         aiAuto(),
	"reveal-file":          aiAuto(),
	"toggle-hidden":        aiAuto(),
	"zen-mode":             aiAuto(),
	"toggle-wrap":          aiAuto(),
	"toggle-inlay-hints":   aiAuto(),
	"split-vertical":       aiAuto(),
	"split-horizontal":     aiAuto(),
	"close-split":          aiAuto(),
	"fold-all":             aiAuto(),
	"unfold-all":           aiAuto(),
	"toggle-fold":          aiAuto(),
	"preview-md":           aiAuto(),
	"view-output":          aiAuto(),
	"output-clear":         aiConfirm(ai.ClassIDE),
	"problems-panel":       aiAuto(),
	"cheat-sheet":          aiAuto(),
	"buffer-info":          aiAuto(),
	"error-log":            aiAuto(),
	"run-doctor":           aiAuto(),
	"open-docs":            aiAuto(),

	// ── Terminal ──
	"toggle-terminal":    aiAuto(),
	"terminal-new-tab":   aiAuto(),
	"terminal-close-tab": aiDestructive(), // kills what runs in it
	"terminal-next-tab":  aiAuto(),
	"terminal-prev-tab":  aiAuto(),
	"terminal-maximize":  aiAuto(),
	"terminal-minimize":  aiAuto(),
	"terminal-open-link": aiNever(aiNoPicker + " (open_file)"),
	"open-shell":         aiNever(aiNoOutside),

	// ── Text edits at the cursor / in the active file ──
	"duplicate-line":  aiConfirm(ai.ClassFileWrite),
	"toggle-comment":  aiConfirm(ai.ClassFileWrite),
	"format-document": aiConfirm(ai.ClassFileWrite),
	"trim-whitespace": aiConfirm(ai.ClassFileWrite),
	"sort-asc":        aiConfirm(ai.ClassFileWrite),
	"sort-desc":       aiConfirm(ai.ClassFileWrite),
	"reverse-lines":   aiConfirm(ai.ClassFileWrite),
	"uppercase":       aiConfirm(ai.ClassFileWrite),
	"lowercase":       aiConfirm(ai.ClassFileWrite),
	"join-lines":      aiConfirm(ai.ClassFileWrite),
	"tabs-to-spaces":  aiConfirm(ai.ClassFileWrite),
	"spaces-to-tabs":  aiConfirm(ai.ClassFileWrite),
	"trim-blanks":     aiConfirm(ai.ClassFileWrite),
	"replace-in-file": aiNever(aiNoPicker + " (write_file)"),
	"snippet-picker":  aiNever(aiNoPicker),
	"snippets-manage": aiNever(aiNoPicker),

	// ── Search and navigation ──
	"find-files":         aiNever(aiNoPicker + " (search)"),
	"find-files-overlay": aiNever(aiNoPicker + " (search)"),
	"replace-workspace":  aiNever(aiNoPicker + " (search, write_file)"),
	"search-next-result": aiAuto(),
	"search-prev-result": aiAuto(),
	"goto-line":          aiNever(aiNoPicker + " (open_file with line)"),
	"problems":           aiNever(aiNoPicker + " (diagnostics)"),
	"next-diagnostic":    aiAuto(),
	"prev-diagnostic":    aiAuto(),
	"bookmark-toggle":    aiAuto(),
	"bookmark-list":      aiNever(aiNoPicker),
	"bookmark-clear":     aiConfirm(ai.ClassIDE),

	// ── LSP ──
	"hover":                 aiAuto(),
	"find-references":       aiAuto(),
	"goto-type-def":         aiAuto(),
	"goto-impl":             aiAuto(),
	"code-actions":          aiNever(aiNoPicker),
	"goto-symbol-file":      aiNever(aiNoPicker + " (lsp_nav)"),
	"goto-symbol-workspace": aiNever(aiNoPicker + " (lsp_nav)"),
	"lsp-manage":            aiNever(aiNoPicker + "; it installs software"),

	// ── Git ──
	"git-stage":           aiConfirm(ai.ClassGit),
	"git-stage-all":       aiConfirm(ai.ClassGit),
	"git-stage-hunk":      aiConfirm(ai.ClassGit),
	"git-unstage-hunk":    aiConfirm(ai.ClassGit),
	"git-discard-hunk":    aiDestructive(),
	"git-discard":         aiDestructive(),
	"git-commit":          aiNever(aiNoPicker + " (git_commit)"),
	"git-diff":            aiAuto(),
	"git-refresh":         aiAuto(),
	"git-view-toggle":     aiAuto(),
	"git-focus":           aiAuto(),
	"git-blame":           aiAuto(),
	"next-hunk":           aiAuto(),
	"prev-hunk":           aiAuto(),
	"git-push":            aiDestructive(), // changes the remote
	"git-pull":            aiConfirm(ai.ClassGit),
	"git-fetch":           aiConfirm(ai.ClassGit),
	"git-sync":            aiDestructive(), // pushes too
	"git-branch":          aiNever(aiNoPicker + " (git_branch, git_checkout)"),
	"git-stash":           aiConfirm(ai.ClassGit),
	"git-stash-pop":       aiConfirm(ai.ClassGit),
	"git-stash-list":      aiNever(aiNoPicker),
	"git-log":             aiNever(aiNoPicker),
	"git-compare-rev":     aiNever(aiNoPicker),
	"git-history":         aiNever(aiNoPicker),
	"git-tags":            aiNever(aiNoPicker),
	"git-conflict-next":   aiAuto(),
	"git-conflict-prev":   aiAuto(),
	"git-conflict-ours":   aiConfirm(ai.ClassFileWrite),
	"git-conflict-theirs": aiConfirm(ai.ClassFileWrite),
	"git-conflict-both":   aiConfirm(ai.ClassFileWrite),
	"git-clone":           aiNever("clones into a folder outside the workspace and opens it"),

	// ── Workspace, session, settings ──
	"workspace-add-folder":    aiNever(aiNoPicker),
	"workspace-remove-folder": aiNever(aiNoPicker),
	"reveal-fs":               aiNever(aiNoOutside),
	"transfer-download":       aiNever(aiNoTransfer),
	"transfer-upload":         aiNever(aiNoTransfer),
	"transfer-cancel":         aiNever(aiNoTransfer),
	"open-url":                aiNever(aiNoOutside),
	"reload-window":           aiNever(aiNoSession),
	"detach-session":          aiNever(aiNoSession),
	"reset-session":           aiNever(aiNoSession),
	"pick-theme":              aiNever(aiNoPicker),
	"open-settings-ui":        aiNever(aiNoPicker),
	"edit-custom-theme":       aiNever(aiNoPicker),
	"customize-keybindings":   aiNever(aiNoPicker),
	"settings-export":         aiNever("settings import / export stays with the user"),
	"settings-import":         aiNever("settings import / export stays with the user"),
	"config-commands":         aiConfirm(ai.ClassIDE),
	"config-theme":            aiConfirm(ai.ClassIDE),

	// ── Debug ──
	"debug-start":             aiConfirm(ai.ClassRun),
	"debug-run-nodebug":       aiConfirm(ai.ClassRun),
	"debug-restart":           aiConfirm(ai.ClassRun),
	"debug-continue":          aiConfirm(ai.ClassRun),
	"debug-step-over":         aiConfirm(ai.ClassRun),
	"debug-step-into":         aiConfirm(ai.ClassRun),
	"debug-step-out":          aiConfirm(ai.ClassRun),
	"debug-pause":             aiConfirm(ai.ClassIDE),
	"debug-stop":              aiConfirm(ai.ClassIDE),
	"debug-toggle-breakpoint": aiAuto(),
	"debug-clear-bps":         aiConfirm(ai.ClassIDE),
	"debug-show-stack":        aiAuto(),
	"debug-show-vars":         aiAuto(),
	"debug-console":           aiAuto(),
	"debug-view":              aiAuto(),
	"debug-open-launch":       aiConfirm(ai.ClassIDE), // may create launch.json
	"debug-select-config":     aiNever(aiNoPicker),
	"debug-conditional-bp":    aiNever(aiNoPicker),
	"debug-logpoint":          aiNever(aiNoPicker),
	"debug-add-watch":         aiNever(aiNoPicker),
	"dap-install":             aiNever(aiNoPicker + "; it installs software"),

	// ── Testing ──
	"run-tests":            aiConfirm(ai.ClassRun),
	"testing-focus":        aiAuto(),
	"testing-run-all":      aiConfirm(ai.ClassRun),
	"testing-rerun-failed": aiConfirm(ai.ClassRun),
	"testing-run-file":     aiConfirm(ai.ClassRun),
	"testing-run-cursor":   aiConfirm(ai.ClassRun),
	"testing-debug-cursor": aiConfirm(ai.ClassRun),
	"testing-stop":         aiConfirm(ai.ClassIDE),
	"testing-refresh":      aiAuto(),
	"testing-collapse":     aiAuto(),
	"testing-results":      aiAuto(),

	// ── Tasks ──
	"tasks-run":            aiNever(aiNoPicker),
	"tasks-build":          aiConfirm(ai.ClassRun),
	"tasks-test":           aiConfirm(ai.ClassRun),
	"tasks-rerun":          aiConfirm(ai.ClassRun),
	"tasks-terminate":      aiConfirm(ai.ClassIDE),
	"tasks-configure":      aiConfirm(ai.ClassIDE), // may create tasks.json
	"tasks-clear-problems": aiAuto(),

	// ── Extensions ──
	"ext-manage-reload":          aiConfirm(ai.ClassRun), // runs extension code
	"ext-manage-open-folder":     aiNever(aiNoOutside),
	"ext-manage-log":             aiAuto(),
	"ext-manage-new":             aiNever(aiNoPicker),
	"ext-manage-install-samples": aiConfirm(ai.ClassRun), // installs code that runs

	// ── AI (the model's own chat and settings) ──
	"ai-open-chat":        aiAuto(),
	"ai-new-chat":         aiNever(aiNoNested),
	"ai-history":          aiNever(aiNoNested),
	"ai-explain":          aiNever(aiNoNested),
	"ai-fix":              aiNever(aiNoNested),
	"ai-edit":             aiNever(aiNoNested),
	"ai-doc":              aiNever(aiNoNested),
	"ai-attach":           aiNever("adds to the chat input; read the file with read_file"),
	"ai-apply-code":       aiNever("use write_file to change files"),
	"ai-apply-proposal":   aiNever("the user reviews proposed edits"),
	"ai-export-chat":      aiConfirm(ai.ClassFileWrite), // writes ai-chat-<date>.md
	"ai-discard-proposal": aiNever("the user reviews proposed edits"),
	"ai-trigger-inline":   aiNever("inline completion is for the user's typing"),
	"ai-toggle-inline":    aiConfirm(ai.ClassIDE),
	"ai-cancel":           aiNever("stops the model's own turn"),
	"ai-configure":        aiNever(aiNoAIConfig),
	"ai-pick-model":       aiNever(aiNoAIConfig),
	"ai-auto-approve":     aiNever(aiNoAIConfig),
	"ai-sign-in-claude":   aiNever(aiNoAIConfig),
	"ai-sign-out-claude":  aiNever(aiNoAIConfig),
	"ai-credentials":      aiNever(aiNoAIConfig),
}

// aiActionPolicyFor returns the policy of a palette id, including the
// dynamic ones: extension commands (ext-…) and user commands (user-…)
// run code, so they ask with class run.
func aiActionPolicyFor(id string) (aiActionPolicy, bool) {
	if p, ok := aiActionPolicies[id]; ok {
		return p, true
	}
	if strings.HasPrefix(id, extPaletteIDPfx) || strings.HasPrefix(id, "user-") {
		return aiConfirm(ai.ClassRun), true
	}
	return aiActionPolicy{}, false
}

// aiSettingPolicies classifies every setting (SettingsCatalog keys). K2
// adds settings_set; until then "planned" settings are not writable by
// the AI either.
const aiPlannedSettings = "planned: K2 settings_set"

var aiSettingPolicies = map[string]aiActionPolicy{
	"theme":               {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"icons":               {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"animations":          {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"images":              {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"dashboard_logo":      {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"font_delta":          {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"auto_save":           {Gate: aiGateConfirm, Class: ai.ClassIDE, Reason: aiPlannedSettings},
	"word_wrap":           {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"show_hidden":         {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"tab_size":            {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"persistent_session":  {Gate: aiGateConfirm, Class: ai.ClassIDE, Reason: aiPlannedSettings},
	"search_max_results":  {Gate: aiGateAuto, Reason: aiPlannedSettings},
	"terminal_skip_shell": {Gate: aiGateConfirm, Class: ai.ClassIDE, Reason: aiPlannedSettings},
	"ai_inline":           {Gate: aiGateConfirm, Class: ai.ClassIDE, Reason: aiPlannedSettings},
	"ai_inline_model":     aiNever(aiNoAIConfig),
	"ai_provider":         aiNever(aiNoAIConfig),
	"ai_model":            aiNever(aiNoAIConfig),
	"ai_base_url":         aiNever(aiNoAIConfig),
	"ai_auto_approve":     aiNever(aiNoAIConfig),
	"ai_max_steps":        aiNever(aiNoAIConfig),
}

// aiListActionsMax caps list_actions results.
const aiListActionsMax = 40

// aiActionCategory is the part of a palette title before ": " ("Git" for
// "Git: Push"), or "General".
func aiActionCategory(title string) string {
	if i := strings.Index(title, ": "); i > 0 {
		return title[:i]
	}
	return "General"
}

// aiListActions is the list_actions result: palette items (never ids left
// out) matching query, at most max. Every word of the query must appear in
// the id or title; when nothing matches, the letters of the query in order
// (fuzzy) are tried.
func aiListActions(items []picker.Item, query string, max int) string {
	type row struct {
		it picker.Item
		p  aiActionPolicy
	}
	var allowed []row
	seen := map[string]bool{}
	for _, it := range items {
		p, ok := aiActionPolicyFor(it.ID)
		if !ok || p.Gate == aiGateNever || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		allowed = append(allowed, row{it, p})
	}
	q := strings.ToLower(strings.TrimSpace(query))
	words := strings.Fields(q)
	hay := func(r row) string { return strings.ToLower(r.it.ID + " " + r.it.Title) }
	var hits []row
	for _, r := range allowed {
		h, all := hay(r), true
		for _, w := range words {
			if !strings.Contains(h, w) {
				all = false
				break
			}
		}
		if all {
			hits = append(hits, r)
		}
	}
	if len(hits) == 0 && q != "" {
		letters := strings.Join(words, "")
		for _, r := range allowed {
			if aiSubsequence(strings.ToLower(r.it.ID), letters) || aiSubsequence(strings.ToLower(r.it.Title), letters) {
				hits = append(hits, r)
			}
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("no matching IDE commands for %q; try a shorter or different word", query)
	}
	var b strings.Builder
	for i, r := range hits {
		if i == max {
			fmt.Fprintf(&b, "\n(%d more; use a narrower query)", len(hits)-max)
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- %s: %s (%s; %s)", r.it.ID, r.it.Title, aiActionCategory(r.it.Title), r.p.Gate)
	}
	return b.String()
}

// aiSubsequence reports whether the letters of sub appear in s in order.
func aiSubsequence(s, sub string) bool {
	i := 0
	for j := 0; j < len(s) && i < len(sub); j++ {
		if s[j] == sub[i] {
			i++
		}
	}
	return i == len(sub)
}
