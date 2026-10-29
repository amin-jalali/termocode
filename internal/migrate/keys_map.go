package migrate

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// ActionMaps maps source commands to stable keymap action names.
var ActionMaps = map[string]map[string]string{
	"vscode":    {"workbench.action.quickOpen": "QuickOpen", "workbench.action.showCommands": "CommandPalette", "workbench.action.terminal.toggleTerminal": "ToggleTerminal", "workbench.view.explorer": "FocusExplorer", "workbench.action.toggleSidebarVisibility": "ToggleExplorer", "workbench.action.files.save": "Save", "workbench.action.findInFiles": "WorkspaceSearch", "editor.action.commentLine": "ToggleComment", "editor.action.formatDocument": "FormatDocument", "workbench.action.debug.start": "DebugStartContinue", "workbench.action.debug.toggleBreakpoint": "DebugToggleBreakpoint", "workbench.action.debug.stepOver": "DebugStepOver", "workbench.action.debug.stepInto": "DebugStepInto", "workbench.action.zenMode": "ToggleZenMode"},
	"vscodium":  {"workbench.action.quickOpen": "QuickOpen", "workbench.action.showCommands": "CommandPalette", "workbench.action.terminal.toggleTerminal": "ToggleTerminal", "workbench.view.explorer": "FocusExplorer", "workbench.action.files.save": "Save", "workbench.action.findInFiles": "WorkspaceSearch", "editor.action.commentLine": "ToggleComment", "editor.action.formatDocument": "FormatDocument"},
	"cursor":    {"workbench.action.quickOpen": "QuickOpen", "workbench.action.showCommands": "CommandPalette", "workbench.action.terminal.toggleTerminal": "ToggleTerminal", "workbench.view.explorer": "FocusExplorer", "workbench.action.files.save": "Save", "editor.action.commentLine": "ToggleComment"},
	"windsurf":  {"workbench.action.quickOpen": "QuickOpen", "workbench.action.showCommands": "CommandPalette", "workbench.action.terminal.toggleTerminal": "ToggleTerminal", "workbench.view.explorer": "FocusExplorer", "workbench.action.files.save": "Save", "editor.action.commentLine": "ToggleComment"},
	"zed":       {"workspace::ActivateFile": "QuickOpen", "command_palette::Toggle": "CommandPalette", "terminal::Toggle": "ToggleTerminal", "workspace::Save": "Save", "pane::CloseActiveItem": "CloseBuffer"},
	"helix":     {"file_picker": "QuickOpen", "command_palette": "CommandPalette", "save": "Save", "search_next": "NextSearchResult"},
	"jetbrains": jetBrainsActions,
	"sublime":   sublimeActions,
	"vim":       {},
}

func mapKey(source string, k KeyBinding) (string, string, string) {
	if source == "helix" || source == "vim" {
		return "", "", "no equivalent (editor-mode binding)"
	}
	key, chord := normalizeSourceKey(source, k.Key)
	note := ""
	if chord {
		return "", "", "no equivalent (two-key chord)"
	}
	if strings.Contains(key, "cmd+") || strings.Contains(key, "super+") {
		key = strings.ReplaceAll(key, "cmd+", "ctrl+")
		key = strings.ReplaceAll(key, "super+", "ctrl+")
		key, _ = normalizeSourceKey("", key)
		note = "Cmd mapped to Ctrl; terminals do not send Cmd"
	}
	if keymap.ValidateKey(key) != nil {
		return "", "", "no equivalent (unsupported key syntax)"
	}
	action := ActionMaps[source][k.Command]
	if action == "" && (source == "vscodium" || source == "cursor" || source == "windsurf") {
		action = ActionMaps["vscode"][k.Command]
	}
	if action == "" {
		return "", "", "no equivalent"
	}
	key = keymap.CanonicalKey(key)
	if !hasModifier(key) && !isFunctionKey(key) && !isSafeNamedKey(key, action) {
		if strings.HasPrefix(key, "shift+") {
			return "", "", "no equivalent (needs ctrl or alt in termocode)"
		}
		return "", "", "no equivalent (needs a modifier in termocode)"
	}
	equivalent := key + " → " + action
	if note != "" {
		equivalent += " (" + note + ")"
	}
	return key, action, equivalent
}

func normalizeSourceKey(source, input string) (string, bool) {
	s := strings.TrimSpace(input)
	if source != "jetbrains" && len(strings.Fields(s)) > 1 {
		return "", true
	}
	if source == "jetbrains" {
		if strings.Contains(s, ",") {
			return "", true
		}
		fields := strings.Fields(strings.ToLower(s))
		var mods []string
		name := ""
		for _, f := range fields {
			switch f {
			case "control", "ctrl":
				mods = append(mods, "ctrl")
			case "alt", "option":
				mods = append(mods, "alt")
			case "shift":
				mods = append(mods, "shift")
			case "meta", "cmd", "command":
				mods = append(mods, "cmd")
			default:
				name = f
				if k, ok := jetBrainsKeyNames[f]; ok {
					name = k
				}
			}
		}
		if name == "" {
			return "", false
		}
		s = strings.Join(append(mods, name), "+")
	} else if source == "zed" {
		s = strings.ToLower(s)
		if strings.HasSuffix(s, "-") {
			s = strings.ReplaceAll(strings.TrimSuffix(s, "-"), "-", "+") + "+-"
		} else {
			s = strings.ReplaceAll(s, "-", "+")
		}
	} else if source == "sublime" {
		s = strings.ToLower(s)
		if i := strings.LastIndex(s, "+"); i > 0 && i < len(s)-1 {
			if k, ok := sublimeKeyNames[s[i+1:]]; ok {
				s = s[:i+1] + k
			}
		}
	} else {
		s = strings.ToLower(s)
	}
	s = strings.ReplaceAll(s, "meta+", "cmd+")
	parts := strings.Split(s, "+")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = append(parts[:len(parts)-2], "+")
	}
	mods := map[string]bool{}
	name := parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "ctrl", "control":
			mods["ctrl"] = true
		case "alt", "option":
			mods["alt"] = true
		case "shift":
			mods["shift"] = true
		case "cmd", "super", "command":
			mods["cmd"] = true
		}
	}
	ordered := make([]string, 0, 4)
	if mods["ctrl"] {
		ordered = append(ordered, "ctrl")
	}
	if mods["alt"] {
		ordered = append(ordered, "alt")
	}
	if mods["shift"] {
		ordered = append(ordered, "shift")
	}
	if mods["cmd"] {
		ordered = append(ordered, "cmd")
	}
	key := strings.Join(append(ordered, name), "+")
	return key, false
}

func hasModifier(key string) bool {
	parts := strings.Split(key, "+")
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "ctrl", "alt", "cmd", "meta", "super":
			return true
		}
	}
	return false
}

func isFunctionKey(key string) bool {
	key = key[strings.LastIndex(key, "+")+1:]
	if len(key) < 2 || key[0] != 'f' {
		return false
	}
	n := 0
	for _, r := range key[1:] {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return n >= 1 && n <= 24
}

func isSafeNamedKey(key, action string) bool {
	if key != "home" && key != "end" {
		return false
	}
	switch action {
	case "QuickOpen", "CommandPalette", "FocusExplorer", "ToggleExplorer", "ToggleTerminal", "WorkspaceSearch":
		return true
	default:
		return false
	}
}
