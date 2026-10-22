package migrate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestJetBrainsKeyNamesAndActions(t *testing.T) {
	for _, tc := range []struct{ key, command, wantKey, wantAction string }{
		{"control alt P", "GotoFile", "ctrl+alt+p", "QuickOpen"},
		{"control SLASH", "CommentByLineComment", "ctrl+/", "ToggleComment"},
		{"control BACK_SLASH", "Find", "ctrl+\\", "Find"},
		{"control BACK_QUOTE", "ActivateTerminalToolWindow", "ctrl+`", "ToggleTerminal"},
		{"control alt OPEN_BRACKET", "ShowBookmarks", "ctrl+alt+[", "ShowBookmarks"},
		{"alt ENTER", "ShowIntentionActions", "alt+enter", "CodeActions"},
		{"control shift PAGE_DOWN", "NextTab", "ctrl+shift+pgdown", "NextBuffer"},
		{"control ESCAPE", "Unsplit", "ctrl+esc", "CloseSplit"},
		{"shift ctrl N", "GotoFile", "ctrl+shift+n", "QuickOpen"},
		{"F5", "Debug", "f5", "DebugStartContinue"},
		{"meta shift A", "GotoAction", "ctrl+shift+a", "CommandPalette"},
	} {
		key, action, note := mapKey("jetbrains", KeyBinding{tc.key, tc.command})
		if key != tc.wantKey || action != tc.wantAction {
			t.Errorf("%q %s: got %q %q (%s), want %q %q", tc.key, tc.command, key, action, note, tc.wantKey, tc.wantAction)
		}
	}
	// Actions termocode does not have stay unmapped.
	for _, id := range []string{"RenameElement", "MoveLineUp", "GotoDeclaration", "Unknown.Action"} {
		if _, action, note := mapKey("jetbrains", KeyBinding{"control alt P", id}); action != "" || !strings.HasPrefix(note, "no equivalent") {
			t.Errorf("%s: %q %q", id, action, note)
		}
	}
	if n := len(ActionMaps["jetbrains"]); n < 40 {
		t.Errorf("only %d JetBrains actions mapped", n)
	}
}

func TestJetBrainsThemeSettingsAndPluginsReachThePlan(t *testing.T) {
	d, err := ReadJetBrains(filepath.Join("testdata", "jetbrains-linux"))
	if err != nil {
		t.Fatal(err)
	}
	p := Build(Source{ID: "jetbrains", Title: "GoLand 2025.1"}, d, Current{})
	assertRow := func(kind RowKind, source string, apply bool, check func(ReviewRow) bool) {
		t.Helper()
		for _, r := range p.Rows {
			if r.Kind == kind && strings.HasPrefix(r.SourceText, source) {
				if r.Apply != apply || (check != nil && !check(r)) {
					t.Errorf("row %q: %+v", source, r)
				}
				return
			}
		}
		t.Errorf("no %s row for %q", kind, source)
	}
	assertRow(RowKey, "control alt P  GotoFile", true, func(r ReviewRow) bool { return r.Key == "ctrl+alt+p" && r.Action == "QuickOpen" })
	assertRow(RowKey, "control K, control F  FindInPath", false, func(r ReviewRow) bool { return r.Action == "" })
	assertRow(RowKey, "alt F18  Unknown.Action", false, func(r ReviewRow) bool { return r.Action == "" })
	assertRow(RowTheme, "Darcula", true, func(r ReviewRow) bool { return r.Target == "vscode-dark-plus" })
	assertRow(RowSetting, "USE_SOFT_WRAPS", true, func(r ReviewRow) bool { return r.Target == "word_wrap" && r.Value == true })
	assertRow(RowSetting, "TAB_SIZE = 4", true, func(r ReviewRow) bool { return r.Target == "tab_size" && r.Value == float64(4) })
	assertRow(RowSetting, "TAB_SIZE (Python) = 8", false, nil)
	assertRow(RowSetting, "FONT_SIZE", false, nil)
	assertRow(RowSetting, "SAVE_FILES_ON_FRAME_DEACTIVATION", true, func(r ReviewRow) bool { return r.Target == "auto_save" })
	assertRow(RowExtension, "org.jetbrains.plugins.go", true, func(r ReviewRow) bool { return len(r.Tools) == 2 })
	assertRow(RowExtension, "IdeaVIM", false, func(r ReviewRow) bool { return r.Equivalent == "load your vimrc" })
	assertRow(RowExtension, "localtool", false, func(r ReviewRow) bool { return r.Equivalent == "no equivalent" })
}
