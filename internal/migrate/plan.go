package migrate

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

type RowKind string

const (
	RowSetting   RowKind = "setting"
	RowKey       RowKind = "key"
	RowTheme     RowKind = "theme"
	RowExtension RowKind = "extension"
	RowVimrc     RowKind = "vimrc"
)

// ReviewRow keeps the source text and proposed destination for the review panel.
type ReviewRow struct {
	Kind       RowKind
	SourceText string
	Equivalent string
	Conflict   bool
	Apply      bool
	Target     string
	Value      any
	Key        string
	Action     string
	Tools      []string
	// Reason says why a row is not applied (not imported, already set, conflict).
	Reason string
}

type Current struct {
	Config    map[string]any
	Overrides map[string]keymap.Action
	ThemeID   string
	// KeymapRaw must contain the complete keymap.json when KeymapExists is true.
	KeymapRaw    []byte
	KeymapExists bool
}

type Plan struct {
	Source  Source
	Rows    []ReviewRow
	Current Current
}

func Build(source Source, doc Document, current Current) Plan {
	p := Plan{Source: source, Current: current}
	effective := effectiveBindings(current)
	for _, setting := range doc.Settings {
		target, value, equivalent := mapSetting(source.ID, setting)
		row := ReviewRow{Kind: RowSetting, SourceText: fmt.Sprintf("%s = %v", setting.Name, setting.Value), Equivalent: equivalent, Target: target, Value: value, Apply: target != ""}
		if target != "" && reflect.DeepEqual(current.Config[target], value) {
			row.Apply = false
			row.Equivalent += " (already set)"
		}
		p.Rows = append(p.Rows, row)
	}
	for _, binding := range doc.Keys {
		key, action, equivalent := mapKey(source.ID, binding)
		if key != "" {
			key = keymap.CanonicalKey(key)
		}
		row := ReviewRow{Kind: RowKey, SourceText: binding.Key + "  " + binding.Command, Equivalent: equivalent, Key: key, Action: action, Apply: action != ""}
		if action != "" {
			if existing, ok := effective[key]; ok {
				row.Conflict = keymap.ActionName(existing) != action
				if row.Conflict {
					row.Apply = false
					row.Equivalent += " (conflicts with " + keymap.ActionName(existing) + ")"
				}
			}
		}
		if action != "" {
			for i := range p.Rows {
				previous := &p.Rows[i]
				if previous.Kind == RowKey && previous.Key == key && previous.Action != "" && previous.Action != action {
					row.Conflict = true
					row.Apply = false
					row.Equivalent += " (conflicts with another imported binding)"
					previous.Conflict = true
					previous.Apply = false
					previous.Equivalent += " (conflicts with another imported binding)"
				}
			}
		}
		p.Rows = append(p.Rows, row)
	}
	if source.ID == "vscode" || source.ID == "vscodium" || source.ID == "cursor" || source.ID == "windsurf" {
		addFamiliarRows(&p, current)
	}
	if doc.Theme != "" {
		id, equiv := mapTheme(doc.Theme)
		_, known := ThemeNames[strings.ToLower(strings.TrimSpace(doc.Theme))]
		for _, t := range theme.AvailableThemes() {
			known = known || strings.EqualFold(t.Name, doc.Theme) || strings.EqualFold(t.ID, doc.Theme)
		}
		p.Rows = append(p.Rows, ReviewRow{Kind: RowTheme, SourceText: doc.Theme, Equivalent: equiv, Target: id, Apply: known && id != "" && id != current.ThemeID})
	}
	for _, extension := range doc.Extensions {
		result, known := extensionMapping(extension)
		equivalent := "no equivalent"
		if known {
			equivalent = result.Note
			if len(result.Tools) > 0 {
				equivalent = "install " + fmt.Sprint(result.Tools)
			}
			if equivalent == "" {
				equivalent = "no equivalent"
			}
		}
		p.Rows = append(p.Rows, ReviewRow{Kind: RowExtension, SourceText: extension, Equivalent: equivalent, Tools: result.Tools, Apply: known && len(result.Tools) > 0})
	}
	if source.ID == "vim" {
		p.Rows = append(p.Rows, ReviewRow{Kind: RowVimrc, SourceText: "~/.vimrc", Equivalent: "load your vimrc", Apply: false})
	}
	for _, s := range doc.Skipped {
		p.Rows = append(p.Rows, ReviewRow{Kind: s.Kind, SourceText: s.Source, Equivalent: "not imported", Reason: s.Reason})
	}
	for i := range p.Rows {
		if p.Rows[i].Reason == "" {
			p.Rows[i].Reason = reasonNotApplied(p.Rows[i])
		}
	}
	return p
}

// reasonNotApplied explains in a few words why a row is off by default.
func reasonNotApplied(r ReviewRow) string {
	if r.Apply {
		return ""
	}
	switch r.Kind {
	case RowSetting:
		if r.Target == "" {
			return "termocode has no matching setting"
		}
		return "already set to this value"
	case RowKey:
		switch {
		case r.Conflict:
			return "the key is already used; turn it on to replace the other binding"
		case r.Action != "":
			return "already bound to this action"
		case strings.HasPrefix(r.Equivalent, "no equivalent ("):
			return strings.TrimSuffix(strings.TrimPrefix(r.Equivalent, "no equivalent ("), ")")
		}
		return "termocode has no command for this action"
	case RowTheme:
		if r.Target != "" && r.Target == r.Equivalent {
			return "already your theme"
		}
		return "no built-in theme has this name; the closest one is only a suggestion"
	case RowExtension:
		if len(r.Tools) == 0 {
			if r.Equivalent == "no equivalent" {
				return "no matching tool or feature in termocode"
			}
			return "nothing to install; see the note"
		}
	}
	return ""
}

// effectiveBindings returns default and override keys, all in canonical form.
func effectiveBindings(current Current) map[string]keymap.Action {
	effective := map[string]keymap.Action{}
	for key, action := range keymap.Default().Bindings() {
		effective[keymap.CanonicalKey(key)] = action
	}
	for key, action := range current.Overrides {
		effective[keymap.CanonicalKey(key)] = action
	}
	return effective
}

func extensionMapping(id string) (ExtensionResult, bool) {
	for key, result := range ExtensionMap {
		if strings.EqualFold(key, id) {
			return result, true
		}
	}
	return ExtensionResult{}, false
}

func addFamiliarRows(p *Plan, current Current) {
	defaults := []KeyBinding{
		{"ctrl+p", "workbench.action.quickOpen"},
		{"ctrl+shift+p", "workbench.action.showCommands"}, {"ctrl+`", "workbench.action.terminal.toggleTerminal"},
		{"ctrl+b", "workbench.action.toggleSidebarVisibility"},
		{"ctrl+shift+e", "workbench.view.explorer"}, {"ctrl+shift+f", "workbench.action.findInFiles"},
		{"ctrl+shift+g", "workbench.view.scm"}, {"f5", "workbench.action.debug.start"},
		{"f9", "workbench.action.debug.toggleBreakpoint"}, {"f10", "workbench.action.debug.stepOver"},
		{"f11", "workbench.action.debug.stepInto"}, {"ctrl+/", "editor.action.commentLine"},
	}
	effective := effectiveBindings(current)
	for _, binding := range defaults {
		binding.Command = familiarCommand(p.Source.ID, binding.Command)
		key, action, equivalent := mapKey(p.Source.ID, binding)
		if key != "" {
			key = keymap.CanonicalKey(key)
		}
		if key == "" {
			key, _ = normalizeSourceKey(p.Source.ID, binding.Key)
		}
		row := ReviewRow{Kind: RowKey, SourceText: familiarSource(p.Source.ID) + " default: " + binding.Key, Equivalent: equivalent, Key: key, Action: action, Apply: true}
		if action == "" {
			row.Equivalent = "no equivalent"
			row.Apply = false
		} else if existing, occupied := effective[key]; occupied {
			row.Apply = false
			if keymap.ActionName(existing) != action {
				row.Conflict = true
				row.Equivalent += " (conflicts with " + keymap.ActionName(existing) + ")"
			} else {
				row.Equivalent += " (already bound)"
			}
		}
		if action != "" {
			for _, imported := range p.Rows {
				if imported.Kind != RowKey || imported.Key != key || imported.Action == "" || strings.Contains(imported.SourceText, " default: ") {
					continue
				}
				row.Apply = false
				if imported.Action != action {
					row.Conflict = true
					row.Equivalent += " (conflicts with your imported binding)"
				}
				break
			}
		}
		p.Rows = append(p.Rows, row)
	}
}

func familiarCommand(source, vscodeCommand string) string {
	commands := map[string]map[string]string{
		"zed":       {"workbench.action.quickOpen": "workspace::ActivateFile", "workbench.action.showCommands": "command_palette::Toggle", "workbench.action.terminal.toggleTerminal": "terminal::Toggle", "workbench.view.explorer": "workspace::ActivateProjectPanel", "workbench.action.findInFiles": "pane::DeploySearch", "workbench.action.debug.start": "debugger::Start", "workbench.action.debug.toggleBreakpoint": "debugger::ToggleBreakpoint", "workbench.action.debug.stepOver": "debugger::StepOver", "workbench.action.debug.stepInto": "debugger::StepInto", "editor.action.commentLine": "editor::ToggleComments"},
		"helix":     {"workbench.action.quickOpen": "file_picker", "workbench.action.showCommands": "command_palette", "workbench.action.files.save": "save", "workbench.action.findInFiles": "global_search", "editor.action.commentLine": "toggle_comments"},
		"jetbrains": {"workbench.action.quickOpen": "GotoFile", "workbench.action.showCommands": "GotoAction", "workbench.action.files.save": "SaveAll", "workbench.action.findInFiles": "FindInPath"},
		"sublime":   {"workbench.action.quickOpen": "show_overlay", "workbench.action.showCommands": "show_command_palette", "workbench.action.files.save": "save", "workbench.action.findInFiles": "find_in_files", "editor.action.commentLine": "toggle_comment", "workbench.action.toggleSidebarVisibility": "toggle_side_bar"},
	}
	if m := commands[source]; m != nil {
		if c := m[vscodeCommand]; c != "" {
			return c
		}
		return "__no_equivalent__"
	}
	return vscodeCommand
}

func familiarSource(source string) string {
	if source == "vscodium" || source == "cursor" || source == "windsurf" {
		return "VS Code"
	}
	return map[string]string{"vscode": "VS Code", "zed": "Zed", "helix": "Helix", "jetbrains": "JetBrains", "sublime": "Sublime", "vim": "Vim"}[source]
}

func (p Plan) Counts() map[RowKind]int {
	out := map[RowKind]int{}
	for _, row := range p.Rows {
		out[row.Kind]++
	}
	return out
}
