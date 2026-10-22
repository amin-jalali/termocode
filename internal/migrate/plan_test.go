package migrate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The setting targets are checked against the app's SettingsCatalog in
// catalog_test.go (package migrate_test: the app imports this package).
func TestCatalogCoverage(t *testing.T) {
	actions := map[string]bool{}
	for _, action := range keymap.AllActions() {
		actions[keymap.ActionName(action)] = true
	}
	for source, table := range ActionMaps {
		for _, target := range table {
			if !actions[target] {
				t.Errorf("%s action target %q is absent from keymap", source, target)
			}
		}
	}
	themes := map[string]bool{}
	for _, entry := range theme.AvailableThemes() {
		themes[entry.ID] = true
	}
	for source, target := range ThemeNames {
		if !themes[target] {
			t.Errorf("theme %q target %q is absent from AvailableThemes", source, target)
		}
	}
}

func TestPlanCarriesReviewRowsAndConflict(t *testing.T) {
	doc := Document{Settings: []Setting{{Name: "editor.tabSize", Value: float64(4)}}, Keys: []KeyBinding{{Key: "ctrl+p", Command: "workbench.action.quickOpen"}}, Theme: "Dark+ (default dark)"}
	p := Build(Source{ID: "vscode", Title: "VS Code"}, doc, Current{Overrides: map[string]keymap.Action{"ctrl+p": keymap.ActionSave}})
	byKind := map[RowKind]ReviewRow{}
	for _, row := range p.Rows {
		if row.Kind != RowKey || row.SourceText == "ctrl+p  workbench.action.quickOpen" {
			byKind[row.Kind] = row
		}
	}
	if row, ok := byKind[RowSetting]; !ok || row.SourceText == "" || row.Equivalent == "" || !row.Apply {
		t.Fatalf("setting row missing review data: %+v", row)
	}
	if row, ok := byKind[RowKey]; !ok || !row.Conflict || row.Apply || row.SourceText == "" || row.Equivalent == "" {
		t.Fatalf("key conflict row missing or incorrect: %+v", row)
	}
	if row, ok := byKind[RowTheme]; !ok || row.Equivalent == "" {
		t.Fatalf("theme row missing: %+v", row)
	}
}

func TestKeyConversionAndFamiliarRows(t *testing.T) {
	key, action, note := mapKey("vscode", KeyBinding{Key: "cmd+shift+p", Command: "workbench.action.showCommands"})
	if key != "ctrl+shift+p" || action != "CommandPalette" || note == "" {
		t.Fatalf("Cmd conversion = %q, %q, %q", key, action, note)
	}
	p := Build(Source{ID: "vscode"}, Document{}, Current{})
	familiar := false
	for _, row := range p.Rows {
		if row.SourceText == "VS Code default: ctrl+shift+f" {
			familiar = row.Apply
		}
	}
	if !familiar {
		t.Fatal("free familiar key was not offered as an applyable row")
	}
}

func TestSourceControlFamiliarKeyHasNoFakeBlameAction(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{}, Current{})
	for _, row := range p.Rows {
		if row.SourceText == "VS Code default: ctrl+shift+g" {
			if row.Action != "" || row.Apply || row.Equivalent != "no equivalent" {
				t.Fatalf("Source Control key has an invented mapping: %+v", row)
			}
			return
		}
	}
	t.Fatal("Source Control familiar row missing")
}

func TestChangesOnlyAppliesSelectedRows(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Settings: []Setting{{Name: "editor.tabSize", Value: float64(4)}}, Keys: []KeyBinding{{Key: "ctrl+alt+q", Command: "workbench.action.files.save"}}}, Current{})
	for i := range p.Rows {
		p.Rows[i].Apply = false
	}
	contents, err := Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents.Config) != 0 || len(contents.Keymap) != 0 {
		t.Fatalf("unticked rows changed files: %+v", contents)
	}
	p.Rows[0].Apply = true
	contents, err = Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents.Config) == 0 && len(contents.Keymap) == 0 {
		t.Fatal("selected row produced no file content")
	}
}

func TestExtensionTargetsAreRegistered(t *testing.T) {
	for extension, row := range ExtensionMap {
		for _, tool := range row.Tools {
			if _, ok := lspinstall.Lookup(tool); !ok {
				t.Errorf("%s maps to unknown tool %q", extension, tool)
			}
		}
	}
}

func TestExtensionIDsMatchCaseInsensitively(t *testing.T) {
	result, ok := extensionMapping("GOLANG.GO")
	if !ok || len(result.Tools) == 0 || result.Tools[0] != "gopls" {
		t.Fatalf("case-insensitive extension lookup failed: %+v, %t", result, ok)
	}
}

func TestApplySelectedExtensionsReturnsTools(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Extensions: []string{"golang.go", "ms-python.python"}}, Current{})
	contents, err := Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"gopls": true, "dlv": true, "pyright": true, "debugpy": true}
	for _, tool := range contents.Tools {
		delete(want, tool)
	}
	if len(want) > 0 {
		t.Fatalf("missing tools: %v; got %v", want, contents.Tools)
	}
}

func TestPerSourceKeyNormalizationAndActions(t *testing.T) {
	cases := []struct{ source, key, command, wantKey, wantAction, wantNote string }{
		{"vscode", "ctrl+shift+p", "workbench.action.showCommands", "ctrl+shift+p", "CommandPalette", ""},
		{"zed", "ctrl-p", "command_palette::Toggle", "ctrl+p", "CommandPalette", ""},
		{"helix", "C-s", "save", "", "", "no equivalent (editor-mode binding)"},
		{"helix", "A-x", "search_next", "", "", "no equivalent (editor-mode binding)"},
		{"jetbrains", "control shift N", "GotoFile", "ctrl+shift+n", "QuickOpen", ""},
		{"jetbrains", "control K, control S", "GotoFile", "", "", "no equivalent (two-key chord)"},
		{"sublime", "ctrl+shift+p", "show_command_palette", "ctrl+shift+p", "CommandPalette", ""},
		{"zed", "ctrl-k ctrl-s", "workspace::Save", "", "", "no equivalent (two-key chord)"},
		{"sublime", "ctrl+k ctrl+b", "save", "", "", "no equivalent (two-key chord)"},
		{"vscode", "ctrl+k ctrl+s", "workbench.action.files.save", "", "", "no equivalent (two-key chord)"},
		{"vscode", "q", "workbench.action.files.save", "", "", "no equivalent (needs a modifier in termocode)"},
		{"helix", "x", "save", "", "", "no equivalent (editor-mode binding)"},
		{"helix", "C-s", "save", "", "", "no equivalent (editor-mode binding)"},
		{"vim", "ctrl+p", "anything", "", "", "no equivalent (editor-mode binding)"},
		{"zed", "ctrl--", "workspace::Save", "ctrl+-", "Save", ""},
	}
	for _, tc := range cases {
		t.Run(tc.source+"/"+tc.key, func(t *testing.T) {
			k, action, note := mapKey(tc.source, KeyBinding{Key: tc.key, Command: tc.command})
			if k != tc.wantKey || action != tc.wantAction || (tc.wantNote != "" && note != tc.wantNote) {
				t.Fatalf("mapKey = %q, %q, %q; want %q, %q, %q", k, action, note, tc.wantKey, tc.wantAction, tc.wantNote)
			}
		})
	}
}

func TestCanonicalModifierOrderFindsImportedConflicts(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "alt+shift+f", Command: "workbench.action.files.save"}}}, Current{Overrides: map[string]keymap.Action{"shift+alt+f": keymap.ActionQuickOpen}})
	for _, row := range p.Rows {
		if row.SourceText == "alt+shift+f  workbench.action.files.save" {
			if row.Key != "alt+shift+f" || !row.Conflict || row.Apply {
				t.Fatalf("canonical modifier-order conflict not found: %+v", row)
			}
			return
		}
	}
	t.Fatal("imported binding missing")
}

func TestFamiliarRowsUseOnlyVerifiedSourceDefaults(t *testing.T) {
	for _, source := range []string{"vscode", "zed", "helix", "jetbrains", "sublime", "vim"} {
		p := Build(Source{ID: source}, Document{}, Current{})
		for _, row := range p.Rows {
			if row.Kind == RowKey && strings.Contains(row.SourceText, " default: ") {
				if source != "vscode" {
					t.Fatalf("unverified familiar default for %s: %+v", source, row)
				}
				if source == "jetbrains" && row.SourceText == "JetBrains default: ctrl+shift+p" {
					t.Fatalf("VS Code default labeled as JetBrains: %+v", row)
				}
			}
		}
	}
}

func TestCurrentKeyConflictUsesCanonicalModifierOrder(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "ctrl+shift+p", Command: "workbench.action.showCommands"}}}, Current{Overrides: map[string]keymap.Action{"shift+ctrl+p": keymap.ActionSave}})
	for _, row := range p.Rows {
		if row.SourceText == "ctrl+shift+p  workbench.action.showCommands" && (!row.Conflict || row.Apply) {
			t.Fatalf("modifier-order conflict not found: %+v", row)
		}
	}
}

func TestImportedRowsConflictWithEachOther(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "alt+q", Command: "workbench.action.quickOpen"}, {Key: "alt+q", Command: "workbench.action.files.save"}}}, Current{})
	conflicts := 0
	for _, row := range p.Rows {
		if row.Kind == RowKey && row.Key == "alt+q" && row.Conflict && !row.Apply {
			conflicts++
		}
	}
	if conflicts != 2 {
		t.Fatalf("both imported rows should be marked as a conflict, got %d", conflicts)
	}
}

func TestPerSourceSettingsConversion(t *testing.T) {
	cases := []struct {
		source, name     string
		value            any
		target           string
		want             any
		wantNoEquivalent bool
	}{
		{"vscode", "editor.tabSize", float64(4), "tab_size", float64(4), false},
		{"zed", "tab_size", float64(4), "tab_size", float64(4), false},
		{"helix", "[editor].auto-save", "false", "auto_save", false, false},
		{"jetbrains", "tabSize", "4", "tab_size", float64(4), false},
		{"jetbrains", "SAVE_FILES_ON_FRAME_DEACTIVATION", "false", "auto_save", false, false},
		{"jetbrains", "WRAP_WHEN_TYPING_REACHES_RIGHT_MARGIN", "false", "", nil, true},
		{"sublime", "save_on_focus_lost", true, "auto_save", true, false},
		{"zed", "autosave", map[string]any{"after_delay": map[string]any{"milliseconds": float64(1000)}}, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.source+"/"+tc.name, func(t *testing.T) {
			target, got, equivalent := mapSetting(tc.source, Setting{Name: tc.name, Value: tc.value})
			if tc.wantNoEquivalent {
				if target != "" || equivalent != "no equivalent" {
					t.Fatalf("got %q, %v, %q", target, got, equivalent)
				}
				return
			}
			if target != tc.target || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, %#v; want %q, %#v", target, got, tc.target, tc.want)
			}
		})
	}
}

func TestBuildListsFamiliarRowsAndImportedConflictsForEverySource(t *testing.T) {
	for _, source := range []string{"vscode", "zed", "helix", "jetbrains", "sublime"} {
		t.Run(source, func(t *testing.T) {
			doc := Document{Keys: []KeyBinding{{Key: "ctrl+p", Command: map[string]string{"vscode": "workbench.action.quickOpen", "zed": "workspace::ActivateFile", "helix": "file_picker", "jetbrains": "GotoFile", "sublime": "show_overlay"}[source]}, {Key: "ctrl+p", Command: "unknown-action"}}}
			p := Build(Source{ID: source}, doc, Current{Overrides: map[string]keymap.Action{"ctrl+p": keymap.ActionSave}})
			foundFamiliar, conflicts, familiarCount := false, 0, 0
			wantFamiliar := 12
			if source != "vscode" {
				wantFamiliar = 0
			}
			for _, row := range p.Rows {
				if row.Kind == RowKey && strings.HasPrefix(row.SourceText, familiarSource(source)+" default: ctrl+p") {
					foundFamiliar = true
				}
				if row.Kind == RowKey && strings.HasPrefix(row.SourceText, familiarSource(source)+" default:") {
					familiarCount++
				}
				if row.Kind == RowKey && row.Key == "ctrl+p" && row.Conflict && !row.Apply {
					conflicts++
				}
			}
			if wantFamiliar > 0 && !foundFamiliar {
				t.Fatal("familiar default row missing")
			}
			if familiarCount != wantFamiliar {
				t.Fatalf("got %d familiar rows, want %d", familiarCount, wantFamiliar)
			}
			if source == "vscode" && conflicts < 1 {
				t.Fatal("taken imported key was not marked conflicting")
			}
		})
	}
}

func TestUnknownThemeSuggestsWithoutApplying(t *testing.T) {
	id, suggestion := mapTheme("Some Unknown Dark Theme")
	if id == "" || !strings.Contains(suggestion, "suggestion") {
		t.Fatalf("no nearest suggestion: %q %q", id, suggestion)
	}
	p := Build(Source{ID: "vscode"}, Document{Theme: "Some Unknown Dark Theme"}, Current{})
	for _, row := range p.Rows {
		if row.Kind == RowTheme && (row.Apply || row.Target == "") {
			t.Fatalf("unknown theme should be a suggestion only: %+v", row)
		}
	}
}

func TestApplyKeepsRawExtensionKeymapEntries(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "ctrl+alt+q", Command: "workbench.action.files.save"}}}, Current{})
	p.Current.KeymapRaw = []byte(`{"alt+e":"ext:example","alt+x":"QuickOpen"}`)
	p.Current.KeymapExists = true
	for _, row := range p.Rows {
		if row.Kind == RowKey {
			t.Logf("row=%+v", row)
		}
	}
	contents, err := Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(contents.Keymap, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["alt+e"]) != `"ext:example"` || string(got["alt+x"]) != `"QuickOpen"` {
		t.Fatalf("raw entries lost: %s", contents.Keymap)
	}
	if string(got["ctrl+alt+q"]) != `"Save"` {
		t.Fatalf("selected binding missing: %s", contents.Keymap)
	}
}

func TestApplyRequiresRawKeymapWhenFileExists(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{}, Current{KeymapExists: true})
	if _, err := Apply(p); err == nil || !strings.Contains(err.Error(), "raw keymap.json is required") {
		t.Fatalf("Apply error = %v, want missing raw keymap error", err)
	}
}

func findKeyRow(t *testing.T, p Plan, source string) ReviewRow {
	t.Helper()
	for _, row := range p.Rows {
		if row.SourceText == source {
			return row
		}
	}
	t.Fatalf("row %q missing", source)
	return ReviewRow{}
}

func TestImportedKeyConflictsWithNonCanonicalDefault(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "shift+alt+f", Command: "workbench.action.files.save"}}}, Current{})
	row := findKeyRow(t, p, "shift+alt+f  workbench.action.files.save")
	if !row.Conflict || row.Apply {
		t.Fatalf("default FormatDocument binding must conflict: %+v", row)
	}
}

func TestShiftOnlyKeyHasNoEquivalent(t *testing.T) {
	for _, key := range []string{"shift+q", "shift+1", "shift+/"} {
		p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: key, Command: "workbench.action.files.save"}}}, Current{})
		row := findKeyRow(t, p, key+"  workbench.action.files.save")
		if row.Apply || row.Key != "" || !strings.Contains(row.Equivalent, "needs ctrl or alt") {
			t.Fatalf("%s should not be importable: %+v", key, row)
		}
	}
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "shift+f20", Command: "workbench.action.files.save"}}}, Current{})
	if row := findKeyRow(t, p, "shift+f20  workbench.action.files.save"); !row.Apply {
		t.Fatalf("function key with shift should still work: %+v", row)
	}
}

func TestFamiliarRowYieldsToImportedBinding(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "ctrl+shift+p", Command: "workbench.action.files.save"}}}, Current{})
	for _, row := range p.Rows {
		if row.Kind == RowKey && row.Key == "ctrl+shift+p" && row.Action == "CommandPalette" {
			if !row.Conflict || row.Apply {
				t.Fatalf("familiar row must not override the imported binding: %+v", row)
			}
		}
		if row.SourceText == "ctrl+shift+p  workbench.action.files.save" && !row.Apply {
			t.Fatalf("the user's own binding should win: %+v", row)
		}
	}
}

func TestEquivalentUsesCanonicalKey(t *testing.T) {
	p := Build(Source{ID: "vscode"}, Document{Keys: []KeyBinding{{Key: "shift+alt+q", Command: "workbench.action.files.save"}}}, Current{})
	row := findKeyRow(t, p, "shift+alt+q  workbench.action.files.save")
	if !strings.HasPrefix(row.Equivalent, row.Key+" ") {
		t.Fatalf("equivalent %q does not match key %q", row.Equivalent, row.Key)
	}
}
