package app

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/findbar"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

type pickerKindEnum int

const (
	pickerKindFiles pickerKindEnum = iota
	pickerKindPalette
	pickerKindTheme
	pickerKindSettingChoice
	pickerKindClipboard
	pickerKindProblems
	pickerKindCodeAction
	pickerKindSymbols
	pickerKindWorkspaceSymbols
	pickerKindBranches
	pickerKindRecentFiles
	pickerKindWorkspaces
	pickerKindStash
	pickerKindDiffLeft
	pickerKindDiffRight
	pickerKindGitLog
	pickerKindTags
	pickerKindSnippet
	pickerKindRemoveRoot
	pickerKindSettings
	pickerKindThemeEditor
	pickerKindKeybinding
	pickerKindToolManager    // LSP / DAP installer (lsp_manager.go)
	pickerKindSnippetManager // Group H — Snippets: Manage…
	pickerKindTasks          // Group C — Run Task… / Open Link… (tasks_run.go)
	pickerKindExt            // Group I — termocode.pick() from an extension
	pickerKindDebugConfig    // Group D — debug configuration pickers (dap.go)
	pickerKindSetupStep      // the setup card's step chooser (setup_card_ui.go)
	pickerKindHelpGuide      // Help: Open Guide… (help.go)
	pickerKindImportSource   // Import from Another Editor: the source (import_editor.go)
	pickerKindRebaseFrom     // Git: Interactive Rebase… (git_rebase.go)
	pickerKindFixupInto      // Git: Fixup Staged Changes Into… (git_fixup.go)
	pickerKindReflog         // Git: Reflog… (git_undo.go)
)

// loadFiles walks the cwd and returns one picker.Item per file (relative path).
//
// In a multi-root workspace it also walks every additional root persisted
// in the session file. Files in the cwd root keep their cwd-relative paths
// (Item.ID + Title) so existing call sites that `nvim.Command("edit "+id)`
// don't change behaviour. Files in extra roots use their absolute path as
// the ID (so :edit can find them) and a title prefixed with the root's
// basename so the picker reads as "myproj/foo.go".
//
// Dedup is by absolute path: a file already surfaced under the cwd root is
// not re-emitted if an extra root happens to overlap.
func loadFiles() []picker.Item {
	return loadFilesIn(workspaceRoots())
}

// loadFilesIn is the testable core of loadFiles: walks each root in `roots`
// (in order) and emits one picker.Item per file. Duplicates (by absolute
// path) are skipped.
// lastFileCount is the size of the last Go to file list. The next list
// starts with room for it (plus a little), so a big workspace's list is
// not copied again and again while it grows: on 50 000 files that copying
// was about a fifth of the listing time.
var lastFileCount atomic.Int64

func loadFilesIn(roots []string) []picker.Item {
	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		"dist":         true,
		"build":        true,
		"target":       true,
		"__pycache__":  true,
		".idea":        true,
		".vscode":      true,
		".cache":       true,
	}
	// WalkDir (no lstat per file) and paths cut from the walk's own
	// absolute path: listing 50 000 files took twice as long with Walk,
	// Abs and Rel on each one. A file can only show up twice when roots
	// overlap, so the seen set is kept only with more than one root.
	var seen map[string]bool
	if len(roots) > 1 {
		seen = make(map[string]bool, 256)
	}
	hint := int(lastFileCount.Load())
	items := make([]picker.Item, 0, hint+hint/16)
	defer func() { lastFileCount.Store(int64(len(items))) }()
	for i, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		prefix := abs
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		base := filepath.Base(abs)
		_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if path != abs && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, ok := strings.CutPrefix(path, prefix)
			if !ok {
				return nil
			}
			if seen != nil {
				if seen[path] {
					return nil
				}
				seen[path] = true
			}
			// Primary root (i==0): keep ID/Title cwd-relative so existing
			// behaviour (e.g. :edit <relpath>) is preserved. Extra roots:
			// use absolute path for ID, prefix the visible title with the
			// root basename so users can tell which project a file lives in.
			if i == 0 {
				items = append(items, picker.Item{ID: rel, Title: rel, File: true})
			} else {
				items = append(items, picker.Item{ID: path, Title: filepath.Join(base, rel), File: true})
			}
			return nil
		})
	}
	return items
}

// workspaceRoots returns the live list of workspace roots: cwd first, then
// any additional roots persisted via Add Folder. The explorer is the
// runtime source of truth, but it isn't reachable from a free function;
// instead we read the saved-roots list from the session file (kept in sync
// by the explorer's add/remove handlers). cwd is always first.
func workspaceRoots() []string {
	ws := procWorkspace()
	cwd, err := rootOf(ws)
	if err != nil {
		return nil
	}
	out := []string{cwd}
	for _, p := range loadSession().Roots {
		if p == "" || p == cwd {
			continue
		}
		// Checked where the workspace lives: a container's extra folders
		// are container paths (FIX-3).
		if _, err := statOn(ws, p); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// paletteItems returns the static list of commands shown in the command palette.
// Each item's ID is dispatched in dispatchPaletteAction. For items listed in
// paletteActions (docs.go) the Hint is replaced at open time by the keys
// bound in the keymap (withKeyHints); the Hint here only orders them.
func paletteItems() []picker.Item {
	items := []picker.Item{
		{ID: "save", Title: "File: Save", Hint: "Ctrl+S"},
		{ID: "save-all", Title: "File: Save All Files"},
		{ID: "close-buffer", Title: "View: Close Editor", Hint: "Ctrl+W"},
		{ID: "reopen-closed", Title: "View: Reopen Closed Editor", Hint: "Alt+Shift+T"},
		{ID: "clipboard-history", Title: "Edit: Clipboard History", Hint: "Ctrl+Shift+V"},
		{ID: "next-buffer", Title: "View: Next Editor", Hint: "Ctrl+PgDn"},
		{ID: "prev-buffer", Title: "View: Previous Editor", Hint: "Ctrl+PgUp"},
		{ID: "toggle-explorer", Title: "View: Toggle Explorer", Hint: "Ctrl+B"},
		{ID: "toggle-actions-panel", Title: "View: Toggle Actions Panel", Hint: "Alt+A"},
		{ID: "focus-explorer", Title: "View: Focus Explorer"},
		{ID: "focus-editor", Title: "View: Focus Editor"},
		{ID: "reveal-file", Title: "Reveal File in Explorer", Hint: "Ctrl+Shift+E"},
		{ID: "toggle-terminal", Title: "Terminal: Toggle Integrated Panel", Hint: "Ctrl+T"},
		{ID: "open-shell", Title: "Terminal: Open External Shell"},
		{ID: "terminal-new-tab", Title: "Terminal: New Tab", Hint: "Ctrl+Shift+`"},
		{ID: "terminal-close-tab", Title: "Terminal: Close Active Tab", Hint: "Ctrl+Shift+W"},
		{ID: "terminal-next-tab", Title: "Terminal: Next Tab", Hint: "Ctrl+Shift+PgDown"},
		{ID: "terminal-prev-tab", Title: "Terminal: Previous Tab", Hint: "Ctrl+Shift+PgUp"},
		{ID: "terminal-maximize", Title: "Terminal: Maximize Panel"},
		{ID: "terminal-minimize", Title: "Terminal: Minimize Panel"},
		// Bottom panel tabs (panel.go / output.go).
		{ID: "view-output", Title: "View: Output"},
		{ID: "output-clear", Title: "Output: Clear Channel"},
		{ID: "quick-open", Title: "Go to File...", Hint: "Ctrl+P"},
		{ID: "duplicate-line", Title: "Edit: Duplicate Line", Hint: "Shift+Alt+Down"},
		{ID: "toggle-comment", Title: "Edit: Toggle Line Comment", Hint: "Ctrl+/"},
		{ID: "fold-all", Title: "View: Fold All", Hint: "zM"},
		{ID: "unfold-all", Title: "View: Unfold All", Hint: "zR"},
		{ID: "toggle-fold", Title: "View: Toggle Fold Under Cursor", Hint: "za"},
		{ID: "preview-md", Title: "Markdown: Open Preview", Hint: "F7"},
		{ID: "find-files", Title: "Search: Find in Files", Hint: "F8 / Alt+F"},
		{ID: "search-next-result", Title: "Search: Go to Next Search Result", Hint: "F4"},
		{ID: "search-prev-result", Title: "Search: Go to Previous Search Result", Hint: "Shift+F4"},
		{ID: "find-files-overlay", Title: "Search: Find in Files (Live Overlay)"},
		{ID: "replace-in-file", Title: "Edit: Replace in File", Hint: "Ctrl+H"},
		{ID: "toggle-inlay-hints", Title: "LSP: Toggle Inlay Hints", Hint: "Alt+L"},
		{ID: "pick-theme", Title: "Preferences: Color Theme…"},
		{ID: "git-stage", Title: "Git: Stage Current File"},
		{ID: "git-stage-all", Title: "Git: Stage All Changes"},
		{ID: "git-unstage-all", Title: "Git: Unstage All Changes"},         // git_bulk.go
		{ID: "git-discard-all", Title: "Git: Discard All Changes…"},        // git_bulk.go
		{ID: "git-clean-untracked", Title: "Git: Delete Untracked Files…"}, // git_bulk.go
		{ID: "git-stage-hunk", Title: "Git: Stage Hunk (at cursor)"},
		{ID: "git-unstage-hunk", Title: "Git: Unstage Hunk (at cursor)"},
		{ID: "git-discard-hunk", Title: "Git: Discard Hunk (at cursor)"},
		{ID: "git-stage-lines", Title: "Git: Stage Line (or Selected Lines)"},
		{ID: "git-unstage-lines", Title: "Git: Unstage Line (or Selected Lines)"},
		{ID: "git-discard-lines", Title: "Git: Discard Line (or Selected Lines)"},
		{ID: "git-commit", Title: "Git: Commit..."},
		{ID: "git-commit-push", Title: "Git: Commit and Push"}, // git_bulk.go
		{ID: "git-commit-sync", Title: "Git: Commit and Sync"}, // git_bulk.go
		{ID: "git-diff", Title: "Git: Show Diff for Current File"},
		{ID: "git-discard", Title: "Git: Discard Changes in Current File"},
		{ID: "git-refresh", Title: "Git: Refresh Status"},
		{ID: "git-view-toggle", Title: "Git: Toggle Tree/Flat View"},
		{ID: "git-focus", Title: "Git: Focus Source Control"},
		{ID: "git-push", Title: "Git: Push"},
		{ID: "git-pull", Title: "Git: Pull"},
		{ID: "git-fetch", Title: "Git: Fetch"},
		{ID: "git-sync", Title: "Git: Sync (Pull, then Push)"},
		{ID: "git-branch", Title: "Git: Switch Branch..."},
		{ID: "git-branch-rename", Title: "Git: Rename Branch…"},    // git_branch_ops.go
		{ID: "git-branch-delete", Title: "Git: Delete Branch…"},    // git_branch_ops.go
		{ID: "git-branch-from", Title: "Git: Create Branch From…"}, // git_branch_ops.go
		{ID: "problems", Title: "Go to Problem..."},                // Group C: "View: Problems" opens the panel now
		{ID: "code-actions", Title: "Edit: Quick Fix...", Hint: "Ctrl+. / Alt+Enter"},
		{ID: "code-action-apply-preferred", Title: "Edit: Apply Preferred Quick Fix", Hint: "Ctrl+Shift+."},
		{ID: "goto-symbol-file", Title: "Go to Symbol in File...", Hint: "Ctrl+Shift+O"},
		{ID: "goto-symbol-workspace", Title: "Go to Symbol in Workspace..."},
		{ID: "bookmark-toggle", Title: "Bookmarks: Toggle on Current Line", Hint: "F3"},
		{ID: "bookmark-list", Title: "Bookmarks: Show List", Hint: "Shift+F3"},
		{ID: "bookmark-clear", Title: "Bookmarks: Clear All"},
		{ID: "goto-line", Title: "Go to Line...", Hint: "Ctrl+G"},
		{ID: "split-vertical", Title: "Editor: Split Right", Hint: "Ctrl+\\"},
		{ID: "split-horizontal", Title: "Editor: Split Down"},
		{ID: "close-split", Title: "Editor: Close Split"},
		{ID: "format-document", Title: "Edit: Format Document", Hint: "Shift+Alt+F"},
		{ID: "open-recent", Title: "File: Open Recent...", Hint: "Ctrl+R"},
		{ID: "open-workspace", Title: "File: Open Recent Workspace..."},
		{ID: "workspace-add-folder", Title: "Workspace: Add Folder..."},
		{ID: "workspace-remove-folder", Title: "Workspace: Remove Folder..."},
		{ID: "workspace-open-container", Title: "Workspace: Open Folder in Container..."},
		{ID: "workspace-reopen-devcontainer", Title: "Workspace: Reopen in Dev Container..."},
		{ID: "toggle-wrap", Title: "View: Toggle Word Wrap", Hint: "Alt+W"},
		{ID: "trim-whitespace", Title: "Edit: Trim Trailing Whitespace"},
		{ID: "reveal-fs", Title: "File: Reveal in File System"},
		{ID: "transfer-download", Title: "File: Download to This Computer"},
		{ID: "transfer-upload", Title: "File: Upload from This Computer..."},
		{ID: "transfer-cancel", Title: "File: Cancel Download"},
		{ID: "transfer-reveal", Title: "File: Show Last Download in Folder"},
		{ID: "diff-files", Title: "File: Compare Two Files..."},
		{ID: "git-stash", Title: "Git: Stash Changes"},
		{ID: "git-stash-pop", Title: "Git: Pop Latest Stash"},
		{ID: "git-stash-list", Title: "Git: Show Stash List..."},
		{ID: "sort-asc", Title: "Edit: Sort Lines Ascending"},
		{ID: "sort-desc", Title: "Edit: Sort Lines Descending"},
		{ID: "reverse-lines", Title: "Edit: Reverse Lines"},
		{ID: "uppercase", Title: "Edit: Convert to UPPERCASE"},
		{ID: "lowercase", Title: "Edit: Convert to lowercase"},
		{ID: "join-lines", Title: "Edit: Join Lines"},
		{ID: "alternate-file", Title: "View: Switch to Last File", Hint: "Ctrl+Tab"},
		{ID: "next-diagnostic", Title: "Go to Next Problem", Hint: "F5"},
		{ID: "prev-diagnostic", Title: "Go to Previous Problem", Hint: "Shift+F5"},
		{ID: "explorer-toggle-preview", Title: "Explorer: Toggle Preview"},
		{ID: "explorer-rename-selected", Title: "Explorer: Rename Selected…"},
		{ID: "explorer-undo-rename", Title: "Explorer: Undo Rename"},
		{ID: "toggle-hidden", Title: "Files: Toggle Hidden Files"},
		{ID: "cheat-sheet", Title: "Help: Show Shortcuts"},
		{ID: "toggle-key-hints", Title: "Help: Toggle Key Hints"},
		{ID: "buffer-info", Title: "View: Show Buffer Info", Hint: "Ctrl+Alt+I"},
		{ID: "error-log", Title: "Help: Show Error Log"},
		{ID: "run-doctor", Title: "Help: Run Doctor"},
		{ID: "setup-show-card", Title: "Setup: Show Setup Card"},
		{ID: "open-docs", Title: "Help: Open Documentation"}, // Docs
		{ID: "help-guide", Title: "Help: Open Guide…"},
		{ID: "help-context", Title: "Help: Explain This…", Hint: "Shift+F1"},
		{ID: "hover", Title: "LSP: Show Hover Documentation"},
		{ID: "find-references", Title: "LSP: Find All References", Hint: "Shift+F12"},
		{ID: "replace-workspace", Title: "Search: Replace in Workspace...", Hint: "Ctrl+Shift+H"},
		{ID: "goto-type-def", Title: "LSP: Go to Type Definition", Hint: "Ctrl+F12"},
		{ID: "goto-impl", Title: "LSP: Go to Implementation"},
		{ID: "run-tests", Title: "Run: Tests", Hint: "Alt+T"},
		{ID: "git-log", Title: "Git: Show Log..."},
		{ID: "git-compare-rev", Title: "Git: Compare with Revision..."},
		{ID: "git-blame", Title: "Git: Blame Current Line", Hint: "Alt+B"},
		{ID: "git-tag-create", Title: "Git: Create Tag…"},                            // git_tags_remotes.go
		{ID: "git-tag-delete", Title: "Git: Delete Tag…"},                            // git_tags_remotes.go
		{ID: "git-tag-push", Title: "Git: Push Tag…"},                                // git_tags_remotes.go
		{ID: "git-tag-delete-remote", Title: "Git: Delete Remote Tag…"},              // git_tags_remotes.go
		{ID: "git-tag-details", Title: "Git: Show Tag Details…"},                     // git_tags_remotes.go
		{ID: "git-remotes", Title: "Git: Manage Remotes…"},                           // git_tags_remotes.go
		{ID: "git-upstream-set", Title: "Git: Set Upstream Branch…"},                 // git_tags_remotes.go
		{ID: "git-upstream-unset", Title: "Git: Unset Upstream Branch"},              // git_tags_remotes.go
		{ID: "git-push-force", Title: "Git: Force Push (with Lease)…"},               // git_tags_remotes.go
		{ID: "git-stash-options", Title: "Git: Stash…"},                              // git_stash_reset.go
		{ID: "git-stash-actions", Title: "Git: Stash Actions…"},                      // git_stash_reset.go
		{ID: "git-uncommit", Title: "Git: Uncommit Last Commit (Keep Changes)"},      // git_stash_reset.go
		{ID: "git-commit-history", Title: "Git: Pick Recent Commit Message…"},        // git_commit_msg.go
		{ID: "git-commit-edit", Title: "Git: Edit Commit Message in Editor"},         // git_commit_msg.go
		{ID: "git-co-author", Title: "Git: Add Co-Author…"},                          // git_commit_msg.go
		{ID: "git-ai-commit-msg", Title: "Git: Generate Commit Message with AI"},     // git_ai_helpers.go
		{ID: "git-ai-stash", Title: "Git: Stash with AI Message"},                    // git_ai_helpers.go
		{ID: "git-ai-explain-changes", Title: "Git: Explain Changes with AI"},        // git_ai_helpers.go
		{ID: "git-ai-explain-line", Title: "Git: Explain Line's Commit with AI"},     // git_ai_helpers.go
		{ID: "git-ai-pr-text", Title: "Git: Write Pull Request Description with AI"}, // git_ai_helpers.go
		{ID: "git-inline-blame", Title: "Git: Toggle Inline Blame"},                  // git_blame_view.go
		{ID: "git-status-blame", Title: "Git: Toggle Blame in Status Bar"},           // git_blame_view.go
		{ID: "git-blame-annotate", Title: "Git: Toggle Blame Annotations"},           // git_blame_view.go
		{ID: "git-blame-options", Title: "Git: Blame Options…"},                      // git_blame_view.go
		{ID: "git-blame-commit", Title: "Git: Open Commit of Line"},                  // git_blame_view.go
		{ID: "git-blame-before", Title: "Git: Blame Before This Change"},             // git_blame_view.go
		{ID: "git-line-history", Title: "Git: Show Line History"},                    // git_blame_view.go
		{ID: "git-line-lens", Title: "Git: Toggle Line History Lens"},                // git_line_lens.go
		{ID: "git-block-history", Title: "Git: Show History of Block"},               // git_line_lens.go
		{ID: "git-rev-prev", Title: "Git: Open Previous Revision of File"},           // git_blame_view.go
		{ID: "git-rev-next", Title: "Git: Open Next Revision of File"},               // git_blame_view.go
		{ID: "git-history", Title: "Git: Show File History..."},
		{ID: "git-tags", Title: "Git: Show Tags..."},
		{ID: "zen-mode", Title: "View: Toggle Zen Mode", Hint: "Alt+Z"},
		{ID: "reload-window", Title: "Developer: Reload Window"},
		{ID: "detach-session", Title: "Session: Detach"},
		{ID: "reset-session", Title: "Developer: Reset Session (clears tabs / cursor history)"},
		{ID: "toggle-auto-save", Title: "File: Toggle Auto Save"},
		{ID: "next-hunk", Title: "Git: Go to Next Change", Hint: "Alt+]"},
		{ID: "prev-hunk", Title: "Git: Go to Previous Change", Hint: "Alt+["},
		// ── Group G: merge conflicts + clone (git_conflicts.go, git_clone.go) ──
		{ID: "git-conflict-next", Title: "Git: Go to Next Merge Conflict", Hint: "]x"},
		{ID: "git-conflict-prev", Title: "Git: Go to Previous Merge Conflict", Hint: "[x"},
		{ID: "git-conflict-ours", Title: "Git: Accept Current Change (Conflict at Cursor)"},
		{ID: "git-conflict-theirs", Title: "Git: Accept Incoming Change (Conflict at Cursor)"},
		{ID: "git-conflict-both", Title: "Git: Accept Both Changes (Conflict at Cursor)"},
		{ID: "git-clone", Title: "Git: Clone Repository..."},
		// Three-way merge view (git_merge_view.go).
		{ID: "git-merge-branch", Title: "Git: Merge Branch…"},                         // git_merge_branch.go
		{ID: "git-merge-status", Title: "Git: Merge in Progress (Continue / Abort)…"}, // git_merge_branch.go
		{ID: "git-merge-open", Title: "Git: Open Merge View"},
		{ID: "git-merge-next", Title: "Git: Merge View: Next Conflict", Hint: "n"},
		{ID: "git-merge-prev", Title: "Git: Merge View: Previous Conflict", Hint: "p"},
		{ID: "git-merge-take-ours", Title: "Git: Merge View: Take Ours", Hint: "o"},
		{ID: "git-merge-take-theirs", Title: "Git: Merge View: Take Theirs", Hint: "t"},
		{ID: "git-merge-take-both", Title: "Git: Merge View: Take Both (Ours, Then Theirs)", Hint: "b"},
		{ID: "git-merge-take-both-theirs-first", Title: "Git: Merge View: Take Both (Theirs, Then Ours)", Hint: "B"},
		{ID: "git-merge-take-base", Title: "Git: Merge View: Take Base", Hint: "a"},
		{ID: "git-merge-undo", Title: "Git: Merge View: Undo Last Change", Hint: "u"},
		{ID: "git-merge-resolve", Title: "Git: Merge View: Mark as Resolved", Hint: "r"},
		{ID: "git-merge-pane", Title: "Git: Merge View: Show Next Pane", Hint: "v"},
		{ID: "git-merge-focus", Title: "Git: Merge View: Merge Keys", Hint: "F6"},
		{ID: "git-merge-close", Title: "Git: Merge View: Close", Hint: "Esc"},
		// ── end Group G ──
		{ID: "git-rebase", Title: "Git: Interactive Rebase…"},                                  // git_rebase.go
		{ID: "git-fixup", Title: "Git: Fixup Staged Changes Into…"},                            // git_fixup.go
		{ID: "git-rebase-status", Title: "Git: Rebase in Progress (Continue / Skip / Abort)…"}, // git_rebase.go
		{ID: "git-undo", Title: "Git: Undo Last Git Action…"},                                  // git_undo.go
		{ID: "git-redo", Title: "Git: Redo Undone Git Action"},                                 // git_undo.go
		{ID: "git-reflog", Title: "Git: Reflog…"},                                              // git_undo.go
		{ID: "reload-buffer", Title: "File: Revert / Reload from Disk"},
		{ID: "open-url", Title: "View: Open URL on Current Line"},
		{ID: "pin-tab", Title: "Tab: Toggle Pinned", Hint: "Alt+P"},
		{ID: "copy-file-ref", Title: "Copy: File:Line Reference"},
		{ID: "snippet-picker", Title: "Snippets: Browse..."},
		{ID: "tabs-to-spaces", Title: "Edit: Convert Tabs to Spaces"},
		{ID: "spaces-to-tabs", Title: "Edit: Convert Spaces to Tabs"},
		{ID: "trim-blanks", Title: "Edit: Collapse Blank Lines"},
		{ID: "config-commands", Title: "Open Config: User Commands"},
		{ID: "config-theme", Title: "Open Config: Theme"},
		// Debug Adapter Protocol. F-key shortcuts (F9 / F10 / F11 / Shift+F11)
		// are exposed via the Hint column so the palette doubles as a
		// discoverability surface. Continue / Start / Stop have no shortcut.
		{ID: "debug-start", Title: "Debug: Start / Continue", Hint: "Alt+F5"}, // Group D
		{ID: "debug-stop", Title: "Debug: Stop", Hint: "Shift+Alt+F5"},        // Group D
		{ID: "debug-toggle-breakpoint", Title: "Debug: Toggle Breakpoint", Hint: "F9"},
		{ID: "debug-step-over", Title: "Debug: Step Over", Hint: "F10"},
		{ID: "debug-step-into", Title: "Debug: Step Into", Hint: "F11"},
		{ID: "debug-step-out", Title: "Debug: Step Out", Hint: "Shift+F11"},
		{ID: "debug-continue", Title: "Debug: Continue"},
		{ID: "debug-show-stack", Title: "Debug: Show Call Stack"},
		{ID: "debug-show-vars", Title: "Debug: Show Variables"},
		// Group D — Run & Debug (dap.go / debug_*.go).
		{ID: "debug-run-nodebug", Title: "Debug: Run Without Debugging"},
		{ID: "debug-select-config", Title: "Debug: Select Configuration..."},
		{ID: "debug-open-launch", Title: "Debug: Open launch.json"},
		{ID: "debug-pause", Title: "Debug: Pause"},
		{ID: "debug-restart", Title: "Debug: Restart"},
		{ID: "debug-conditional-bp", Title: "Debug: Add Conditional Breakpoint..."},
		{ID: "debug-logpoint", Title: "Debug: Add Logpoint..."},
		{ID: "debug-clear-bps", Title: "Debug: Remove All Breakpoints"},
		{ID: "debug-add-watch", Title: "Debug: Add Watch Expression..."},
		{ID: "debug-console", Title: "View: Debug Console"},
		{ID: "debug-view", Title: "View: Run and Debug"},
		// ── end Group D ──
		// LSP / DAP installer (lsp_manager.go).
		{ID: "lsp-manage", Title: "LSP: Manage Language Servers..."},
		{ID: "dap-install", Title: "DAP: Install Adapter..."},
		{ID: "open-settings-ui", Title: "Preferences: Open Settings (UI)"},
		{ID: "edit-custom-theme", Title: "Preferences: Edit Custom Theme"},
		{ID: "customize-keybindings", Title: "Preferences: Customize Keybindings"},
		// Group H — settings sync + user snippets.
		{ID: "settings-export", Title: "Preferences: Export termocode Settings File…"},
		{ID: "settings-import", Title: "Preferences: Import termocode Settings File…"},
		importPaletteItems()[0], importPaletteItems()[1], // import_editor.go
		{ID: "snippets-manage", Title: "Snippets: Manage..."},
		// ── Group E: Testing (test_explorer.go / test_runner.go) ──
		{ID: "testing-focus", Title: "Testing: Focus Test Explorer"},
		{ID: "testing-run-all", Title: "Testing: Run All Tests", Hint: "Alt+T"},
		{ID: "testing-rerun-failed", Title: "Testing: Rerun Failed Tests"},
		{ID: "testing-run-file", Title: "Testing: Run Tests in Current File"},
		{ID: "testing-run-cursor", Title: "Testing: Run Test at Cursor"},
		{ID: "testing-debug-cursor", Title: "Testing: Debug Test at Cursor"},
		{ID: "testing-stop", Title: "Testing: Stop Test Run"},
		{ID: "testing-refresh", Title: "Testing: Refresh Tests"},
		{ID: "testing-collapse", Title: "Testing: Collapse All"},
		{ID: "testing-results", Title: "View: Test Results"},
		// ── end Group E ──
		{ID: "quit", Title: "File: Quit", Hint: "Ctrl+Q"},
	}
	items = append(items, servicesPaletteItems()...)  // services_actions.go
	items = append(items, kubePaletteItems()...)      // kube_actions.go
	items = append(items, worktreePaletteItems()...)  // worktrees_keys.go
	items = append(items, databasesPaletteItems()...) // DB-3
	items = append(items, extManagePaletteItems()...) // Group I — Extensions: Reload / Open Folder / …
	items = append(items, httpPaletteItems()...)      // HTTP client (http_commands.go)
	items = append(items, secretsPaletteItems()...)   // secret store (secrets_ui.go)
	items = append(items, mcpImportOtherPaletteItems()...)
	items = append(items, mcpServePaletteItems()...)       // termocode as an MCP server (mcp_serve_status.go)
	items = append(items, webPaletteItems()...)            // the web panel (web_tui.go)
	items = append(items, webAccessPaletteItems()...)      // pairing and devices (web_access.go)
	items = append(items, itermocPaletteItems()...)        // the Itermoc view (itermoc_actions.go)
	items = append(items, workflowsPaletteItems()...)      // WFX-7 (workflows_keys.go)
	items = append(items, ciPaletteItems()...)             // G-CI (ci_card.go)
	items = append(items, layoutPaletteItems()...)         // G-LAY (layout_palette.go)
	items = append(items, updatePaletteItems()...)         // G-UPD (update_palette.go)
	items = append(items, prPaletteItems()...)             // G-PR (pr_list.go)
	items = append(items, termPanePaletteItems()...)       // terminal splits (terminal_split.go)
	items = append(items, gitxPaletteItems()...)           // GITX features (gitx_hub.go)
	items = append(items, codebaseSearchPaletteItems()...) // CMP-69 (codebase_search.go)
	// Group A — AI commands (ai_state.go).
	for _, e := range aiPaletteItems() {
		items = append(items, picker.Item{ID: e.id, Title: e.title, Hint: e.hint})
	}
	return items
}

// dispatchPaletteAction performs the action selected in the palette.
// Returns an optional tea.Cmd.
func (m *Model) dispatchPaletteAction(id string) tea.Cmd {
	// Record this action in the recent-palette list so next time it floats
	// to the top. Best-effort; failures don't block the dispatch.
	pushPaletteRecent(id)
	if debugPaletteIDs[id] { // Group D
		return m.runDebugPaletteCmd(id)
	}
	if cmd, ok := m.runTermPaneAction(id); ok { // terminal splits (terminal_split.go)
		return cmd
	}
	switch id {
	case "save": // git status / markers refresh as after Ctrl+S
		if err := m.editor.Save(); err != nil {
			text := saveFailText(m.editor.Path(), err)
			m.err = text
			var toastCmd tea.Cmd
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Save failed", text)
			return tea.Batch(fetchGitCmd(), toastCmd)
		}
		return fetchGitCmd()
	case "save-all":
		return tea.Batch(fetchGitCmd(), m.saveAll())
	case "close-buffer":
		if m.activeBuf != 0 && m.promptCloseBufferIfDirty(m.activeBuf) {
			return nil
		}
		if buf, ok := m.findBuffer(m.activeBuf); ok {
			m.pushClosed(buf.Path)
		}
		if m.nvim != nil {
			_ = m.nvim.Command("bdelete")
		}
	case "reopen-closed":
		return m.reopenClosed()
	case "clipboard-history":
		return m.openClipboardPicker()
	case "next-buffer":
		if m.nvim != nil {
			_ = m.nvim.Command("bnext")
		}
	case "prev-buffer":
		if m.nvim != nil {
			_ = m.nvim.Command("bprevious")
		}
	case "toggle-explorer":
		m.showExp = !m.showExp
		m.applyLayout()
	case "toggle-actions-panel":
		m.toggleActionsPanel()
		m.applyLayout()
	case "focus-explorer":
		m.focus = FocusExplorer
	case "focus-editor":
		m.focus = FocusEditor
	case "reveal-file":
		m.revealActiveFileInExplorer()
	case "open-shell":
		return openShellCmd()
	case "toggle-terminal":
		m.toggleTerminalPanel()
		m.applyLayout()
	case "terminal-new-tab":
		m.newTerminalTab()
		m.applyLayout()
		m.resizeTerminalSplit()
	case "terminal-close-tab":
		m.closeActivePanelTab()
	case "terminal-next-tab":
		if m.termOpen {
			m.cycleTerminalTab(1)
		}
	case "terminal-prev-tab":
		if m.termOpen {
			m.cycleTerminalTab(-1)
		}
	case "terminal-maximize":
		if m.termOpen {
			m.maximizeTerminalPanel()
		}
	case "terminal-minimize":
		if m.termOpen {
			m.minimizeTerminalPanel()
		}
	// ── Bottom panel tabs (panel.go / output.go) ──
	case "view-output":
		m.showPanelTab(panelKindOutput)
	case "output-clear":
		m.output.Clear(m.output.Active())
	case "quick-open":
		m.picker = picker.NewWith(" Go to file ", func() []picker.Item { return loadFiles() })
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindFiles
		return m.picker.Init()
	case "duplicate-line":
		// Forward to nvim's <M-S-Down> binding (defined in lsp_lua.go), which
		// duplicates the current line in normal/insert/visual modes.
		if m.nvim != nil {
			_ = m.nvim.Input("<M-S-Down>")
		}
	case "toggle-comment":
		m.toggleLineComment()
	case "fold-all":
		if m.nvim != nil {
			_ = m.nvim.Command("set foldlevel=0")
		}
	case "unfold-all":
		if m.nvim != nil {
			_ = m.nvim.Command("set foldlevel=99")
		}
	case "toggle-fold":
		// `za` is the vim-native "toggle fold under cursor" normal-mode action.
		// Use Input with a normal-mode escape so it works regardless of which
		// mode the user is currently in (insert vs normal).
		if m.nvim != nil {
			_ = m.nvim.Command("normal! za")
		}
	case "preview-md":
		return m.openPreviewCmd()
	case "find-files":
		m.openFindInFilesPrompt()
		return nil
	case "find-files-overlay":
		m.search = search.New()
		m.search.SetSize(m.w, m.h)
		m.search.SetRoots(m.explorer.Roots())
		if fsys, ok := m.remoteFS(); ok {
			// A container workspace (FIX-3): its one root, in the container.
			m.search.SetRoots([]string{m.workspace().Root})
			m.search.SetFS(fsys)
		}
		m.search.SetMaxResults(searchMaxResults(loadSettings()))
		m.searchOpen = true
	case "replace-in-file":
		// Unified findbar in expanded (Replace) mode — mirrors Ctrl+H.
		if !m.findOpen {
			m.find = findbar.New()
			m.findOpen = true
		}
		m.find.EnableReplace(true)
		m.applyLayout()
	case "toggle-inlay-hints":
		m.toggleInlayHints()
	case "search-next-result":
		return m.findResultNav(1)
	case "search-prev-result":
		return m.findResultNav(-1)
	case "pick-theme":
		return m.openThemePicker()
	case "git-stage":
		return m.gitStageActiveFile()
	case "git-stage-all":
		return m.gitStageAll()
	case "git-stage-hunk":
		return m.gitStageHunk()
	case "git-unstage-hunk":
		return m.gitUnstageHunk()
	case "git-discard-hunk":
		return m.gitDiscardHunk()
	case "git-stage-lines":
		return m.gitStageLines()
	case "git-unstage-lines":
		return m.gitUnstageLines()
	case "git-discard-lines":
		return m.gitDiscardLines()
	case "git-commit":
		m.openCommitPrompt()
	case "git-diff":
		return m.gitDiffActiveFile()
	case "git-discard":
		m.openGitDiscardForActiveFile()
	case "git-refresh":
		return fetchGitCmd()
	case "git-view-toggle":
		m.toggleGitViewMode()
	case "git-focus":
		m.activity.SetActive(activity.ViewGit)
		m.showExp = true
		m.focus = FocusExplorer
		m.applyLayout()
	case "git-merge-branch":
		return m.openMergeBranchPicker()
	case "git-merge-status":
		return m.openGitOpControl()
	case "git-unstage-all":
		return m.gitUnstageAll()
	case "git-discard-all":
		return m.openGitDiscardAllCard()
	case "git-clean-untracked":
		return m.openGitCleanUntrackedCard()
	case "git-commit-push":
		return m.gitInlineCommitThen(false, false, gitAfterPush)
	case "git-commit-sync":
		return m.gitInlineCommitThen(false, false, gitAfterSync)
	case "git-push":
		return m.gitPush()
	case "git-pull":
		return m.gitPull()
	case "git-fetch":
		return m.gitFetch()
	case "git-sync":
		return m.gitSync()
	case "git-branch":
		return m.openBranchPicker()
	case "git-branch-rename":
		return m.openRenameBranchPrompt()
	case "git-branch-delete":
		return m.openDeleteBranchPicker()
	case "git-branch-from":
		return m.openBranchFromPicker()
	case "problems":
		return m.openProblemsPicker()
	case "code-actions":
		return m.fetchCodeActionsCmd()
	case "code-action-apply-preferred":
		if m.preferredCodeAction != nil {
			return m.applyPreferredCodeActionFromCache()
		}
		return m.fetchCodeActionsCmd()
	case "goto-symbol-file":
		return m.fetchSymbolsCmd()
	case "goto-symbol-workspace":
		return m.fetchWorkspaceSymbolsCmd()
	case "bookmark-toggle":
		return m.toggleBookmark()
	case "bookmark-list":
		return m.fetchBookmarksCmd()
	case "bookmark-clear":
		return m.clearAllBookmarks()
	case "goto-line":
		m.openGotoLinePrompt()
	case "split-vertical":
		if m.nvim != nil {
			_ = m.nvim.Command("vsplit")
		}
	case "split-horizontal":
		if m.nvim != nil {
			_ = m.nvim.Command("split")
		}
	case "close-split":
		if m.nvim != nil {
			_ = m.nvim.Command("close")
		}
	case "format-document":
		return m.formatDocumentCmd()
	case "open-recent":
		return m.openRecentFilePicker()
	case "open-workspace":
		return m.openWorkspacePicker()
	case "workspace-add-folder":
		m.openAddRootPrompt()
	case "workspace-remove-folder":
		return m.openRemoveRootPicker()
	case "workspace-open-container": // container_workspace.go
		return m.openFolderInContainerPalette()
	case "workspace-reopen-devcontainer": // devcontainer_open.go
		return m.openDevContainerReopen()
	case "toggle-wrap":
		return m.toggleWordWrap()
	case "trim-whitespace":
		return m.trimTrailingWhitespace()
	case "reveal-fs":
		return m.revealInFileSystem()
	case "transfer-download":
		return m.downloadPath(m.xferDownloadTarget())
	case "transfer-upload":
		return m.uploadTo(m.xferUploadTarget())
	case "transfer-cancel":
		return m.cancelDownload()
	case "transfer-reveal":
		return m.revealLast()
	case "diff-files":
		return m.openDiffFilesFlow()
	case "git-stash":
		return m.gitStash()
	case "git-stash-pop":
		return m.gitStashPop()
	case "git-stash-list":
		return m.openStashPicker()
	case "sort-asc":
		return m.sortLinesAsc()
	case "sort-desc":
		return m.sortLinesDesc()
	case "reverse-lines":
		return m.reverseLines()
	case "uppercase":
		return m.uppercaseSelection()
	case "lowercase":
		return m.lowercaseSelection()
	case "join-lines":
		return m.joinLines()
	case "alternate-file":
		m.alternateFile()
	case "next-diagnostic":
		m.gotoNextDiagnostic()
	case "prev-diagnostic":
		m.gotoPrevDiagnostic()
	case "explorer-toggle-preview":
		return m.toggleExplorerPreview()
	case "explorer-rename-selected":
		m.renameExplorerSelection()
	case "explorer-undo-rename":
		return m.undoBulkRename(m.bulkRename.serial)
	case "toggle-hidden":
		return m.toggleHiddenFiles()
	case "toggle-key-hints":
		return m.toggleKeyHints()
	case "cheat-sheet":
		m.openCheatSheet()
	case "buffer-info":
		return m.openBufferInfo()
	case "error-log":
		return m.openErrorLog()
	case "run-doctor":
		return runDoctorCmd()
	case "setup-show-card":
		return m.showSetupCard()
	case "open-docs": // Docs
		return m.openDocs()
	case "help-guide":
		m.openHelpGuidePicker()
	case "help-context":
		return m.openContextHelp()
	case "hover":
		return m.hoverCmd()
	case "find-references":
		return m.fetchReferencesCmd()
	case "replace-workspace":
		m.openReplaceInWorkspacePrompt()
	case "goto-type-def":
		return m.gotoTypeDefCmd()
	case "goto-impl":
		return m.gotoImplementationCmd()
	case "run-tests":
		return m.runTestsCmd()
	case "git-log":
		return m.gitLogPicker()
	case "git-compare-rev":
		m.openCompareWithRevisionPrompt()
	case "git-blame":
		return m.gitBlameCurrentLine()
	case "git-tag-create":
		return m.openCreateTagPrompt("", "")
	case "git-tag-delete":
		return m.openTagPickerFor(pickerKindGitTagDelete, " Delete which tag? ")
	case "git-tag-push":
		return m.openTagPickerFor(pickerKindGitTagPush, " Push which tag? ", picker.Item{ID: "tags-all", Title: "All tags"})
	case "git-tag-delete-remote":
		return m.openTagPickerFor(pickerKindGitTagDeleteRemote, " Delete which tag on the remote? ")
	case "git-tag-details":
		return m.openTagPickerFor(pickerKindGitTagDetails, " Tag details ")
	case "git-remotes":
		return m.openRemotesPicker()
	case "git-upstream-set":
		return m.openUpstreamPicker()
	case "git-upstream-unset":
		return m.gitUnsetUpstream()
	case "git-push-force":
		return m.openForcePushCard()
	case "git-commit-history":
		return m.openCommitHistoryPicker()
	case "git-commit-edit":
		return m.gitOpenCommitEditor()
	case "git-co-author":
		return m.openCoAuthorPicker()
	case "git-ai-commit-msg":
		return m.gitAIAsk("commit", true)
	case "git-ai-stash":
		return m.gitAIAsk("stash", false)
	case "git-ai-explain-changes":
		return m.gitAIExplainChanges()
	case "git-ai-explain-line":
		return m.gitAIExplainLineCommit()
	case "git-ai-pr-text":
		return m.gitAIPRText()
	case "git-stash-options":
		return m.openStashModePicker()
	case "git-stash-actions":
		return m.openStashActionsPicker()
	case "git-uncommit":
		return m.gitUncommit()
	case "git-inline-blame":
		return m.toggleInlineBlame()
	case "git-status-blame":
		return m.toggleStatusBlame()
	case "git-blame-annotate":
		return m.toggleBlameAnnotate()
	case "git-blame-options":
		return m.openBlameOptionsPicker()
	case "git-blame-commit":
		return m.gitOpenLineCommit()
	case "git-blame-before":
		return m.gitBlameBefore()
	case "git-line-lens":
		return m.toggleLineLens()
	case "git-block-history":
		return m.gitBlockHistoryPicker()
	case "git-line-history":
		return m.gitLineHistoryPicker()
	case "git-rev-prev":
		return m.gitStepRevision(-1)
	case "git-rev-next":
		return m.gitStepRevision(1)
	case "git-history":
		return m.gitFileHistoryPicker()
	case "git-tags":
		return m.gitTagPicker()
	case "zen-mode":
		return m.toggleZenMode()
	case "reload-window":
		return m.reloadWindow()
	case "detach-session":
		return m.detachSession()
	case "reset-session":
		return m.resetSession()
	case "toggle-auto-save":
		return m.toggleAutoSave()
	case "next-hunk":
		return m.gotoNextHunk()
	case "prev-hunk":
		return m.gotoPrevHunk()
	case "reload-buffer":
		return m.reloadBuffer()
	case "open-url":
		return m.openURLUnderCursor()
	case "pin-tab":
		return m.togglePinActiveTab()
	case "copy-file-ref":
		return m.copyFileLineRef()
	case "snippet-picker":
		return m.openSnippetPicker()
	case "tabs-to-spaces":
		return m.tabsToSpaces()
	case "spaces-to-tabs":
		return m.spacesToTabs()
	case "trim-blanks":
		return m.trimBlankLines()
	case "config-commands":
		return m.editConfigFile("commands.json", defaultCommandsJSON)
	case "config-theme":
		return m.editConfigFile("config.json", defaultThemeConfigJSON)
	case "debug-start":
		return m.dapStart()
	case "debug-stop":
		return m.dapStop()
	case "debug-toggle-breakpoint":
		return m.dapToggleBreakpoint()
	case "debug-step-over":
		return m.dapStepOver()
	case "debug-step-into":
		return m.dapStepInto()
	case "debug-step-out":
		return m.dapStepOut()
	case "debug-continue":
		return m.dapContinue()
	case "debug-show-stack":
		return m.dapShowStack()
	case "debug-show-vars":
		return m.dapShowVars()
	case "lsp-manage":
		return m.openToolManager(lspinstall.CategoryLSP)
	case "dap-install":
		return m.openToolManager(lspinstall.CategoryDAP)
	case "open-settings-ui":
		return m.openSettingsPicker()
	case "edit-custom-theme":
		return m.openThemeEditor()
	case "customize-keybindings":
		return m.openKeybindingPicker()
	// Group H — settings sync + user snippets.
	case "settings-export":
		return m.openSettingsExportPrompt()
	case "settings-import":
		return m.openSettingsImportPrompt()
	case "snippets-manage":
		return m.openSnippetManager()
	default:
		if cmd, ok := m.dispatchGitxPalette(id); ok { // gitx_hub.go
			return cmd
		}
		if cmd, ok := m.dispatchCodebaseSearchPalette(id); ok { // CMP-69 (codebase_search.go)
			return cmd
		}
		if cmd, ok := m.dispatchImportPalette(id); ok { // import_editor.go
			return cmd
		}
		if cmd, ok := m.dispatchAIPalette(id); ok { // Group A
			return cmd
		}
		if cmd, ok := m.dispatchHTTPPalette(id); ok { // HTTP client
			return cmd
		}
		if cmd, ok := m.dispatchSecretsPalette(id); ok { // secrets_ui.go
			return cmd
		}
		if cmd, ok := m.dispatchMCPImportOtherPalette(id); ok { // mcp_import_other.go
			return cmd
		}
		if cmd, ok := m.dispatchMCPServePalette(id); ok { // mcp_serve_status.go
			return cmd
		}
		if cmd, ok := m.dispatchWebAccessPalette(id); ok { // web_access.go
			return cmd
		}
		if cmd, ok := m.dispatchWebPalette(id); ok { // web_tui.go
			return cmd
		}
		if cmd, ok := m.dispatchItermocPalette(id); ok { // itermoc_actions.go
			return cmd
		}
		if cmd, ok := m.dispatchWorkflowsPalette(id); ok { // workflows_keys.go
			return cmd
		}
		if cmd, ok := m.dispatchCIPalette(id); ok { // ci_card.go
			return cmd
		}
		if cmd, ok := m.dispatchLayoutPalette(id); ok { // layout_palette.go
			return cmd
		}
		if cmd, ok := m.dispatchUpdatePalette(id); ok { // update_palette.go
			return cmd
		}
		if cmd, ok := m.dispatchPRPalette(id); ok { // pr_list.go
			return cmd
		}
		// Group G: merge conflicts + clone (git_conflicts.go, git_clone.go).
		if cmd, ok := m.dispatchGitExtrasPalette(id); ok {
			return cmd
		}
		if cmd, ok := m.dispatchRebasePalette(id); ok { // git_rebase.go
			return cmd
		}
		if cmd, ok := m.dispatchUndoPalette(id); ok { // git_undo.go
			return cmd
		}
		if cmd, ok := m.dispatchMergePalette(id); ok { // git_merge_view.go
			return cmd
		}
		// Group C: tasks + problems panel (tasks_run.go).
		if cmd, ok := m.dispatchTasksPalette(id); ok {
			return cmd
		}
		// Group E: Testing (test_explorer.go).
		if cmd, ok := m.dispatchTestingPalette(id); ok {
			return cmd
		}
		if cmd, ok := m.dispatchServicesPalette(id); ok { // services_actions.go
			return cmd
		}
		if cmd, ok := m.dispatchKubePalette(id); ok { // kube_actions.go
			return cmd
		}
		if cmd, ok := m.dispatchWorktreePalette(id); ok { // worktrees_keys.go
			return cmd
		}
		if cmd, ok := m.dispatchDatabasesPalette(id); ok { // db_actions.go
			return cmd
		}
		// Group I: "Ext: …" commands + "Extensions: …" management (ext.go).
		if cmd, ok := m.dispatchExtPalette(id); ok {
			return cmd
		}
		// User commands have IDs prefixed with "user-".
		if strings.HasPrefix(id, "user-") {
			return m.runUserCommand(id)
		}
	case "quit":
		return m.quit()
	}
	return nil
}

// applyTheme switches the runtime palette and composite styles to the given
// theme ID and persists the choice. Persistence is best-effort: a write
// failure surfaces in m.err but does not block the swap (the user still sees
// the new colors for the rest of the session). Components that read directly
// from the package-level palette vars (statusbar, explorer, tabbar, …) pick
// up the new colors on their next render automatically.
func (m *Model) applyTheme(id string) {
	t := m.applyThemeLive(id)
	if err := theme.SaveThemeID(t.ID); err != nil {
		m.err = "theme: " + err.Error()
	}
}

// applyThemeLive switches the colours without saving the choice (an
// import has already written config.json; import_apply.go).
func (m *Model) applyThemeLive(id string) theme.NamedTheme {
	t, _ := theme.ApplyTheme(id)
	m.themeID = t.ID
	m.applyNvimTheme()            // editor colours follow the chrome
	m.refreshConflictHighlights() // Group G: conflict tints follow the theme
	m.refreshAIHighlights()       // Group A: ghost text recolors live
	return t
}

// openClipboardPicker pops a sub-picker listing entries in the in-memory
// clipboard ring. Selection pastes the chosen entry at the editor cursor.
func (m *Model) openClipboardPicker() tea.Cmd {
	entries := m.clipboardPickerItems()
	if len(entries) == 0 {
		return nil
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, picker.Item{ID: fmt.Sprintf("clip-%d", e.Index), Title: e.Preview})
	}
	m.picker = picker.NewItems(" Clipboard History ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindClipboard
	return m.picker.Init()
}

// openThemePicker pops a sub-picker listing every built-in theme. Selection is
// dispatched in handlePickerSelect via pickerKindTheme. Each Item.ID is the
// theme's stable persistent identifier; Item.Hint shows " ✓" on the active
// theme so the user can see what's currently applied without reading the
// config file.
func (m *Model) openThemePicker() tea.Cmd {
	themes := theme.AvailableThemes()
	active := theme.LoadThemeID()
	if active == "" {
		active = "vscode-dark-plus"
	}
	items := make([]picker.Item, 0, len(themes))
	for _, t := range themes {
		hint := ""
		if t.ID == active {
			hint = "active"
		}
		items = append(items, picker.Item{ID: t.ID, Title: t.Name, Hint: hint})
	}
	m.picker = picker.NewItems(" Color Theme ", items)
	m.picker.SetSize(m.w, m.h)
	m.picker.SetCursorID(active)
	m.pickerOpen = true
	m.pickerKind = pickerKindTheme
	// Live preview: moving through the list applies each theme (not
	// saved); Esc restores this one.
	m.themePreviewFrom = m.themeID
	m.themePreviewing = true
	return m.picker.Init()
}
