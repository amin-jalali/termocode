package keymap

import (
	"sort"
	"strconv"
	"strings"
)

// Docs — user-facing descriptions for every Action, read by cmd/docgen to
// build docs/reference/keys.md. Keep one entry per Action: the docgen test
// and TestEveryActionDocumented fail when an Action has no entry here, so
// a new key can never ship undocumented.

// ActionInfo is the documentation for one Action.
type ActionInfo struct {
	// Area groups the action in the reference ("Editor", "Git", …).
	Area string
	// Description is one short sentence for the reference table.
	Description string
}

// Areas lists the documentation areas in display order.
var Areas = []string{
	"File", "Editor", "Navigation", "Search", "LSP", "Git", "Debug",
	"Tests", "Tasks", "AI", "Terminal", "Panels", "View", "Preferences",
	"Help", "Developer",
}

var actionInfo = map[Action]ActionInfo{
	ActionQuit:           {"File", "Quit termocode."},
	ActionSave:           {"File", "Save the active file."},
	ActionSaveAll:        {"File", "Save every modified file."},
	ActionOpenRecent:     {"File", "Pick a recently opened file."},
	ActionToggleAutoSave: {"File", "Turn auto save on or off."},
	ActionReloadBuffer:   {"File", "Reload the active file from disk."},

	ActionCopy:                     {"Editor", "Copy the selection to the clipboard."},
	ActionCut:                      {"Editor", "Cut the selection to the clipboard."},
	ActionPaste:                    {"Editor", "Paste from the clipboard."},
	ActionClipboardHistory:         {"Editor", "Pick an entry from the clipboard history and paste it."},
	ActionCloseBuffer:              {"Editor", "Close the active editor tab."},
	ActionReopenClosed:             {"Editor", "Reopen the editor you closed last."},
	ActionDuplicateLine:            {"Editor", "Duplicate the current line."},
	ActionToggleComment:            {"Editor", "Comment or uncomment the current line or selection."},
	ActionFormatDocument:           {"Editor", "Format the active file with its language server."},
	ActionPinTab:                   {"Editor", "Pin or unpin the active tab."},
	ActionCopyFileRef:              {"Editor", "Copy a file:line reference for the cursor position."},
	ActionShowSnippetPicker:        {"Editor", "Browse and insert a snippet."},
	ActionOpenURL:                  {"Editor", "Open the first URL on the current line in the browser."},
	ActionApplyPreferredCodeAction: {"LSP", "Apply the preferred quick fix at the cursor without a picker."},

	ActionQuickOpen:        {"Navigation", "Open the Go to File picker."},
	ActionCommandPalette:   {"Navigation", "Open the command palette."},
	ActionNextBuffer:       {"Navigation", "Go to the next editor tab."},
	ActionPrevBuffer:       {"Navigation", "Go to the previous editor tab."},
	ActionAlternateFile:    {"Navigation", "Switch between the two most recent files."},
	ActionGotoLine:         {"Navigation", "Jump to a line number."},
	ActionGotoSymbolInFile: {"Navigation", "Jump to a symbol in the active file."},
	ActionRevealFile:       {"Navigation", "Show the active file in the explorer."},
	ActionToggleBookmark:   {"Navigation", "Toggle a bookmark on the current line."},
	ActionShowBookmarks:    {"Navigation", "List all bookmarks."},
	ActionFocusExplorer:    {"Navigation", "Move focus to the sidebar."},
	ActionFocusEditor:      {"Navigation", "Move focus to the editor."},
	ActionFocusSwap:        {"Navigation", "Switch focus between the sidebar and the editor."},

	ActionFind:               {"Search", "Find in the active file."},
	ActionReplaceInFile:      {"Search", "Find and replace in the active file."},
	ActionWorkspaceSearch:    {"Search", "Search all files in the workspace (ripgrep)."},
	ActionReplaceInWorkspace: {"Search", "Replace text in all files of the workspace."},

	ActionHover:              {"LSP", "Show hover documentation for the symbol at the cursor."},
	ActionFindReferences:     {"LSP", "List all references to the symbol at the cursor."},
	ActionGotoTypeDef:        {"LSP", "Go to the type definition of the symbol at the cursor."},
	ActionGotoImplementation: {"LSP", "Go to the implementation of the symbol at the cursor."},
	ActionCodeActions:        {"LSP", "Show quick fixes and code actions at the cursor."},
	ActionToggleInlayHints:   {"LSP", "Show or hide inlay hints."},
	ActionNextDiagnostic:     {"LSP", "Go to the next problem in the active file."},
	ActionPrevDiagnostic:     {"LSP", "Go to the previous problem in the active file."},

	ActionGitBlameLine: {"Git", "Show who last changed the current line."},
	ActionFileHistory:  {"Git", "Show the commit history of the active file."},
	ActionNextHunk:     {"Git", "Go to the next changed block (hunk)."},
	ActionPrevHunk:     {"Git", "Go to the previous changed block (hunk)."},
	ActionStageHunk:    {"Git", "Stage the hunk at the cursor."},
	ActionUnstageHunk:  {"Git", "Unstage the hunk at the cursor."},
	ActionDiscardHunk:  {"Git", "Discard the hunk at the cursor."},

	ActionDebugToggleBreakpoint: {"Debug", "Toggle a breakpoint on the current line."},
	ActionDebugStepOver:         {"Debug", "Step over the current line."},
	ActionDebugStepInto:         {"Debug", "Step into the function call."},
	ActionDebugStepOut:          {"Debug", "Step out of the current function."},
	ActionDebugStartContinue:    {"Debug", "Start debugging, or continue a paused session."},
	ActionDebugStop:             {"Debug", "Stop the debug session."},

	ActionRunTests: {"Tests", "Run all tests of the project."},

	ActionRunTask:   {"Tasks", "Pick a task and run it."},
	ActionRerunTask: {"Tasks", "Run the last task again."},

	ActionAITriggerInline: {"AI", "Ask the AI for an inline completion now."},
	ActionAIEditSelection: {"AI", "Edit the selection (or line) with an AI instruction."},
	ActionAIToggleInline:  {"AI", "Turn automatic AI ghost text on or off."},

	ActionToggleTerminal:   {"Terminal", "Show or hide the integrated terminal panel."},
	ActionOpenShell:        {"Terminal", "Open an external shell in the workspace folder."},
	ActionTerminalNewTab:   {"Terminal", "Open a new terminal tab."},
	ActionTerminalCloseTab: {"Terminal", "Close the active terminal tab."},
	ActionTerminalNextTab:  {"Terminal", "Go to the next terminal tab."},
	ActionTerminalPrevTab:  {"Terminal", "Go to the previous terminal tab."},

	ActionToggleExplorer:     {"Panels", "Show or hide the sidebar."},
	ActionToggleActionsPanel: {"Panels", "Show or hide the right-side Actions / AI chat panel."},
	ActionShowProblems:       {"Panels", "Open the Problems panel."},
	ActionToggleHidden:       {"Panels", "Show or hide hidden files in the explorer."},

	ActionSplitVertical:   {"View", "Split the editor to the right."},
	ActionSplitHorizontal: {"View", "Split the editor down."},
	ActionCloseSplit:      {"View", "Close the active split."},
	ActionToggleWordWrap:  {"View", "Turn soft word wrap on or off."},
	ActionToggleZenMode:   {"View", "Turn Zen mode on or off."},
	ActionMarkdownPreview: {"View", "Open the Markdown preview for the active file."},
	ActionMdLivePreview:   {"View", "Old Markdown live preview. Not bound and does nothing; kept so old keymap.json files still load."},
	ActionShowBufferInfo:  {"View", "Show path, size, encoding and LSP info for the active file."},

	ActionPickTheme: {"Preferences", "Pick a color theme."},

	ActionShowCheatSheet: {"Help", "Show the keyboard shortcuts overlay."},

	ActionReloadWindow: {"Developer", "Reload the window (restart the UI, keep the session)."},
}

// Describe returns the documentation for a. The zero ActionInfo means
// the action is not documented yet.
func Describe(a Action) ActionInfo {
	return actionInfo[a]
}

// HumanKey turns a Bubble Tea key string ("ctrl+shift+p", "alt+f5") into
// the name a user reads on the keyboard ("Ctrl+Shift+P", "Alt+F5").
// Legacy terminal spellings get the key the user actually presses:
// "alt+T" is Alt+Shift+T, "f16" is Shift+F4 (xterm), "f23" is Shift+F11
// (xterm), "alt+f17" is Shift+Alt+F5 (xterm), "ctrl+_" is Ctrl+/.
func HumanKey(s string) string {
	switch s {
	case "f13", "f14", "f15", "f16", "f17", "f18", "f19", "f20", "f21", "f22", "f23", "f24":
		return "Shift+" + HumanKey(shiftedF(s))
	case "alt+f13", "alt+f14", "alt+f15", "alt+f16", "alt+f17", "alt+f18", "alt+f19", "alt+f20", "alt+f21", "alt+f22", "alt+f23", "alt+f24":
		return "Shift+Alt+" + HumanKey(shiftedF(strings.TrimPrefix(s, "alt+")))
	case "ctrl+_":
		return "Ctrl+/"
	}
	parts := strings.Split(s, "+")
	// A trailing "+" key ("ctrl++") splits into an empty last part.
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = append(parts[:len(parts)-2], "+")
	}
	out := make([]string, 0, len(parts)+1)
	last := parts[len(parts)-1]
	shift := false
	for _, p := range parts[:len(parts)-1] {
		if p == "shift" {
			shift = true
		}
		out = append(out, humanPart(p))
	}
	// "alt+T": an upper-case letter means Shift is held.
	if len(last) == 1 && last[0] >= 'A' && last[0] <= 'Z' && !shift {
		out = append(out, "Shift")
	}
	out = append(out, humanPart(last))
	return strings.Join(out, "+")
}

// shiftedF maps xterm's extra function keys back to Shift+Fn
// ("f16" → "f4": F13..F24 are Shift+F1..F12).
func shiftedF(s string) string {
	n := 0
	for _, c := range strings.TrimPrefix(s, "f") {
		n = n*10 + int(c-'0')
	}
	return "f" + strconv.Itoa(n-12)
}

var humanNames = map[string]string{
	"ctrl": "Ctrl", "alt": "Alt", "shift": "Shift",
	"pgup": "PgUp", "pgdown": "PgDn", "enter": "Enter", "tab": "Tab",
	"esc": "Esc", "space": "Space", "up": "Up", "down": "Down",
	"left": "Left", "right": "Right", "home": "Home", "end": "End",
	"delete": "Delete", "backspace": "Backspace",
}

func humanPart(p string) string {
	if n, ok := humanNames[p]; ok {
		return n
	}
	if len(p) > 1 && p[0] == 'f' && p[1] >= '0' && p[1] <= '9' {
		return "F" + p[1:]
	}
	return strings.ToUpper(p)
}

// KeysFor returns every key string bound to a in k, sorted.
func (k KeyMap) KeysFor(a Action) []string {
	var out []string
	for key, act := range k.bindings {
		if act == a {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
