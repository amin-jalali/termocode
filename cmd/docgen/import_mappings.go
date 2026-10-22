package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/migrate"
)

// importFamilies are the source editors in page order. The VS Code
// forks use the VS Code tables for any key or setting they do not list
// themselves, so one section covers the family.
var importFamilies = []struct{ id, title string }{
	{"vscode", "VS Code, VSCodium, Cursor and Windsurf"},
	{"zed", "Zed"},
	{"helix", "Helix"},
	{"jetbrains", "JetBrains IDEs"},
	{"sublime", "Sublime Text"},
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// renderImportMappings writes import-mappings.md from the migrate tables.
func renderImportMappings() string {
	var b strings.Builder
	b.WriteString(banner)
	b.WriteString("# Import mappings\n\n")
	b.WriteString("These tables list what **Preferences: Import from Another Editor...** can bring over. " +
		"Anything not listed here is shown in the review panel as *not imported*, with the reason. " +
		"How the import works is in [Import settings from another editor](../guides/import-from-another-editor.md).\n\n")

	byName := map[string]keymap.Action{}
	for _, a := range keymap.AllActions() {
		byName[keymap.ActionName(a)] = a
	}
	km := keymap.Default()

	b.WriteString("## Settings\n\n")
	b.WriteString("The left column is the name in the other editor. The right column is the key in `config.json` " +
		"(see [Settings](settings.md)). Values are converted where needed, for example `on` and `off` become true and false.\n\n")
	for _, f := range importFamilies {
		m := migrate.SettingMaps[f.id]
		fmt.Fprintf(&b, "### %s\n\n", f.title)
		t := newTable(&b, "Setting in the other editor", "termocode setting")
		for _, k := range sortedKeys(m) {
			t.row(code(k), code(m[k]))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Keys\n\n")
	b.WriteString("The left column is the command or action in the other editor. The other columns are the termocode action it becomes, " +
		"that action's default key, and what it does. Your key from the other editor is written to `keymap.json` for that action. " +
		"See [Keyboard shortcuts](keys.md) for every action.\n\n")
	for _, f := range importFamilies {
		m := migrate.ActionMaps[f.id]
		fmt.Fprintf(&b, "### %s\n\n", f.title)
		if f.id == "helix" {
			// mapKey refuses every Helix binding (they are editor-mode keys).
			b.WriteString("Helix key bindings are not imported. They belong to Helix's own editing modes, so every one shows as *not imported* in the review panel.\n\n")
			continue
		}
		t := newTable(&b, "Command in the other editor", "termocode action", "Default key", "What it does")
		for _, k := range sortedKeys(m) {
			name := m[k]
			var keys []string
			desc := ""
			if a, ok := byName[name]; ok {
				keys = km.KeysFor(a)
				desc = keymap.Describe(a).Description
			}
			sort.Strings(keys)
			t.row(code(k), code(name), humanKeys(keys), cell(desc))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Themes\n\n")
	b.WriteString("A theme name that is in this table, or that is the name of a built-in theme, is matched exactly and can be turned on. " +
		"Any other name gets the closest built-in theme as a suggestion only; that row starts off.\n\n")
	t := newTable(&b, "Theme name (lower case)", "termocode theme")
	for _, k := range sortedKeys(migrate.ThemeNames) {
		t.row(code(k), code(migrate.ThemeNames[k]))
	}
	b.WriteString("\n")

	b.WriteString("## Extensions and plug-ins\n\n")
	b.WriteString("Extensions are never installed. A known one becomes a *suggestion*: the language servers or debuggers to add yourself " +
		"with **LSP: Manage Language Servers...**, or a note about the built-in feature that does the same job.\n\n")
	t = newTable(&b, "Extension, plug-in or package", "Suggested tools", "Note")
	for _, k := range sortedKeys(migrate.ExtensionMap) {
		r := migrate.ExtensionMap[k]
		tools := make([]string, len(r.Tools))
		for i, tool := range r.Tools {
			tools[i] = code(tool)
		}
		t.row(code(k), strings.Join(tools, ", "), cell(r.Note))
	}
	b.WriteString("\n")
	return b.String()
}
