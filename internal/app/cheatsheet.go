package app

import "termocode/internal/preview"

// openCheatSheet opens a non-interactive preview overlay listing every
// keyboard shortcut and the most useful palette commands. Closes with
// Esc / q like any other preview. Built statically here so it stays in
// sync with keymap.go (no runtime introspection — that would force us
// to surface internal action names which aren't user-meaningful).
//
// Anything reachable only via the palette (no default keybinding) is
// tagged "Palette". The rendered overlay is scrollable so this list can
// grow without worrying about a single-screen cap.
func (m *Model) openCheatSheet() {
	body := `
FILE
  Ctrl+S            Save
  Ctrl+Q            Quit
  Ctrl+W            Close current editor
  Alt+Shift+T       Reopen closed editor
  Ctrl+R            Open Recent file
  Palette           Save All Files
  Palette           Open Recent Workspace
  Palette           Reveal in File System
  Palette           Compare Two Files
  Palette           Revert / Reload from Disk
  Palette           Toggle Auto Save

EDIT
  Ctrl+C / X / V    Copy / Cut / Paste
  Ctrl+Shift+V      Clipboard history
  Ctrl+Z / Y        Undo / Redo
  Ctrl+/            Toggle line comment
  Alt+up/dn         Move line
  Shift+Alt+up/dn   Duplicate line
  Ctrl+D            Multi-cursor: select next occurrence
  Ctrl+Alt+up/dn    Add cursor above / below
  Shift+Alt+F       Format document
  Ctrl+. / Alt+Ent  Quick fix (code actions)
  Palette           Trim Trailing Whitespace
  Palette           Collapse Blank Lines
  Palette           Convert Tabs <-> Spaces
  Palette           Sort Lines (Asc / Desc) / Reverse
  Palette           UPPERCASE / lowercase
  Palette           Join Lines

SEARCH
  Ctrl+F            Find in current file
  Ctrl+H            Replace in current file
  F8 / Alt+F        Find in workspace (ripgrep)
  Ctrl+Shift+H      Replace in workspace
  Find/Replace bar  Aa case · ab word · .* regex toggles
                    (selection is auto-prefilled when opened)

NAVIGATION
  Ctrl+P            Quick open file
  F1                Command Palette
  Ctrl+B            Toggle file explorer
  F6                Switch focus (sidebar <-> editor)
  Ctrl+Shift+E      Reveal active file in explorer
  Ctrl+Tab          Alternate file (last two)
  Ctrl+PgDn / PgUp  Next / previous editor tab
  Ctrl+G            Go to line
  Ctrl+Shift+O      Go to symbol in file
  Palette           Go to Symbol in Workspace
  F3 / Shift+F3     Toggle bookmark / show all
  Palette           Bookmarks: Clear All
  Alt+P             Pin / unpin current tab

LSP
  F12               Go to definition
  Ctrl+F12          Go to type definition
  Palette           Go to Implementation
  Shift+F12         Find references
  K / Palette       Hover documentation
  F2                Rename symbol
  Ctrl+Space        Manual completion
  F4                Toggle inlay hints
  F5 / Shift+F5     Next / previous diagnostic
  Palette           View: Problems

VIEW
  Alt+Z             Toggle Zen mode
  Alt+W             Toggle word wrap
  Ctrl+\            Split editor right
  Palette           Editor: Split Down / Close Split
  Palette           Fold All / Unfold All / Toggle Fold
  Ctrl+Alt+I        Show buffer info
  Palette           View: Open URL on Current Line
  Palette           Files: Toggle Hidden Files
  Palette           Help: Show Shortcuts (this overlay)
  Palette           Help: Show Error Log

GIT
  Alt+] / Alt+[     Next / previous change (hunk)
  Alt+B             Blame current line
  Palette           Git: Stage Current File / Stage All
  Palette           Git: Commit / Discard / Refresh
  Palette           Git: Push / Pull / Switch Branch
  Palette           Git: Show Log / File History / Tags
  Palette           Git: Compare with Revision
  Palette           Git: Stash / Pop Stash / Show Stash List
  Palette           Git: Show Diff for Current File
  Palette           Git: Focus Source Control

TERMINAL & RUN
  Ctrl+T            Toggle integrated terminal panel
  Ctrl+` + "`" + ` / Alt+`   + "`" + `    Alternate terminal toggles (best effort)
  Palette           Terminal: Open External Shell
  Alt+T             Run project tests
  F7                Markdown preview (.md files only)

DEBUG
  F9                Toggle breakpoint
  F10               Step over
  F11 / Shift+F11   Step into / out (terminal-dependent)
  Palette           Debug: Start / Stop / Continue
  Palette           Debug: Show Call Stack / Variables

WORKSPACE
  Palette           Workspace: Add Folder
  Palette           Workspace: Remove Folder
  Palette           File: Open Recent Workspace
  Palette           Copy: File:Line Reference
  Palette           Snippets: Browse

PREFERENCES
  Palette           Preferences: Open Settings (UI)
  Palette           Preferences: Color Theme
  Palette           Preferences: Edit Custom Theme
  Palette           Preferences: Customize Keybindings
  Palette           Open Config: User Commands / Theme

DEVELOPER
  Palette           Developer: Reload Window
  Palette           Developer: Reset Session

ENV / TIPS
  TERMOCODE_ICON_MODE=nerd_font|unicode|ascii  override icons
  scripts/run-with-small-font.sh               wrapper to launch with
                                               a smaller terminal font
  Esc                                          drop into Vim Normal mode;
                                               the editor is real Neovim
`
	m.preview = preview.New(" Keyboard Shortcuts ", body)
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
}
