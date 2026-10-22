package migrate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSublimeKeyNamesAndCommands(t *testing.T) {
	for _, tc := range []struct{ key, command, wantKey, wantAction string }{
		{"ctrl+shift+p", "show_overlay:command_palette", "ctrl+shift+p", "CommandPalette"},
		{"ctrl+alt+s", "show_overlay:goto:@", "ctrl+alt+s", "GotoSymbolInFile"},
		{"ctrl+alt+l", "show_overlay:goto::", "ctrl+alt+l", "GotoLine"},
		{"ctrl+alt+p", "show_overlay", "ctrl+alt+p", "QuickOpen"},
		{"ctrl+alt+f", "show_panel:find_in_files", "ctrl+alt+f", "WorkspaceSearch"},
		{"ctrl+alt+h", "show_panel:replace", "ctrl+alt+h", "ReplaceInFile"},
		{"ctrl+shift+d", "duplicate_line", "ctrl+shift+d", "DuplicateLine"},
		{"ctrl+alt+escape", "close", "ctrl+alt+esc", "CloseBuffer"},
		{"ctrl+pagedown", "next_view", "ctrl+pgdown", "NextBuffer"},
		{"ctrl+pageup", "prev_view", "ctrl+pgup", "PrevBuffer"},
		{"ctrl+alt+w", "toggle_setting:word_wrap", "ctrl+alt+w", "ToggleWordWrap"},
		{"ctrl+alt+b", "toggle_side_bar", "ctrl+alt+b", "ToggleExplorer"},
		{"super+shift+p", "show_command_palette", "ctrl+shift+p", "CommandPalette"},
		{"f12", "lsp_symbol_references", "f12", "FindReferences"},
		{"ctrl+alt+plus", "toggle_distraction_free", "ctrl+alt++", "ToggleZenMode"},
	} {
		key, action, note := mapKey("sublime", KeyBinding{tc.key, tc.command})
		if key != tc.wantKey || action != tc.wantAction {
			t.Errorf("%s %s: got %q %q (%s), want %q %q", tc.key, tc.command, key, action, note, tc.wantKey, tc.wantAction)
		}
	}
	for _, c := range []string{"rename_something_odd", "toggle_comment:block", "move", "toggle_minimap"} {
		if _, action, note := mapKey("sublime", KeyBinding{"ctrl+alt+p", c}); action != "" || !strings.HasPrefix(note, "no equivalent") {
			t.Errorf("%s: %q %q", c, action, note)
		}
	}
	if n := len(ActionMaps["sublime"]); n < 30 {
		t.Errorf("only %d Sublime commands mapped", n)
	}
}

func TestSublimeFixtureReachesThePlan(t *testing.T) {
	d, err := ReadSublime(filepath.Join("testdata", "sublime-linux"))
	if err != nil {
		t.Fatal(err)
	}
	p := Build(Source{ID: "sublime", Title: "Sublime Text 4"}, d, Current{})
	find := func(kind RowKind, prefix string) ReviewRow {
		t.Helper()
		for _, r := range p.Rows {
			if r.Kind == kind && strings.HasPrefix(r.SourceText, prefix) {
				return r
			}
		}
		t.Fatalf("no %s row %q", kind, prefix)
		return ReviewRow{}
	}
	if r := find(RowKey, "ctrl+alt+s  show_overlay:goto:@"); r.Action != "GotoSymbolInFile" || !r.Apply {
		t.Errorf("%+v", r)
	}
	if r := find(RowKey, "ctrl+k ctrl+b"); r.Apply || r.Action != "" {
		t.Errorf("chord imported: %+v", r)
	}
	if r := find(RowKey, "ctrl+alt+r  rename_something_odd"); r.Apply {
		t.Errorf("%+v", r)
	}
	if r := find(RowTheme, "Monokai"); !r.Apply || r.Target != "one-dark" {
		t.Errorf("%+v", r)
	}
	if r := find(RowSetting, "tab_size"); !r.Apply || r.Target != "tab_size" {
		t.Errorf("%+v", r)
	}
	if r := find(RowSetting, "word_wrap"); !r.Apply || r.Value != true {
		t.Errorf("%+v", r)
	}
	if r := find(RowSetting, "font_size"); r.Apply {
		t.Errorf("%+v", r)
	}
	if r := find(RowExtension, "LSP-gopls"); !r.Apply || len(r.Tools) == 0 {
		t.Errorf("%+v", r)
	}
	if r := find(RowExtension, "GitGutter"); r.Apply || r.Equivalent != "Source Control view" {
		t.Errorf("%+v", r)
	}
	if r := find(RowExtension, "Dracula Color Scheme"); r.Apply || r.Equivalent != "no equivalent" {
		t.Errorf("%+v", r)
	}
}
