package migrate

// jetBrainsActions maps JetBrains action ids to termocode keymap action names.
// Only actions that termocode has are listed; every other id becomes a
// "not imported" row.
var jetBrainsActions = map[string]string{
	// Files and navigation
	"GotoFile": "QuickOpen", "GotoAction": "CommandPalette", "GotoLine": "GotoLine", "FileStructurePopup": "GotoSymbolInFile",
	"RecentFiles": "OpenRecent", "SelectInProjectView": "RevealFile", "ActivateProjectToolWindow": "FocusExplorer",
	"ActivateTerminalToolWindow": "ToggleTerminal", "ActivateProblemsViewToolWindow": "ShowProblems",
	"NextTab": "NextBuffer", "PreviousTab": "PrevBuffer", "CloseContent": "CloseBuffer", "ReopenClosedTab": "ReopenClosed",
	"SaveAll": "SaveAll", "EditorSave": "Save", "$Copy": "Copy", "$Cut": "Cut", "$Paste": "Paste",
	// Search
	"Find": "Find", "Replace": "ReplaceInFile", "FindNext": "NextSearchResult", "FindPrevious": "PrevSearchResult",
	"FindInPath": "WorkspaceSearch", "ReplaceInPath": "ReplaceInWorkspace",
	// Editing
	"CommentByLineComment": "ToggleComment", "EditorDuplicate": "DuplicateLine", "EditorDuplicateLines": "DuplicateLine",
	"ReformatCode": "FormatDocument", "EditorToggleUseSoftWraps": "ToggleWordWrap", "ToggleDistractionFreeMode": "ToggleZenMode",
	"ToggleBookmark": "ToggleBookmark", "ShowBookmarks": "ShowBookmarks",
	// Code intelligence
	"ShowIntentionActions": "CodeActions", "ShowUsages": "FindReferences", "FindUsages": "FindReferences",
	"GotoTypeDeclaration": "GotoTypeDef", "GotoImplementation": "GotoImplementation", "QuickJavaDoc": "Hover", "QuickDocumentation": "Hover",
	"GotoNextError": "NextDiagnostic", "GotoPreviousError": "PrevDiagnostic",
	// Window layout
	"SplitVertically": "SplitVertical", "SplitHorizontally": "SplitHorizontal", "Unsplit": "CloseSplit",
	// Run and debug
	"Debug": "DebugStartContinue", "Resume": "DebugStartContinue", "ToggleLineBreakpoint": "DebugToggleBreakpoint",
	"StepOver": "DebugStepOver", "StepInto": "DebugStepInto",
	// Version control
	"Annotate": "GitBlameLine", "Vcs.ShowTabbedFileHistory": "FileHistory",
}

// jetBrainsKeyNames turns JetBrains key names (lower case) into termocode ones.
var jetBrainsKeyNames = map[string]string{
	"slash": "/", "back_slash": "\\", "back_quote": "`", "open_bracket": "[", "close_bracket": "]", "comma": ",", "period": ".",
	"semicolon": ";", "equals": "=", "minus": "-", "plus": "+", "quote": "'", "enter": "enter", "escape": "esc", "back_space": "backspace",
	"page_up": "pgup", "page_down": "pgdown",
}
