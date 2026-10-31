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
	aiNoUpdate   = "updates or rolls back the termocode binary; only the user decides that"
	aiNoOutside  = "acts outside the IDE (OS shell, file manager or browser)"
	aiNoAIConfig = "AI provider, credentials and approvals stay with the user"
	aiNoNested   = "starts or changes an AI chat or edit; answer in this chat instead"
	aiNoTransfer = "moves files between the user's machine and the server; user only"
	aiNoMerge    = "resolves the user's merge conflict in the merge view; user only"
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
	"open-workspace":    aiNever("restarts termocode in another folder and ends this chat (workspace_folders adds a folder)"),
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
	"toggle-key-hints":     aiAuto(),
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
	"help-guide":           aiNever(aiNoPicker),
	"help-context":         aiAuto(),
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
	// terminal splits (terminal_split.go)
	"terminal-split-right":     aiAuto(),
	"explorer-toggle-preview":  aiAuto(), // UI only: shows or hides the preview pane
	"explorer-rename-selected": aiNever(aiNoPicker),
	"explorer-undo-rename":     aiNever("undoes the user's own bulk rename; only the user decides that"),
	"terminal-split-down":      aiAuto(),
	"terminal-close-pane":      aiDestructive(), // kills what runs in it
	"terminal-focus-next-pane": aiAuto(),
	"terminal-focus-prev-pane": aiAuto(),
	"terminal-grow-pane":       aiAuto(),
	"terminal-shrink-pane":     aiAuto(),
	"terminal-open-link":       aiNever(aiNoPicker + " (open_file)"),
	"open-shell":               aiNever(aiNoOutside),

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
	"hover":                       aiAuto(),
	"find-references":             aiAuto(),
	"goto-type-def":               aiAuto(),
	"goto-impl":                   aiAuto(),
	"code-actions":                aiNever(aiNoPicker),
	"code-action-apply-preferred": aiNever("applies an edit chosen by the language server; the AI uses lsp tools instead"),
	"goto-symbol-file":            aiNever(aiNoPicker + " (lsp_nav)"),
	"goto-symbol-workspace":       aiNever(aiNoPicker + " (lsp_nav)"),
	"lsp-manage":                  aiNever(aiNoPicker + " (lsp_servers)"),

	// ── Git ──
	"git-stage":           aiConfirm(ai.ClassGit),
	"git-stage-all":       aiConfirm(ai.ClassGit),
	"git-stage-hunk":      aiConfirm(ai.ClassGit),
	"git-unstage-hunk":    aiConfirm(ai.ClassGit),
	"git-discard-hunk":    aiDestructive(),
	"git-stage-lines":     aiConfirm(ai.ClassGit),
	"git-unstage-lines":   aiConfirm(ai.ClassGit),
	"git-discard-lines":   aiDestructive(),
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
	"git-stash-list":      aiNever(aiNoPicker + " (git_stash list)"),
	"git-log":             aiNever(aiNoPicker + " (git_log)"),
	"git-compare-rev":     aiNever(aiNoPicker),
	"git-history":         aiNever(aiNoPicker),
	"git-tags":            aiNever(aiNoPicker),
	"git-conflict-next":   aiAuto(),
	"git-conflict-prev":   aiAuto(),
	"git-conflict-ours":   aiConfirm(ai.ClassFileWrite),
	"git-conflict-theirs": aiConfirm(ai.ClassFileWrite),
	"git-conflict-both":   aiConfirm(ai.ClassFileWrite),
	"git-clone":           aiNever("clones into a folder outside the workspace and opens it"),
	"git-ci":              aiNever(aiNoPicker),
	"layout-open":         aiNever(layoutNoAI),
	"layout-save":         aiNever(layoutNoAI),
	"layout-delete":       aiNever(layoutNoAI),
	"layout-reset":        aiNever(layoutNoAI),
	"git-ci-fix":          aiNever(aiNoNested),
	"update-check":        aiNever(aiNoUpdate),
	"update-rollback":     aiNever(aiNoUpdate),
	"git-pr-list":         aiAuto(), // read only: a list of open pull requests
	"git-pr-create":       aiNever("publishes a pull request in the user's name; only the user creates one"),
	"git-rebase":          aiNever(aiNoPicker),
	"git-fixup":           aiNever(aiNoPicker),
	"git-rebase-status":   aiNever(aiNoPicker),
	"git-undo":            aiNever(aiNoGitUndo),
	"git-redo":            aiNever(aiNoGitUndo),
	"git-reflog":          aiNever(aiNoPicker),

	// ── Merge view (git_merge_view.go): opening and moving around are
	// UI only; each Take changes the user's merge result, so the user
	// decides it. ──
	"git-merge-open":                   aiAuto(),
	"git-merge-next":                   aiAuto(),
	"git-merge-prev":                   aiAuto(),
	"git-merge-pane":                   aiAuto(),
	"git-merge-focus":                  aiAuto(),
	"git-merge-close":                  aiAuto(),
	"git-merge-take-ours":              aiNever(aiNoMerge),
	"git-merge-take-theirs":            aiNever(aiNoMerge),
	"git-merge-take-both":              aiNever(aiNoMerge),
	"git-merge-take-both-theirs-first": aiNever(aiNoMerge),
	"git-merge-take-base":              aiNever(aiNoMerge),
	"git-merge-undo":                   aiNever(aiNoMerge),
	"git-merge-resolve":                aiNever(aiNoMerge),

	// ── Workspace, session, settings ──
	"workspace-add-folder":    aiNever(aiNoPicker + " (workspace_folders)"),
	"workspace-remove-folder": aiNever(aiNoPicker + " (workspace_folders)"),
	"reveal-fs":               aiNever(aiNoOutside),
	"transfer-download":       aiNever(aiNoTransfer),
	"transfer-upload":         aiNever(aiNoTransfer),
	"transfer-cancel":         aiNever(aiNoTransfer),
	"transfer-reveal":         aiNever(aiNoTransfer),
	"open-url":                aiNever(aiNoOutside),
	"reload-window":           aiNever(aiNoSession),
	"detach-session":          aiNever(aiNoSession),
	"reset-session":           aiNever(aiNoSession),
	"pick-theme":              aiNever(aiNoPicker),
	"open-settings-ui":        aiNever(aiNoPicker),
	"setup-show-card":         aiAuto(),
	"edit-custom-theme":       aiNever(aiNoPicker),
	"customize-keybindings":   aiNever(aiNoPicker),
	"settings-export":         aiNever("settings import / export stays with the user"),
	"settings-import":         aiNever("settings import / export stays with the user"),
	"prefs-import-editor":     aiNever("importing another editor's settings stays with the user"),
	"prefs-import-undo":       aiNever("undoing a settings import stays with the user"),
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
	"debug-select-config":     aiNever(aiNoPicker + " (debug_start with config)"),
	"debug-conditional-bp":    aiNever(aiNoPicker + " (debug_breakpoint with condition)"),
	"debug-logpoint":          aiNever(aiNoPicker + " (debug_breakpoint with log_message)"),
	"debug-add-watch":         aiNever(aiNoPicker + " (debug_evaluate; debug_state shows the watches)"),
	"dap-install":             aiNever(aiNoPicker + " (dap_adapters)"),

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

	// ── Services (services_actions.go; typed tools in ai_services_tools.go) ──
	"services-focus":        aiAuto(),
	"services-refresh":      aiAuto(),
	"services-start-all":    aiConfirm(ai.ClassServices),
	"services-stop-all":     aiConfirm(ai.ClassServices),
	"services-start":        aiNever(aiNoPicker + " (services_start)"),
	"services-stop":         aiNever(aiNoPicker + " (services_stop)"),
	"services-restart":      aiNever(aiNoPicker + " (services_restart)"),
	"services-logs":         aiNever(aiNoPicker + " (services_logs)"),
	"services-configure":    aiConfirm(ai.ClassIDE), // may create .termocode/services.json
	"services-open-compose": aiAuto(),
	"services-trust":        aiNever(aiNoAIConfig),

	// ── Databases (DB-3: connection management only) ──
	"db-new-console":               aiNever(aiNoPicker),
	"db-run":                       aiNever("SQL runs require the database query tool and its guard"),
	"db-run-file":                  aiNever("SQL runs require the database query tool and its guard"),
	"db-explain":                   aiNever(aiNoPicker + " (db_explain)"),
	"db-explain-analyze":           aiNever("executes the statement; use db_explain"),
	"db-history":                   aiNever(aiNoPicker),
	"db-cancel":                    aiAuto(),
	"db-focus":                     aiAuto(),
	"db-refresh":                   aiAuto(),
	"db-filter":                    aiNever("the filter is typed by the user; the AI uses db_schema"),
	"db-guide":                     aiAuto(),
	"db-connect":                   aiNever(aiNoPicker),
	"db-disconnect":                aiDestructive(),
	"db-test-connection":           aiAuto(),
	"db-add":                       aiNever(aiNoAIConfig),
	"db-add-tunnel":                aiNever(aiNoAIConfig),
	"db-ssl":                       aiNever(aiNoAIConfig),
	"db-configure":                 aiDestructive(),
	"db-data":                      aiNever("Table rows are read with the database tools"),
	"db-select":                    aiNever("SQL runs require the database query tool and its guard"),
	"db-describe":                  aiAuto(),
	"db-ddl":                       aiAuto(),
	"db-count-rows":                aiNever("Table rows are read with the database tools"),
	"db-transaction-mode":          aiNever(aiNoAIConfig),
	"db-commit":                    aiNever("the user's transaction"),
	"db-rollback":                  aiNever("the user's transaction"),
	"db-submit-changes":            aiNever("the user's pending edits; use the database query tool"),
	"db-revert-changes":            aiNever("the user's pending edits"),
	"db-result-pin":                aiAuto(),
	"db-export":                    aiNever(aiNoPicker + " (db_query + write_file)"),
	"db-import-csv":                aiNever("importing rows is a write the user confirms (db_query)"),
	"db-compare-schemas":           aiNever(aiNoPicker),
	"redis-set-ttl":                aiNever(aiNoPicker + " (redis_command)"),
	"redis-delete-key":             aiNever(aiNoPicker + " (redis_command)"),
	"containers-focus":             aiAuto(),
	"kubernetes-show":              aiAuto(), // read only (kube_actions.go)
	"kubernetes-refresh":           aiAuto(),
	"kubernetes-context":           aiNever(aiNoPicker + "; the context is the user's choice"),
	"kubernetes-namespace":         aiNever(aiNoPicker),
	"containers-context-use":       aiNever("a context switch is the user's choice; it moves every container action to another host"),
	"containers-context-default":   aiNever("changes the machine-wide Docker default"),
	"containers-registry-connect":  aiNever("signing in to a registry takes the user's token; it is the user's"),
	"containers-registry-signout":  aiNever("signing out of a registry is the user's"),
	"containers-search":            aiNever(aiNoPicker + "; it asks Docker Hub on the user's behalf"),
	"containers-browse-tags":       aiNever(aiNoPicker + "; it asks a registry over the network on the user's behalf"),
	"containers-filter":            aiNever("the filter is typed by the user; the AI uses containers_list / images_list"),
	"containers-refresh":           aiAuto(),
	"containers-help":              aiAuto(),
	"containers-start":             aiDestructive(),
	"containers-stop":              aiDestructive(),
	"containers-restart":           aiDestructive(),
	"containers-pause":             aiDestructive(),
	"containers-remove":            aiNever("container removal requires the user's confirm card"),
	"containers-compose-down":      aiNever("project removal requires the user's confirm card"),
	"containers-compose-up":        aiDestructive(),
	"containers-compose-stop":      aiDestructive(),
	"containers-compose-restart":   aiDestructive(),
	"containers-logs":              aiAuto(),
	"containers-shell":             aiNever(aiNoPicker + " (container shell)"),
	"containers-attach":            aiNever(aiNoPicker + " (container attach)"),
	"containers-stats":             aiAuto(),
	"containers-inspect":           aiAuto(),
	"containers-copy-id":           aiAuto(),
	"containers-open-port":         aiAuto(),
	"containers-open-databases":    aiNever(aiNoPicker + " (db_connections)"),
	"containers-image-pull":        aiDestructive(),
	"containers-image-push":        aiNever("pushing an image is the user's; nothing is offered to the model"),
	"containers-image-tag":         aiNever("tagging an image is the user's; nothing is offered to the model"),
	"containers-image-run":         aiNever("running an image is the user's; nothing is offered to the model (containers_control starts existing containers)"),
	"containers-image-history":     aiAuto(),
	"containers-image-inspect":     aiAuto(),
	"containers-image-remove":      aiNever("removing is the user's; nothing is offered to the model"),
	"containers-prune":             aiNever("pruning containers is the user's; nothing is offered to the model"),
	"containers-system-prune":      aiNever("pruning the system is the user's; nothing is offered to the model"),
	"containers-image-prune":       aiNever("pruning images is the user's; nothing is offered to the model"),
	"containers-network-create":    aiDestructive(),
	"containers-network-remove":    aiNever("removing is the user's; nothing is offered to the model"),
	"containers-network-prune":     aiNever("pruning networks is the user's; nothing is offered to the model"),
	"containers-volume-create":     aiDestructive(),
	"containers-volume-remove":     aiNever("removing is the user's; nothing is offered to the model"),
	"containers-volume-prune":      aiNever("pruning volumes is the user's; nothing is offered to the model"),
	"containers-volume-disk-usage": aiNever("disk usage is the user's; nothing is offered to the model"),

	// ── Tasks ──
	"tasks-run":            aiNever(aiNoPicker),
	"tasks-build":          aiConfirm(ai.ClassRun),
	"tasks-test":           aiConfirm(ai.ClassRun),
	"tasks-rerun":          aiConfirm(ai.ClassRun),
	"tasks-terminate":      aiConfirm(ai.ClassIDE),
	"tasks-configure":      aiConfirm(ai.ClassIDE), // may create tasks.json
	"tasks-clear-problems": aiAuto(),

	// ── Extensions ──
	"ext-manage-reload":          aiConfirm(ai.ClassRun), // runs extension code; also extensions(op: reload)
	"ext-manage-open-folder":     aiNever(aiNoOutside),
	"ext-manage-log":             aiAuto(), // extensions(op: log) returns the text
	"ext-manage-new":             aiNever(aiNoPicker),
	"ext-manage-new-process":     aiNever(aiNoPicker),
	"ext-manage-install-samples": aiConfirm(ai.ClassRun), // installs code that runs

	// ── AI (the model's own chat and settings) ──
	"ai-open-chat":        aiAuto(),
	"ai-new-chat":         aiNever(aiNoNested),
	"ai-history":          aiNever(aiNoNested),
	"ai-history-rename":   aiNever(aiNoNested),
	"ai-fork-chat":        aiNever(aiNoNested),
	"ai-history-delete":   aiNever("deletes saved chats; only the user may"),
	"ai-history-clear":    aiNever("deletes saved chats; only the user may"),
	"ai-explain":          aiNever(aiNoNested),
	"ai-fix":              aiNever(aiNoNested),
	"ai-edit":             aiNever(aiNoNested),
	"ai-doc":              aiNever(aiNoNested),
	"ai-attach":           aiNever("adds to the chat input; read the file with read_file"),
	"ai-apply-code":       aiNever("use write_file to change files"),
	"ai-apply-proposal":   aiNever("the user reviews proposed edits"),
	"ai-export-chat":      aiConfirm(ai.ClassFileWrite), // writes ai-chat-<date>.md
	"ai-discard-proposal": aiNever("the user reviews proposed edits"),
	"ai-review":           aiNever("the user reviews proposed edits"),
	"ai-edit-review":      aiNever(aiNoAIConfig),
	"ai-undo-turn":        aiNever("the user undoes the AI's changes"),
	"ai-trigger-inline":   aiNever("inline completion is for the user's typing"),
	"ai-toggle-inline":    aiConfirm(ai.ClassIDE),
	"ai-toggle-next-edit": aiConfirm(ai.ClassIDE),
	"ai-cancel":           aiNever("stops the model's own turn"),
	"ai-configure":        aiNever(aiNoAIConfig),
	"ai-pick-model":       aiNever(aiNoAIConfig),
	"ai-auto-approve":     aiNever(aiNoAIConfig),
	"ai-sign-in-claude":   aiNever(aiNoAIConfig),
	"ai-sign-out-claude":  aiNever(aiNoAIConfig),
	"ai-credentials":      aiNever(aiNoAIConfig),
	"ai-mcp-servers":      aiNever(aiNoPicker),
	"ai-mcp-insert":       aiNever(aiNoPicker),
	"ai-mcp-background":   aiNever("moves the model's own running call; the user decides"),
	"ai-mcp-profile":      aiNever(aiNoPicker),
	"ai-mcp-catalog":      aiNever(aiNoPicker),
	"ai-mcp-bundle":       aiNever(aiNoPicker),
	"ai-mcp-updates":      aiNever(aiNoAIConfig),
	"ai-mcp-connect":      aiNever(aiNoAIConfig + " (signing in to services is the user's)"),
	"ai-mcp-log":          aiAuto(),
	"ai-mcp-reset-cache":  aiNever(aiNoAIConfig),
	"ai-mcp-config":       aiNever(aiNoAIConfig),
	"mcp-import-other":    aiNever(aiNoAIConfig + " (importing servers is the user's)"),
	// Only servers that ran before (no trust card); network only.
	"ai-mcp-reconnect-all": aiConfirm(ai.ClassIDE),
	// termocode as an MCP server (mcp_serve_status.go): the user's own
	// screens; the config snippet is for the user's agents, not the chat.
	"mcp-serve-status":      aiNever(aiNoPicker),
	"mcp-serve-copy-config": aiNever(aiNoAIConfig + " (the config is for the user to paste into an agent)"),
	// The web panel gives shell access from a browser (web_tui.go).
	"web-start":       aiNever(aiNoWeb),
	"web-start-panel": aiNever(aiNoWeb),
	"web-stop":        aiNever(aiNoWeb),
	"web-copy-link":   aiNever(aiNoWeb + " (the link logs a browser in)"),
	"web-pair":        aiNever(aiNoWeb + " (pairing lets another device in)"),
	"web-devices":     aiNever(aiNoWeb + " (who may come in)"),
	"web-totp":        aiNever(aiNoWeb + " (a second factor of the web sign-in)"),
	"web-share":       aiNever(aiNoWeb + " (lets another person watch)"),
	// Itermoc (itermoc_actions.go): the user's screen for profiles, models,
	// services and outside agents. The AI opens it with the typed tool
	// ai_view_open (ai_itermoc_tools.go); as palette commands they are the user's.
	"itermoc-open":      aiNever(itermocNoOpen),
	"itermoc-open-ai":   aiNever(itermocNoOpen),
	"itermoc-profiles":  aiNever(itermocNoOpen),
	"itermoc-models":    aiNever(itermocNoOpen),
	"itermoc-services":  aiNever(itermocNoOpen),
	"itermoc-agents":    aiNever(itermocNoOpen),
	"ai-model-slot-add": aiNever(itermocNoSlotEdit),
	"ai-add":            aiNever(addAINoAI),
	// The secret store (secrets_ui.go): unlocking and locking are the
	// user's; the AI never reads, sets or lists secrets.
	"secrets-unlock": aiNever(aiNoAIConfig),
	"secrets-lock":   aiNever(aiNoAIConfig),
	"secrets-store":  aiNever(aiNoAIConfig),
	// Moving secrets out of the plain file is the user's (U19 SS-4).
	"secrets-migrate": aiNever(aiNoAIConfig),
	// Changing the passphrase is the user's (U19 SS-8).
	"secrets-passphrase": aiNever(aiNoAIConfig),
	// Agent profiles (U20 AP-5): the model may suggest a profile (the
	// user confirms the switch); editing and trusting stay with the user.
	"ai-profile-switch": aiConfirm(ai.ClassSettings),
	"ai-profile-next":   aiNever(aiNoAIConfig + " (ai-profile-switch asks the user)"),
	"ai-profile-show":   aiNever("a read-only screen for the user; the AI already knows its own profile"),
	// Work until done (OM-3) is a profile switch: the user confirms it.
	"ai-until-done": aiConfirm(ai.ClassSettings),
	// Plan approval (G-PLAN): the AI can never approve its own plan or
	// turn the question off.
	"ai-reasoning-next": aiNever(aiNoAIConfig + "; more thinking costs the user more"),
	"ai-plan-approval":  aiNever(aiNoAIConfig + "; the AI never approves its own plan"),
	"ai-plan-review":    aiNever("the plan card is the user's answer; the AI never approves its own plan"),
	"ai-profile-edit":   aiNever(aiNoAIConfig),
	"ai-profile-new":    aiNever(aiNoAIConfig),
	"ai-profile-form":   aiNever(aiNoAIConfig + " (the step form is for the user; a typed tool comes later)"),
	"ai-profile-trust":  aiNever("trusting a project's prompts is the user's decision"),
	// Keep going (OMX-7) sends model turns that cost money: only the
	// user turns it on.
	"ai-keep-going": aiNever(aiNoAIConfig + "; extra paid turns are the user's choice"),
	// The side chat (OMX-30) is the user's own question.
	"ai-side": aiNever("the side chat is the user's own question beside the AI's turn"),
	// Handoff (OMX-21) ends the AI's own chat; the user starts it.
	"ai-handoff": aiNever("it ends the AI's own chat; the user starts a handoff"),
	// Project rules (OM-1): the AI only gets the text in its prompt.
	"ai-rules":       aiNever("trusting a project's rules is the user's decision"),
	"ai-models-edit": aiNever(aiNoAIConfig),
	// Memory (OM-2): the AI gets no memory tool; turning it on or off and
	// forgetting lessons is the user's.
	"ai-memory":        aiNever(aiNoPicker),
	"ai-memory-toggle": aiNever(aiNoAIConfig),
	// Skills (OM-4): the AI loads them with skill_load; the picker, new
	// files and trusting a project's skills are the user's.
	"ai-skills":       aiNever(aiNoPicker),
	"help-bug-report": aiNever("only the user chooses what to export"),
	"ai-doctor":       aiNever("a local diagnostic page for the user"),
	"ai-hooks":        aiNever(aiNoPicker),
	// Web tools reach the internet and add servers: the user turns them on.
	"ai-web-tools": aiNever(aiNoPicker),
	// Ship pushes and opens pull requests: only the user starts it.
	"ai-ship": aiNever("it pushes and opens a pull request: the user starts it"),
	// It edits other apps' config files: only the user, after the preview.
	"agent-hooks": aiNever("it changes other apps' config files: the user decides after the preview"),
	// The AGENTS view (U20 AP-9) only shows the chat's runs.
	"ai-agents": aiAuto(),
	// Agent worktrees (G-WT): merging or discarding an agent's work and
	// turning the feature off are the user's.
	"ai-worktrees":        aiNever(aiNoPicker),
	"ai-worktrees-toggle": aiNever(aiNoAIConfig),
	// AI: Usage (AU-4) is the user's own view of tokens and money.
	"ai-usage": aiNever("a read-only screen for the user; the AI cannot read its own bill"),
	// Runs is the user's monitoring page (stop, pause and resume are theirs).
	"ai-runs": aiNever("the user's monitoring page; it holds Stop, Pause and Resume"),

	// ── HTTP client ──
	"http-send":             aiNever("sends a network request; use http_send (gated by host and method)"),
	"http-send-pick":        aiNever(aiNoPicker + " (http_list_requests, http_send)"),
	"http-quick":            aiNever(aiNoPicker + " (http_send with method and url)"),
	"http-cancel":           aiConfirm(ai.ClassIDE), // stops the user's request
	"http-last-response":    aiAuto(),
	"http-history":          aiNever(aiNoPicker + " (http_history)"),
	"http-env-select":       aiNever(aiNoPicker + " (http_set_environment)"),
	"http-env-open":         aiAuto(),
	"http-env-open-private": aiNever("opens the file that holds the user's secrets"),
	"http-saved":            aiNever(aiNoPicker + " (http_list_requests on http/saved.http)"),
	"http-save":             aiNever(aiNoPicker + " (http_save_request)"),
	"http-copy-curl":        aiConfirm(ai.ClassIDE), // overwrites the system clipboard
	"http-import-curl":      aiNever(aiNoPicker + " (http_save_request)"),
	"http-new-file":         aiNever(aiNoPicker + " (create_file)"),
	"http-save-body":        aiNever(aiNoPicker + " (http_get_last_response, write_file)"),
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

// aiSettingPolicies classifies every setting (SettingsCatalog keys) for
// settings_list / settings_set (ai_settings_tools.go). Every exposed
// setting asks first with class settings, so the user can "Always allow"
// them in AI: Agent Auto-Approve…; never settings are not listed.
var aiSettingPolicies = map[string]aiActionPolicy{
	"notifications":       aiNever("the user controls desktop notifications"),
	"theme":               aiConfirm(ai.ClassSettings),
	"icons":               aiConfirm(ai.ClassSettings),
	"animations":          aiConfirm(ai.ClassSettings),
	"images":              aiConfirm(ai.ClassSettings),
	"dashboard_logo":      aiConfirm(ai.ClassSettings),
	"font_delta":          aiConfirm(ai.ClassSettings),
	"auto_save":           aiConfirm(ai.ClassSettings),
	"word_wrap":           aiConfirm(ai.ClassSettings),
	"show_hidden":         aiConfirm(ai.ClassSettings),
	"tab_size":            aiConfirm(ai.ClassSettings),
	"key_hints":           aiConfirm(ai.ClassSettings),
	"persistent_session":  aiConfirm(ai.ClassSettings),
	"auto_reconnect":      aiConfirm(ai.ClassSettings),
	"search_max_results":  aiConfirm(ai.ClassSettings),
	"update_check":        aiConfirm(ai.ClassSettings),
	"ai_inline":           aiConfirm(ai.ClassSettings),
	"ai_next_edit":        aiConfirm(ai.ClassSettings),
	"mouse_hover":         aiNever("a file-only terminal setting read at start; the user edits it in config.json"),
	"rtl":                 aiNever("a file-only display setting read at start; the user edits it in config.json"),
	"terminal_skip_shell": aiNever("decides which keys reach the shell; a file-only list the user edits in config.json"),
	"ai_inline_model":     aiNever(aiNoAIConfig),
	"ai_provider":         aiNever(aiNoAIConfig),
	"ai_model":            aiNever(aiNoAIConfig),
	"ai_base_url":         aiNever(aiNoAIConfig),
	"ai_auto_approve":     aiNever(aiNoAIConfig),
	"ai_max_steps":        aiNever(aiNoAIConfig),
	"ai_edit_review":      aiNever(aiNoAIConfig),
	"ai_prices":           aiNever(aiNoAIConfig),
	"ai_context_windows":  aiNever(aiNoAIConfig),
	"ai_models":           aiNever(aiNoAIConfig),
	"ai_prompt_cache":     aiNever(aiNoAIConfig),
	"ai_reasoning":        aiNever(aiNoAIConfig),
	"ai_fallback":         aiNever(aiNoAIConfig),
	"ai_concurrency":      aiNever(aiNoAIConfig),
	"ai_parallel_max":     aiNever(aiNoAIConfig),
	"ai_worktrees":        aiNever(aiNoAIConfig),
	"ai_compact":          aiNever(aiNoAIConfig),
	"ai_plan_approval":    aiNever(aiNoAIConfig + "; the AI never approves its own plan"),
	"ai_memory_recall":    aiNever(aiNoAIConfig),
	"ai_web_fetch":        aiNever(aiNoAIConfig + "; the AI never turns on its own web access"),
	"ai_web_search":       aiNever(aiNoAIConfig + "; the AI never turns on its own web access"),
	// Work until done: the AI never raises its own rounds or budget.
	"ai_until_done_rounds":     aiNever(aiNoAIConfig),
	"ai_until_done_budget_usd": aiNever(aiNoAIConfig),
	// MCP servers run programs and reach outside services: their
	// settings stay with the user.
	"mcp_enabled":           aiNever(aiNoAIConfig),
	"mcp_idle_timeout":      aiNever(aiNoAIConfig),
	"mcp_call_timeout":      aiNever(aiNoAIConfig),
	"mcp_max_result_kb":     aiNever(aiNoAIConfig),
	"mcp_trust_annotations": aiNever(aiNoAIConfig),
	"mcp_share":             aiNever(aiNoAIConfig),
	"mcp_tool_search":       aiNever(aiNoAIConfig),
	"mcp_catalog_url":       aiNever(aiNoAIConfig),
	"mcp_auto_update":       aiNever(aiNoAIConfig),
	"mcp_trusted":           aiNever(aiNoAIConfig),
	"mcp_disabled_tools":    aiNever(aiNoAIConfig),
	"mcp_log_level":         aiNever(aiNoAIConfig),
	// The secret store's backend and auto-lock (secrets_ui.go).
	"secret_store":        aiNever(aiNoAIConfig),
	"secret_lock_minutes": aiNever(aiNoAIConfig),
	// Services view (services_quit.go).
	"services_on_quit":          aiConfirm(ai.ClassSettings),
	"services_max_log_kb":       aiConfirm(ai.ClassSettings),
	"containers_sections":       aiConfirm(ai.ClassSettings),
	"containers_engine":         aiConfirm(ai.ClassSettings),
	"containers_max_inspect_kb": aiConfirm(ai.ClassSettings),
	"mcp_trusted_tools":         aiNever(aiNoAIConfig),
	"mcp_oauth_port":            aiNever(aiNoAIConfig),
	// HTTP client: limits the user may let the AI change; the AI never
	// raises its own budgets or turns TLS checks off.
	"db_max_rows":          aiNever(aiNoAIConfig),
	"db_page_rows":         aiNever(aiNoAIConfig),
	"db_timeout":           aiNever(aiNoAIConfig),
	"db_history_max":       aiNever(aiNoAIConfig),
	"db_ai_max_kb":         aiNever(aiNoAIConfig), // the AI never raises its own budget
	"db_redis_separator":   aiConfirm(ai.ClassSettings),
	"db_redis_max_keys":    aiNever(aiNoAIConfig),
	"http_timeout":         aiConfirm(ai.ClassSettings),
	"http_max_redirects":   aiConfirm(ai.ClassSettings),
	"http_cookie_jar":      aiConfirm(ai.ClassSettings),
	"http_history_max":     aiConfirm(ai.ClassSettings),
	"http_tls_insecure":    aiNever(aiNoAIConfig),
	"http_max_body_mb":     aiNever(aiNoAIConfig),
	"http_ai_max_kb":       aiNever(aiNoAIConfig),
	"mcp_github_client_id": aiNever(aiNoAIConfig),
	// The door outside agents use to drive the IDE (mcp_serve*.go).
	"mcp_server": aiNever("lets outside agents use the IDE's tools; the user turns it on or off in config.json"),
	// What an outside agent may do without a card: the user's answer only.
	"mcp_server_auto_approve": aiNever("what outside agents may do without asking; only the user's \"Always allow\" on their card sets it"),
	// The web panel gives shell access from a browser (web_settings.go).
	"web_panel":        aiNever(aiNoWeb),
	"web_port":         aiNever(aiNoWeb),
	"web_listen":       aiNever(aiNoWeb),
	"web_require_totp": aiNever(aiNoWeb),
	"web_idle_minutes": aiNever(aiNoWeb),
	"web_max_hours":    aiNever(aiNoWeb),
	"web_open_browser": aiNever(aiNoWeb),
	"web_max_viewers":  aiNever(aiNoWeb),
}

// aiNoWeb is why the AI never touches the web panel.
const aiNoWeb = "the web panel gives shell access from a browser; only the user changes it, in config.json"

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
