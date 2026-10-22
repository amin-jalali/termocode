package migrate

// sublimeActions maps Sublime commands to termocode keymap action names. A
// command that takes arguments is written with them folded in by
// sublimeCommand, for example "show_overlay:goto:@". Commands that termocode
// does not have become "not imported" rows.
var sublimeActions = map[string]string{
	// Files and navigation
	"show_overlay": "QuickOpen", "show_overlay:goto:@": "GotoSymbolInFile", "show_overlay:goto::": "GotoLine",
	"show_overlay:command_palette": "CommandPalette", "show_command_palette": "CommandPalette",
	"save": "Save", "save_all": "SaveAll", "close": "CloseBuffer", "next_view": "NextBuffer", "prev_view": "PrevBuffer",
	"reopen_last_file": "ReopenClosed", "toggle_side_bar": "ToggleExplorer", "toggle_distraction_free": "ToggleZenMode",
	"toggle_terminus_panel": "ToggleTerminal", "exit": "Quit", "copy": "Copy", "cut": "Cut", "paste": "Paste",
	// Search
	"find_in_files": "WorkspaceSearch", "show_panel:find_in_files": "WorkspaceSearch", "show_panel:find": "Find",
	"show_panel:incremental_find": "Find", "show_panel:replace": "ReplaceInFile", "find_next": "NextSearchResult", "find_prev": "PrevSearchResult",
	// Editing
	"toggle_comment": "ToggleComment", "duplicate_line": "DuplicateLine", "toggle_setting:word_wrap": "ToggleWordWrap",
	"toggle_bookmark": "ToggleBookmark",
	// Language server (the LSP package)
	"lsp_symbol_references": "FindReferences", "lsp_symbol_type_definition": "GotoTypeDef", "lsp_symbol_implementation": "GotoImplementation",
	"lsp_format_document": "FormatDocument", "lsp_code_actions": "CodeActions", "lsp_hover": "Hover",
}

// sublimeKeyNames turns Sublime key names into termocode ones.
var sublimeKeyNames = map[string]string{
	"escape": "esc", "pageup": "pgup", "pagedown": "pgdown", "plus": "+", "minus": "-", "equals": "=",
}
